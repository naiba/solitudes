//go:build !postgres_test

package router

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func newIdentityTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// PostgreSQL's uuid_generate_v4() default is not supported by SQLite.
	// Define equivalent tables for portable handler/transaction tests.
	for _, ddl := range []string{
		`CREATE TABLE accounts (id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL, nickname TEXT, password_hash TEXT, role TEXT, email_verified_at datetime, disabled_at datetime, created_at datetime, updated_at datetime)`,
		`CREATE TABLE login_sessions (id TEXT PRIMARY KEY, account_id TEXT, token_hash TEXT UNIQUE, expires_at datetime, created_at datetime)`,
		`CREATE TABLE email_actions (id TEXT PRIMARY KEY, account_id TEXT, token_hash TEXT UNIQUE, purpose TEXT, expires_at datetime, used_at datetime, created_at datetime)`,
		`CREATE TABLE external_identities (id TEXT PRIMARY KEY, account_id TEXT, provider TEXT, subject TEXT)`,
		`CREATE TABLE passkeys (id TEXT PRIMARY KEY, account_id TEXT, credential_id BLOB UNIQUE, public_key BLOB, aaguid BLOB, sign_count INTEGER, user_present BOOL, user_verified BOOL, backup_eligible BOOL, backup_state BOOL, attestation_type TEXT, name TEXT, created_at datetime, updated_at datetime)`,
		`CREATE TABLE passkey_ceremonies (id TEXT PRIMARY KEY, account_id TEXT, cookie_hash TEXT UNIQUE, purpose TEXT, session_json BLOB, expires_at datetime)`,
		`CREATE TABLE comments (id TEXT PRIMARY KEY, article_id TEXT, is_spam BOOL)`,
		`CREATE TABLE o_id_c_clients (id TEXT PRIMARY KEY, owner_id TEXT, name TEXT, secret_hash TEXT, public BOOL, redirect_uris_json TEXT, disabled_at datetime, created_at datetime)`,
		`CREATE TABLE o_id_c_auth_requests (id TEXT PRIMARY KEY, client_id TEXT, account_id TEXT, request_json BLOB, code_hash TEXT UNIQUE, code_used_at datetime, approved BOOL, auth_time datetime, expires_at datetime, created_at datetime)`,
		`CREATE TABLE o_id_c_access_tokens (id TEXT PRIMARY KEY, client_id TEXT, account_id TEXT, scopes TEXT, expires_at datetime, created_at datetime)`,
		`CREATE TABLE o_id_c_refresh_tokens (id TEXT PRIMARY KEY, token_hash TEXT UNIQUE, client_id TEXT, account_id TEXT, access_id TEXT, scopes TEXT, auth_time datetime, expires_at datetime)`,
		`CREATE TABLE o_id_c_signing_keys (id TEXT PRIMARY KEY, key_der BLOB, active BOOL, created_at datetime)`,
		`CREATE TABLE o_id_c_crypto_keys (id TEXT PRIMARY KEY, key BLOB)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestIdentityTablesAccessible(t *testing.T) {
	newIdentityTestDB(t)
}

func TestPublicDomainMustBeHostOnly(t *testing.T) {
	withIdentityDB(t, newIdentityTestDB(t))
	for _, domain := range []string{"localhost:8080", "blog.example.com"} {
		solitudes.System.Config.Site.Domain = domain
		if !validSiteDomain() {
			t.Errorf("valid domain rejected: %s", domain)
		}
	}
	for _, domain := range []string{"", "example.com/path", "example.com?next=evil", "user@example.com", "evil.com\\@example.com", "example.com\nHost: evil.com"} {
		solitudes.System.Config.Site.Domain = domain
		if validSiteDomain() {
			t.Errorf("unsafe site domain accepted: %q", domain)
		}
	}
}

func testAccount(t *testing.T, db *gorm.DB, role model.Role) model.Account {
	t.Helper()
	now := time.Now()
	account := model.Account{ID: "10000000-0000-4000-8000-000000000001", Email: "writer@example.com",
		Nickname: "Writer", PasswordHash: "hash", Role: role, EmailVerifiedAt: &now}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	return account
}

func withIdentityDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	cfg := &model.Config{}
	cfg.Site.Domain = "localhost:8080"
	solitudes.System = &solitudes.SysVariable{DB: db, Config: cfg}
}

func TestDBSessionsAndVerification(t *testing.T) {
	db := newIdentityTestDB(t)
	withIdentityDB(t, db)
	account := testAccount(t, db, model.RoleUser)
	raw := strings.Repeat("a", 64)
	session := model.LoginSession{ID: "10000000-0000-4000-8000-000000000002", AccountID: account.ID,
		TokenHash: secretHash(raw), ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	if found, err := findLoginSession(raw); err != nil || found.ID != account.ID {
		t.Fatalf("valid session: %v, %v", found, err)
	}
	if _, err := findLoginSession(strings.Repeat("b", 64)); err == nil {
		t.Fatal("accepted unknown session")
	}
	if err := db.Model(&session).Update("expires_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := findLoginSession(raw); err == nil {
		t.Fatal("accepted expired session")
	}
	if err := db.Model(&account).Update("email_verified_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&session).Update("expires_at", time.Now().Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := findLoginSession(raw); err == nil {
		t.Fatal("accepted unverified account")
	}

	token := strings.Repeat("v", 64)
	action := model.EmailAction{ID: "10000000-0000-4000-8000-000000000003", AccountID: account.ID,
		TokenHash: secretHash(token), Purpose: "verify_email", ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.Create(&action).Error; err != nil {
		t.Fatal(err)
	}
	if err := verifyAccountEmail(token); err != nil {
		t.Fatal(err)
	}
	if err := verifyAccountEmail(token); err == nil {
		t.Fatal("reused email token")
	}
	if found, err := findLoginSession(raw); err != nil || found.ID != account.ID {
		t.Fatalf("verified account session: %v, %v", found, err)
	}
}

func TestUpstreamIdentityCannotSilentlyTakeOverEmail(t *testing.T) {
	db := newIdentityTestDB(t)
	withIdentityDB(t, db)
	owner := testAccount(t, db, model.RoleUser)
	attempt := &model.OAuthAttempt{Provider: "github"}
	identity := upstreamIdentity{Subject: "12345", Email: owner.Email, Name: "Imposter", Verified: true}
	if _, err := findOrLinkExternalIdentity(attempt, identity, nil); err == nil {
		t.Fatal("external identity with matching email silently took over an existing account")
	}
	var count int64
	if err := db.Model(&model.ExternalIdentity{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("identity created without explicit binding: %d %v", count, err)
	}
	attempt.AccountID = &owner.ID
	if _, err := findOrLinkExternalIdentity(attempt, identity, &owner); err != nil {
		t.Fatalf("explicit binding failed: %v", err)
	}
	attempt.AccountID = nil
	linked, err := findOrLinkExternalIdentity(attempt, identity, nil)
	if err != nil || linked.ID != owner.ID {
		t.Fatalf("linked identity failed to sign in: %v %v", linked, err)
	}
}

func TestAccountPasswordChangeRevokesOtherSessions(t *testing.T) {
	db := newIdentityTestDB(t)
	withIdentityDB(t, db)
	account := testAccount(t, db, model.RoleUser)
	oldHash, err := bcrypt.GenerateFromPassword([]byte("first-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&account).Update("password_hash", string(oldHash)).Error; err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"current", "other"} {
		if err := db.Create(&model.LoginSession{ID: token, AccountID: account.ID,
			TokenHash: secretHash(token), ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.OIDCAccessToken{ID: "access", ClientID: "app", AccountID: account.ID,
		ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.OIDCRefreshToken{ID: "refresh", TokenHash: secretHash("refresh"),
		ClientID: "app", AccountID: account.ID, AccessID: "access", ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Use(auth, csrfGuard)
	app.Post("/account/password", requireAccount, changeAccountPassword)
	change := func(old string) int {
		t.Helper()
		body := url.Values{"old_password": {old}, "new_password": {"second-password"}}
		req := httptest.NewRequest(http.MethodPost, "/account/password", strings.NewReader(body.Encode()))
		req.Host = "localhost"
		req.Header.Set("Host", "localhost")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://localhost")
		req.AddCookie(&http.Cookie{Name: solitudes.AuthCookie, Value: "current"})
		response, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response.StatusCode
	}
	if status := change("wrong-password"); status != http.StatusForbidden {
		t.Fatalf("wrong old password returned %d", status)
	}
	if status := change("first-password"); status != http.StatusSeeOther {
		t.Fatalf("password change returned %d", status)
	}
	if _, err := findLoginSession("other"); err == nil {
		t.Fatal("other session not revoked")
	}
	var remaining int64
	if err := db.Model(&model.OIDCAccessToken{}).Where("account_id = ?", account.ID).Count(&remaining).Error; err != nil || remaining != 0 {
		t.Fatalf("OIDC access tokens not revoked: %d %v", remaining, err)
	}
	if err := db.Model(&model.OIDCRefreshToken{}).Where("account_id = ?", account.ID).Count(&remaining).Error; err != nil || remaining != 0 {
		t.Fatalf("OIDC refresh tokens not revoked: %d %v", remaining, err)
	}
	if _, err := findLoginSession("current"); err != nil {
		t.Fatalf("current session unexpectedly revoked: %v", err)
	}
	if status := change("first-password"); status != http.StatusForbidden {
		t.Fatalf("old password accepted after change: %d", status)
	}
}

func TestRoleAndArticlePrivacy(t *testing.T) {
	id := "10000000-0000-4000-8000-000000000001"
	article := model.Article{ID: "article", AuthorID: &id, IsPrivate: true}
	admin := &model.Account{ID: "admin", Role: model.RoleAdmin}
	editor := &model.Account{ID: id, Role: model.RoleEditor}
	otherEditor := &model.Account{ID: "other", Role: model.RoleEditor}
	user := &model.Account{ID: id, Role: model.RoleUser}
	if !canReadArticle(admin, &article) || !canReadArticle(editor, &article) {
		t.Fatal("owner or admin cannot read private article")
	}
	if canReadArticle(otherEditor, &article) || canReadArticle(user, &article) || canReadArticle(nil, &article) {
		t.Fatal("private article leaked to non-author")
	}
	if !mayEditArticle(admin, &article) || !mayEditArticle(editor, &article) {
		t.Fatal("author or admin cannot edit")
	}
	if mayEditArticle(otherEditor, &article) || mayEditArticle(user, &article) {
		t.Fatal("unauthorized edit")
	}
}

func TestCommentCannotReplyAcrossArticles(t *testing.T) {
	db := newIdentityTestDB(t)
	withIdentityDB(t, db)
	if err := db.Exec(`INSERT INTO comments (id, article_id, is_spam) VALUES (?, ?, false)`, "private-comment", "private-article").Error; err != nil {
		t.Fatal(err)
	}
	id := "private-comment"
	form := &commentForm{ReplyTo: &id}
	if _, _, err := getCommentType(form, "public-article"); err == nil {
		t.Fatal("cross-article reply accepted")
	}
	if kind, _, err := getCommentType(form, "private-article"); err != nil || kind != "reply" {
		t.Fatalf("same-article reply failed: %s %v", kind, err)
	}
}

func TestOIDCAuthorizationCodeSingleUseAndPKCE(t *testing.T) {
	db := newIdentityTestDB(t)
	withIdentityDB(t, db)
	account := testAccount(t, db, model.RoleUser)
	storage := &oidcStorage{db: db}
	request := &oidc.AuthRequest{
		ClientID: "test-client", RedirectURI: "https://app.example/callback",
		ResponseType: oidc.ResponseTypeCode, Scopes: oidc.SpaceDelimitedArray{"openid", "email"},
	}
	if _, err := storage.CreateAuthRequest(t.Context(), request, ""); err == nil {
		t.Fatal("accepted authorization without PKCE")
	}
	request.CodeChallenge = strings.Repeat("x", 43)
	request.CodeChallengeMethod = oidc.CodeChallengeMethodS256
	stored, err := storage.CreateAuthRequest(t.Context(), request, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.OIDCAuthRequest{}).Where("id = ?", stored.GetID()).Updates(map[string]interface{}{
		"approved": true, "account_id": account.ID, "auth_time": time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := storage.SaveAuthCode(t.Context(), stored.GetID(), "single-use-code"); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.AuthRequestByCode(t.Context(), "single-use-code"); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.AuthRequestByCode(t.Context(), "single-use-code"); err == nil {
		t.Fatal("reused authorization code")
	}
}

func TestOIDCRefreshTokenRotation(t *testing.T) {
	db := newIdentityTestDB(t)
	withIdentityDB(t, db)
	account := testAccount(t, db, model.RoleUser)
	storage := &oidcStorage{db: db}
	request := &oidcRefreshRequest{token: model.OIDCRefreshToken{AccountID: account.ID, ClientID: "client", AuthTime: time.Now()}, scopes: []string{"openid", "email"}}
	_, raw, _, err := storage.CreateAccessAndRefreshTokens(t.Context(), request, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.TokenRequestByRefreshToken(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	_, newRaw, _, err := storage.CreateAccessAndRefreshTokens(t.Context(), request, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.TokenRequestByRefreshToken(t.Context(), raw); !errors.Is(err, op.ErrInvalidRefreshToken) {
		t.Fatalf("old refresh token is still valid: %v", err)
	}
	if _, err := storage.TokenRequestByRefreshToken(t.Context(), newRaw); err != nil {
		t.Fatalf("new refresh token invalid: %v", err)
	}
}

func TestOIDCProviderAuthorizationFlow(t *testing.T) {
	db := newIdentityTestDB(t)
	withIdentityDB(t, db)
	account := testAccount(t, db, model.RoleUser)
	client := model.OIDCClient{ID: "test-client", Name: "Test", Public: true,
		RedirectURIsJSON: `["http://localhost:9090/callback"]`}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	provider, err := newOIDCProvider()
	if err != nil {
		t.Fatal(err)
	}
	previousProvider := activeOIDCProvider
	activeOIDCProvider = provider
	t.Cleanup(func() { activeOIDCProvider = previousProvider })
	if err := db.Create(&model.LoginSession{ID: "consent-session", AccountID: account.ID,
		TokenHash: secretHash("consent-cookie"), ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	consentApp := fiber.New()
	consentApp.Use(auth, csrfGuard)
	consentApp.Post("/oidc/consent", requireAccount, consentHandler)
	approve := func(id string) {
		t.Helper()
		body := url.Values{"request_id": {id}, "allow": {"yes"}}
		req := httptest.NewRequest(http.MethodPost, "/oidc/consent", strings.NewReader(body.Encode()))
		req.Host = "localhost:8080"
		req.Header.Set("Host", "localhost:8080")
		req.Header.Set("Origin", "http://localhost:8080")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: solitudes.AuthCookie, Value: "consent-cookie"})
		resp, err := consentApp.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusFound || !strings.Contains(resp.Header.Get("Location"), "/authorize/callback") {
			t.Fatalf("consent result: %d %s", resp.StatusCode, resp.Header.Get("Location"))
		}
	}
	request := func(method, target string, form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		var body *strings.Reader
		if form == nil {
			body = strings.NewReader("")
		} else {
			body = strings.NewReader(form.Encode())
		}
		req := httptest.NewRequest(method, target, body)
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		response := httptest.NewRecorder()
		provider.ServeHTTP(response, req)
		return response
	}
	discovery := request(http.MethodGet, "/.well-known/openid-configuration", nil)
	if discovery.Code != http.StatusOK || !strings.Contains(discovery.Body.String(), `"issuer":"http://localhost:8080"`) {
		t.Fatalf("OIDC discovery: %d %s", discovery.Code, discovery.Body.String())
	}
	firstKeys := request(http.MethodGet, "/keys", nil)
	if firstKeys.Code != http.StatusOK {
		t.Fatalf("JWKS: %d %s", firstKeys.Code, firstKeys.Body.String())
	}
	var firstSet struct {
		Keys []struct {
			KID string `json:"kid"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(firstKeys.Body.Bytes(), &firstSet); err != nil || len(firstSet.Keys) != 1 {
		t.Fatalf("initial signing key: %v %s", err, firstKeys.Body.String())
	}
	if err := generateSigningKey(db); err != nil {
		t.Fatal(err)
	}
	rotated := request(http.MethodGet, "/keys", nil)
	if rotated.Code != http.StatusOK || !strings.Contains(rotated.Body.String(), firstSet.Keys[0].KID) {
		t.Fatalf("old signing key lost after rotation: %d %s", rotated.Code, rotated.Body.String())
	}
	verifier := strings.Repeat("v", 64)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	query := url.Values{"client_id": {client.ID}, "redirect_uri": {"http://localhost:9090/callback"},
		"response_type": {"code"}, "scope": {"openid email"}, "state": {"state-123"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}}
	start := request(http.MethodGet, "/authorize?"+query.Encode(), nil)
	if start.Code != http.StatusFound {
		t.Fatalf("authorize: %d %s", start.Code, start.Body.String())
	}
	location, err := url.Parse(start.Header().Get("Location"))
	if err != nil || location.Path != "/oidc/consent" {
		t.Fatalf("consent redirect: %v %v", location, err)
	}
	id := location.Query().Get("authRequestID")
	if id == "" {
		t.Fatal("missing authorization request ID")
	}
	approve(id)
	callback := request(http.MethodGet, "/authorize/callback?id="+url.QueryEscape(id), nil)
	if callback.Code != http.StatusFound {
		t.Fatalf("authorization callback: %d %s", callback.Code, callback.Body.String())
	}
	redirect, err := url.Parse(callback.Header().Get("Location"))
	if err != nil || redirect.Query().Get("state") != "state-123" || redirect.Query().Get("code") == "" {
		t.Fatalf("client redirect: %v %v", redirect, err)
	}
	code := redirect.Query().Get("code")
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ID},
		"code": {code}, "code_verifier": {verifier}, "redirect_uri": {"http://localhost:9090/callback"}}
	// A valid verifier must work exactly once.
	result := request(http.MethodPost, "/oauth/token", form)
	if result.Code != http.StatusOK {
		t.Fatalf("token exchange: %d %s", result.Code, result.Body.String())
	}
	var tokens struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &tokens); err != nil || tokens.AccessToken == "" || tokens.IDToken == "" {
		t.Fatalf("missing tokens: %v %s", err, result.Body.String())
	}
	userReq := httptest.NewRequest(http.MethodGet, "/userinfo", nil)
	userReq.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	userResp := httptest.NewRecorder()
	provider.ServeHTTP(userResp, userReq)
	if userResp.Code != http.StatusOK || !strings.Contains(userResp.Body.String(), account.Email) {
		t.Fatalf("userinfo: %d %s", userResp.Code, userResp.Body.String())
	}
	if response := request(http.MethodPost, "/oauth/token", form); response.Code == http.StatusOK {
		t.Fatal("authorization code reused")
	}
	second := request(http.MethodGet, "/authorize?"+query.Encode(), nil)
	secondURL, err := url.Parse(second.Header().Get("Location"))
	if err != nil || secondURL.Query().Get("authRequestID") == "" {
		t.Fatalf("second authorization: %d %s", second.Code, second.Header().Get("Location"))
	}
	secondID := secondURL.Query().Get("authRequestID")
	approve(secondID)
	secondCallback := request(http.MethodGet, "/authorize/callback?id="+url.QueryEscape(secondID), nil)
	secondRedirect, err := url.Parse(secondCallback.Header().Get("Location"))
	if err != nil || secondRedirect.Query().Get("code") == "" {
		t.Fatalf("second callback: %d %s", secondCallback.Code, secondCallback.Body.String())
	}
	bad := url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ID},
		"code": {secondRedirect.Query().Get("code")}, "code_verifier": {strings.Repeat("x", 64)},
		"redirect_uri": {"http://localhost:9090/callback"}}
	if response := request(http.MethodPost, "/oauth/token", bad); response.Code == http.StatusOK {
		t.Fatal("PKCE mismatch accepted")
	}
	adminApp := fiber.New()
	adminApp.Post("/admin/oidc/clients/:id/disable", func(c *fiber.Ctx) error {
		administrator := account
		administrator.Role = model.RoleAdmin
		c.Locals(solitudes.CtxAccount, &administrator)
		return disableOIDCClient(c)
	})
	disableReq := httptest.NewRequest(http.MethodPost, "/admin/oidc/clients/"+client.ID+"/disable", nil)
	disableResp, err := adminApp.Test(disableReq, -1)
	if err != nil {
		t.Fatal(err)
	}
	disableResp.Body.Close()
	if disableResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("disable client: %d", disableResp.StatusCode)
	}
	if _, err := (&oidcStorage{db: db}).GetClientByClientID(t.Context(), client.ID); err == nil {
		t.Fatal("disabled client still authorized")
	}
	userResp = httptest.NewRecorder()
	provider.ServeHTTP(userResp, userReq)
	if userResp.Code == http.StatusOK {
		t.Fatal("disabled client's access token still works")
	}
}
