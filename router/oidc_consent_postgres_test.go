package router

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

type consentFixture struct {
	db            *gorm.DB
	app           *fiber.App
	owner, reader model.Account
	client        model.OIDCClient
	now           time.Time
}

func newConsentFixture(t *testing.T) *consentFixture {
	t.Helper()
	db, owner, reader := auditTestDB(t)
	if err := db.AutoMigrate(&model.OIDCSigningKey{}, &model.OIDCCryptoKey{}); err != nil {
		t.Fatal(err)
	}
	f := &consentFixture{db: db, owner: owner, reader: reader, now: time.Now().Truncate(time.Second)}
	f.client = model.OIDCClient{ID: "consent-client", OwnerID: &owner.ID, Name: "Consent app", HomepageURL: "http://localhost:9090/", Public: true, RedirectURIsJSON: `["http://localhost:9090/callback"]`}
	if err := db.Create(&f.client).Error; err != nil {
		t.Fatal(err)
	}
	for raw, account := range map[string]model.Account{"reader-session": reader, "owner-session": owner} {
		if err := db.Create(&model.LoginSession{AccountID: account.ID, TokenHash: secretHash(raw), CreatedAt: f.now.Add(-5 * time.Minute), ExpiresAt: f.now.Add(time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	provider, err := newOIDCProvider()
	if err != nil {
		t.Fatal(err)
	}
	previous := activeOIDCProvider
	activeOIDCProvider = provider
	t.Cleanup(func() { activeOIDCProvider = previous })
	f.app = fiber.New(fiber.Config{Views: &paginationView{}})
	f.app.Use(auditMiddleware)
	// Match production: protocol endpoints precede browser auth middleware.
	for _, path := range []string{"/authorize", "/oauth/token"} {
		f.app.Use(path, oidcHTTPHandler(provider))
	}
	f.app.Use(auth, csrfGuard, func(c *fiber.Ctx) error { c.Locals(solitudes.CtxTranslator, &translator.Translator{}); return c.Next() })
	f.app.Get("/oidc/consent", consentPage)
	f.app.Post("/oidc/consent", requireAccount, consentHandler)
	f.app.Get("/login", guestRequired, func(c *fiber.Ctx) error { return c.SendString("sign-in") })
	f.app.Post("/login", guestRequired, func(c *fiber.Ctx) error { return c.SendString("sign-in") })
	return f
}

func (f *consentFixture) request(t *testing.T, method, target, cookie string, form url.Values) *http.Response {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	req.Host = "localhost:8080"
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: solitudes.AuthCookie, Value: cookie})
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://localhost:8080")
	}
	resp, err := f.app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (f *consentFixture) authorize(t *testing.T, cookie string, extra url.Values) *http.Response {
	t.Helper()
	digest := sha256.Sum256([]byte(strings.Repeat("v", 64)))
	query := url.Values{"client_id": {f.client.ID}, "redirect_uri": {"http://localhost:9090/callback"}, "response_type": {"code"}, "scope": {"openid email"}, "state": {"consent-state"}, "nonce": {"consent-nonce"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"}}
	for key, values := range extra {
		query[key] = values
	}
	return f.request(t, http.MethodGet, "/authorize?"+query.Encode(), cookie, nil)
}

func consentLocation(t *testing.T, resp *http.Response) *url.URL {
	t.Helper()
	u, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || resp.StatusCode != http.StatusFound || u.Path == "" {
		t.Fatalf("unexpected redirect: status=%d error=%v", resp.StatusCode, err)
	}
	return u
}

func (f *consentFixture) grant(t *testing.T, accountID, clientID, scopes string, expires time.Time, refresh bool) {
	t.Helper()
	if err := f.db.Create(&model.OIDCConsent{AccountID: accountID, ClientID: clientID, Scopes: scopes}).Error; err != nil {
		t.Fatal(err)
	}
	access := model.OIDCAccessToken{AccountID: accountID, ClientID: clientID, Scopes: scopes, ExpiresAt: expires}
	if refresh {
		access.ExpiresAt = f.now.Add(-time.Hour)
	}
	if err := f.db.Create(&access).Error; err != nil {
		t.Fatal(err)
	}
	if refresh {
		if err := f.db.Create(&model.OIDCRefreshToken{AccountID: accountID, ClientID: clientID, AccessID: access.ID, TokenHash: secretHash(access.ID), Scopes: scopes, ExpiresAt: expires}).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestPostgresOIDCConsentReuseAndIsolation(t *testing.T) {
	for _, tc := range []struct {
		name, cookie, prompt, scope, want                     string
		refresh, expired, foreignUser, foreignClient, noGrant bool
		tokenOnly                                             bool
	}{
		{name: "token-only", cookie: "reader-session", tokenOnly: true, prompt: "none", want: "consent_required"},
		{name: "existing", cookie: "reader-session", want: "ready"},
		{name: "subset", cookie: "reader-session", scope: "openid", want: "ready"},
		{name: "scope-order", cookie: "reader-session", scope: "email openid", want: "ready"},
		{name: "refresh-only", cookie: "reader-session", refresh: true, want: "ready"},
		{name: "silent", cookie: "reader-session", prompt: "none", want: "ready"},
		{name: "force-consent", cookie: "reader-session", prompt: "consent", want: "consent"},
		{name: "new-scope", cookie: "reader-session", scope: "openid email profile", want: "consent"},
		{name: "new-offline-access", cookie: "reader-session", scope: "openid email offline_access", want: "consent"},
		{name: "silent-new-scope", cookie: "reader-session", scope: "openid email profile", prompt: "none", want: "consent_required"},
		{name: "silent-no-grant", cookie: "reader-session", prompt: "none", noGrant: true, want: "consent_required"},
		{name: "expired-access", cookie: "reader-session", expired: true, want: "ready"},
		{name: "expired-refresh", cookie: "reader-session", expired: true, refresh: true, prompt: "none", want: "ready"},
		{name: "other-user", cookie: "reader-session", foreignUser: true, prompt: "none", want: "consent_required"},
		{name: "other-client", cookie: "reader-session", foreignClient: true, prompt: "none", want: "consent_required"},
		{name: "guest", prompt: "none", want: "login_required"},
		{name: "bad-cookie", cookie: "not-a-session", prompt: "none", want: "login_required"},
		{name: "interactive-guest", want: "login"},
		{name: "login-prompt", cookie: "reader-session", prompt: "login", want: "login"},
		{name: "select-account", cookie: "reader-session", prompt: "select_account", want: "login"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newConsentFixture(t)
			if !tc.noGrant {
				accountID, clientID, expires := f.reader.ID, f.client.ID, f.now.Add(time.Hour)
				if tc.foreignUser {
					accountID = f.owner.ID
				}
				if tc.foreignClient {
					clientID = "different-client"
				}
				if tc.expired {
					expires = f.now
				}
				f.grant(t, accountID, clientID, "openid email", expires, tc.refresh)
				if tc.tokenOnly {
					if err := f.db.Where("account_id = ?", accountID).Delete(&model.OIDCConsent{}).Error; err != nil {
						t.Fatal(err)
					}
				}
			}
			extra := url.Values{}
			if tc.prompt != "" {
				extra.Set("prompt", tc.prompt)
			}
			if tc.scope != "" {
				extra.Set("scope", tc.scope)
			}
			start := f.authorize(t, tc.cookie, extra)
			location := consentLocation(t, start)
			if strings.HasSuffix(tc.want, "_required") {
				if location.Path != "/callback" || location.Query().Get("error") != tc.want || location.Query().Get("state") != "consent-state" {
					t.Fatalf("wrong silent error: %s", location)
				}
				return
			}
			if location.Path != "/oidc/consent" {
				t.Fatalf("unexpected auth destination: %s", location)
			}
			response := f.request(t, http.MethodGet, location.RequestURI(), tc.cookie, nil)
			if !strings.Contains(response.Header.Get("Cache-Control"), "no-store") {
				t.Fatal("consent must not be cached")
			}
			if tc.want == "consent" {
				if response.StatusCode != http.StatusOK {
					t.Fatalf("consent UI missing: %d", response.StatusCode)
				}
				return
			}
			next := consentLocation(t, response)
			if tc.want == "login" {
				if next.Path != "/login" {
					t.Fatal("login interaction missing")
				}
				return
			}
			if next.Path != "/authorize/callback" {
				t.Fatal("existing grant not reused")
			}
			var saved model.OIDCAuthRequest
			if err := f.db.Take(&saved, "id = ?", location.Query().Get("authRequestID")).Error; err != nil {
				t.Fatal(err)
			}
			if !saved.Approved || saved.AccountID == nil || *saved.AccountID != f.reader.ID || saved.AuthTime == nil || !saved.AuthTime.Equal(f.now.Add(-5*time.Minute)) {
				t.Fatal("approval changed identity or authentication time")
			}
			callback := consentLocation(t, f.request(t, http.MethodGet, next.RequestURI(), tc.cookie, nil))
			if callback.Query().Get("code") == "" || callback.Query().Get("state") != "consent-state" {
				t.Fatal("missing code/state")
			}
		})
	}
}

func TestPostgresOIDCConsentReauthenticationAndRevocation(t *testing.T) {
	f := newConsentFixture(t)
	f.grant(t, f.reader.ID, f.client.ID, "openid email", f.now.Add(time.Hour), false)
	for _, extra := range []url.Values{{"prompt": {"login"}}, {"max_age": {"0"}}, {"max_age": {"1"}}} {
		if err := f.db.Model(&model.LoginSession{}).Where("token_hash = ?", secretHash("reader-session")).Update("created_at", f.now.Add(-5*time.Minute)).Error; err != nil {
			t.Fatal(err)
		}
		location := consentLocation(t, f.authorize(t, "reader-session", extra))
		id := location.Query().Get("authRequestID")
		login := consentLocation(t, f.request(t, http.MethodGet, location.RequestURI(), "reader-session", nil))
		if login.Path != "/login" {
			t.Fatal("fresh authentication not required")
		}
		if resp := f.request(t, http.MethodGet, login.RequestURI(), "reader-session", nil); resp.StatusCode != http.StatusOK {
			t.Fatal("reauthentication login loop")
		}
		if resp := f.request(t, http.MethodPost, "/login", "reader-session", url.Values{"return_to": {location.RequestURI()}}); resp.StatusCode != http.StatusOK {
			t.Fatal("reauthentication POST blocked")
		}
		bypass := consentLocation(t, f.request(t, http.MethodPost, "/oidc/consent", "reader-session", url.Values{"request_id": {id}, "allow": {"yes"}}))
		if bypass.Path != "/login" {
			t.Fatal("manual consent bypassed required reauthentication")
		}
		// A real sign-in creates a new session; simulate only its verified result.
		if err := f.db.Model(&model.LoginSession{}).Where("token_hash = ?", secretHash("reader-session")).Update("created_at", time.Now()).Error; err != nil {
			t.Fatal(err)
		}
		if next := consentLocation(t, f.request(t, http.MethodGet, location.RequestURI(), "reader-session", nil)); next.Path != "/authorize/callback" {
			t.Fatal("fresh session not accepted")
		}
	}
	// A pending request opened while signed out must reuse consent after sign-in.
	location := consentLocation(t, f.authorize(t, "", nil))
	if next := consentLocation(t, f.request(t, http.MethodGet, location.RequestURI(), "reader-session", nil)); next.Path != "/authorize/callback" {
		t.Fatal("return from login did not reuse consent")
	}
	// Another signed-in account cannot finish an already approved request.
	if next := consentLocation(t, f.request(t, http.MethodGet, location.RequestURI(), "owner-session", nil)); next.Path != "/login" {
		t.Fatal("approved request crossed accounts")
	}
	if err := f.db.Transaction(func(tx *gorm.DB) error {
		if _, err := lockOIDCClient(tx, f.client.ID, true); err != nil {
			return err
		}
		return deleteOIDCCredentials(tx, f.client.ID, f.reader.ID)
	}); err != nil {
		t.Fatal(err)
	}
	if resp := f.request(t, http.MethodGet, location.RequestURI(), "reader-session", nil); resp.StatusCode == http.StatusFound {
		t.Fatal("revoked in-flight approval survived")
	}
	location = consentLocation(t, f.authorize(t, "reader-session", url.Values{"prompt": {"none"}}))
	if location.Query().Get("error") != "consent_required" {
		t.Fatal("revoked grant silently restored")
	}
}

func TestPostgresOIDCConsentSessionBoundariesAndHints(t *testing.T) {
	for _, mode := range []string{"expired-session", "disabled-account", "unverified-account", "hint-only", "wrong-hint", "disabled-client", "deleted-client", "max-age", "invalid-prompt", "unknown-prompt", "invalid-redirect", "missing-pkce", "scope-prefix", "approved-request-only"} {
		t.Run(mode, func(t *testing.T) {
			f := newConsentFixture(t)
			if mode != "approved-request-only" {
				f.grant(t, f.reader.ID, f.client.ID, "openid email", f.now.Add(time.Hour), false)
			}
			extra := url.Values{"prompt": {"none"}}
			mutate := func(q *gorm.DB) {
				t.Helper()
				if q.Error != nil {
					t.Fatal(q.Error)
				}
			}
			switch mode {
			case "expired-session":
				mutate(f.db.Model(&model.LoginSession{}).Where("token_hash = ?", secretHash("reader-session")).Update("expires_at", f.now))
			case "disabled-account":
				mutate(f.db.Model(&f.reader).Update("disabled_at", f.now))
			case "unverified-account":
				mutate(f.db.Model(&f.reader).Update("email_verified_at", nil))
			case "disabled-client":
				mutate(f.db.Model(&f.client).Update("disabled_at", f.now))
			case "deleted-client":
				mutate(f.db.Delete(&f.client))
			case "max-age":
				extra.Set("max_age", "1")
			case "invalid-prompt":
				extra.Set("prompt", "none consent")
			case "unknown-prompt":
				extra.Set("prompt", "unsupported")
			case "invalid-redirect":
				extra.Set("redirect_uri", "https://attacker.example/callback")
			case "missing-pkce":
				extra.Set("code_challenge", "")
			case "scope-prefix":
				mutate(f.db.Model(&model.OIDCConsent{}).Where("client_id = ?", f.client.ID).Update("scopes", "openid email.read"))
			case "approved-request-only":
				authorizedRequestFixture(t, f.db, f.client.ID, f.reader.ID)
			case "hint-only", "wrong-hint":
				request := &oidc.AuthRequest{ClientID: f.client.ID, ResponseType: oidc.ResponseTypeCode, RedirectURI: "http://localhost:9090/callback", Scopes: []string{"openid", "email"}, Prompt: []string{oidc.PromptNone}, CodeChallenge: "test", CodeChallengeMethod: oidc.CodeChallengeMethodS256}
				ctx := t.Context()
				subject := f.reader.ID
				if mode == "wrong-hint" {
					ctx = context.WithValue(ctx, oidcBrowserKey{}, oidcBrowserIdentity{secretHash("reader-session")})
					subject = f.owner.ID
				}
				if _, err := (&oidcStorage{db: f.db}).CreateAuthRequest(ctx, request, subject); err == nil {
					t.Fatal("ID hint was treated as authentication")
				}
				return
			}
			resp := f.authorize(t, "reader-session", extra)
			if mode == "invalid-redirect" || mode == "disabled-client" || mode == "deleted-client" {
				if resp.StatusCode == http.StatusFound {
					t.Fatal("untrusted request was redirected")
				}
				return
			}
			location := consentLocation(t, resp)
			want := "login_required"
			if mode == "scope-prefix" || mode == "approved-request-only" {
				want = "consent_required"
			}
			if mode == "missing-pkce" || mode == "invalid-prompt" || mode == "unknown-prompt" {
				want = "invalid_request"
			}
			if location.Query().Get("error") != want {
				t.Fatalf("got %s, want %s", location.Query().Get("error"), want)
			}
		})
	}
}

func TestPostgresOIDCConsentReuseSerializesWithRevocation(t *testing.T) {
	f := newConsentFixture(t)
	f.grant(t, f.reader.ID, f.client.ID, "openid email", f.now.Add(time.Hour), false)
	// Use an independent connection: a one-connection pool would serialize the
	// test even if the application's client-row lock were accidentally removed.
	var schema string
	if err := f.db.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	config, err := pgx.ParseConfig(os.Getenv("SOLITUDES_TEST_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	config.RuntimeParams["search_path"] = schema
	connection := stdlib.OpenDB(*config)
	t.Cleanup(func() { connection.Close() })
	peer, err := gorm.Open(postgres.New(postgres.Config{Conn: connection}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tx := f.db.Begin()
	defer tx.Rollback()
	if _, err := lockOIDCClient(tx, f.client.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := deleteOIDCCredentials(tx, f.client.ID, f.reader.ID); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(t.Context(), oidcBrowserKey{}, oidcBrowserIdentity{secretHash("reader-session")})
	request := &oidc.AuthRequest{ClientID: f.client.ID, ResponseType: oidc.ResponseTypeCode, RedirectURI: "http://localhost:9090/callback", Scopes: []string{"openid", "email"}, Prompt: []string{oidc.PromptNone}, CodeChallenge: "test", CodeChallengeMethod: oidc.CodeChallengeMethodS256}
	result := make(chan error, 1)
	go func() { _, err := (&oidcStorage{db: peer}).CreateAuthRequest(ctx, request, ""); result <- err }()
	select {
	case err := <-result:
		t.Fatalf("bypassed revocation lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("revoked consent was reused")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("consent deadlocked")
	}
	var count int64
	if err := f.db.Model(&model.OIDCAuthRequest{}).Where("approved = true").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("auto-approved request remained", err)
	}
}

func TestOIDCFreshAuthentication(t *testing.T) {
	now := time.Now()
	zero, minute := uint(0), uint(60)
	for _, tc := range []struct {
		max  *uint
		age  time.Duration
		want bool
	}{{nil, time.Hour, true}, {&minute, 60 * time.Second, true}, {&minute, 61 * time.Second, false}, {&zero, time.Second, false}, {&zero, 0, true}} {
		r := &oidcAuthRequest{OIDCAuthRequest: model.OIDCAuthRequest{CreatedAt: now}, req: oidc.AuthRequest{MaxAge: tc.max}}
		if got := oidcFreshAuthentication(r, now.Add(-tc.age), now); got != tc.want {
			t.Errorf("age=%v max=%v: %v", tc.age, tc.max, got)
		}
	}
}

func TestPostgresOIDCConsentAuditAndStorageFailure(t *testing.T) {
	f := newConsentFixture(t)
	for _, cookie := range []string{"", "reader-session"} {
		f.authorize(t, cookie, url.Values{"prompt": {"none"}})
	}
	var count int64
	if err := f.db.Model(&model.AuditEvent{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("routine silent-login checks created audit noise", err)
	}
	if err := f.db.Migrator().DropTable(&model.OIDCConsent{}); err != nil {
		t.Fatal(err)
	}
	location := consentLocation(t, f.authorize(t, "reader-session", nil))
	if location.Query().Get("error") != "server_error" {
		t.Fatal("storage failure did not fail closed")
	}
	if err := f.db.Model(&model.OIDCAuthRequest{}).Where("approved = true").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("storage failure approved a request", err)
	}
	if err := f.db.Model(&model.AuditEvent{}).Where("reason = ?", "server_error").Count(&count).Error; err != nil || count != 1 {
		t.Fatal("real failure was not audited", err)
	}
}

// Keep a protocol-level assertion that an interaction error is machine-readable.
func TestOIDCConsentRequiredErrorCode(t *testing.T) {
	body, err := json.Marshal(oidcNeedsConsent.protocolError())
	if err != nil || !strings.Contains(string(body), `"error":"consent_required"`) {
		t.Fatal(string(body), err)
	}
}

func TestPostgresOIDCGuestLoginNavigationDoesNotCreateAudit(t *testing.T) {
	f := newConsentFixture(t)
	for _, cookie := range []string{"", "expired-session"} {
		location := consentLocation(t, f.authorize(t, cookie, nil))
		if location.Path != "/oidc/consent" {
			t.Fatalf("unexpected authorization destination: %s", location.Path)
		}
		location = consentLocation(t, f.request(t, http.MethodGet, location.String(), cookie, nil))
		if location.Path != "/login" {
			t.Fatalf("guest did not reach login: %s", location.Path)
		}
		response := f.request(t, http.MethodGet, location.String(), cookie, nil)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("login page status: %d", response.StatusCode)
		}
	}
	var count int64
	if err := f.db.Model(&model.AuditEvent{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("guest navigation created %d audit records: %v", count, err)
	}
}

func (f *consentFixture) storedConsent(t *testing.T) model.OIDCConsent {
	t.Helper()
	var rows []model.OIDCConsent
	if err := f.db.Where("account_id = ? AND client_id = ?", f.reader.ID, f.client.ID).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one cumulative consent, got %d", len(rows))
	}
	return rows[0]
}

func (f *consentFixture) approve(t *testing.T, scope string) *url.URL {
	t.Helper()
	location := consentLocation(t, f.authorize(t, "reader-session", url.Values{"prompt": {"consent"}, "scope": {scope}}))
	next := consentLocation(t, f.request(t, http.MethodPost, "/oidc/consent", "reader-session", url.Values{"request_id": {location.Query().Get("authRequestID")}, "allow": {"yes"}}))
	if next.Path != "/authorize/callback" {
		t.Fatalf("approval failed: %s", next)
	}
	return next
}

func TestPostgresOIDCDurableConsentExplicitMergeAndTokenSubset(t *testing.T) {
	f := newConsentFixture(t)
	f.approve(t, "openid email offline_access")
	first := f.storedConsent(t)
	f.approve(t, "openid profile")
	merged := f.storedConsent(t)
	if merged.Scopes != "email offline_access openid profile" || !merged.CreatedAt.Equal(first.CreatedAt) || !merged.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("invalid cumulative consent: %+v", merged)
	}
	callback := f.approve(t, "openid offline_access")
	subset := f.storedConsent(t)
	if subset.Scopes != merged.Scopes || !subset.CreatedAt.Equal(first.CreatedAt) {
		t.Fatal("subset approval removed historical scopes")
	}
	replay := f.request(t, http.MethodPost, "/oidc/consent", "reader-session", url.Values{"request_id": {callback.Query().Get("id")}, "allow": {"yes"}})
	if replay.StatusCode != http.StatusBadRequest || !reflect.DeepEqual(f.storedConsent(t), subset) {
		t.Fatal("replayed approval mutated consent")
	}
	location := consentLocation(t, f.request(t, http.MethodGet, callback.RequestURI(), "reader-session", nil))
	response := f.request(t, http.MethodPost, "/oauth/token", "", url.Values{"grant_type": {"authorization_code"}, "client_id": {f.client.ID}, "code": {location.Query().Get("code")}, "redirect_uri": {"http://localhost:9090/callback"}, "code_verifier": {strings.Repeat("v", 64)}})
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("token exchange %d: %s", response.StatusCode, body)
	}
	var tokens []model.OIDCAccessToken
	if err := f.db.Find(&tokens).Error; err != nil || len(tokens) != 1 || tokens[0].Scopes != "openid offline_access" {
		t.Fatalf("token broadened to historical consent: %+v %v", tokens, err)
	}
	var refreshTokens []model.OIDCRefreshToken
	if err := f.db.Find(&refreshTokens).Error; err != nil || len(refreshTokens) != 1 || refreshTokens[0].Scopes != "openid offline_access" {
		t.Fatalf("refresh token broadened to historical consent: %+v %v", refreshTokens, err)
	}
	for _, table := range []interface{}{&model.OIDCAccessToken{}, &model.OIDCRefreshToken{}} {
		if err := f.db.Model(table).Where("client_id = ?", f.client.ID).Update("expires_at", f.now.Add(-time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, remove := range []bool{false, true} {
		if remove {
			for _, table := range []interface{}{&model.OIDCAccessToken{}, &model.OIDCRefreshToken{}} {
				if err := f.db.Where("client_id = ?", f.client.ID).Delete(table).Error; err != nil {
					t.Fatal(err)
				}
			}
		}
		location := consentLocation(t, f.authorize(t, "reader-session", url.Values{"prompt": {"none"}, "scope": {"openid email profile"}}))
		if location.Path != "/oidc/consent" {
			t.Fatalf("credential expiry/removal revoked consent: %s", location)
		}
		if got := f.storedConsent(t); !reflect.DeepEqual(got, subset) {
			t.Fatal("silent reuse mutated consent")
		}
	}
}

func TestPostgresOIDCDurableConsentRejectedApprovalDoesNotMutate(t *testing.T) {
	for _, mode := range []string{"decline", "stale-authentication", "disabled-client", "expired-request", "consent-write-failure", "request-write-failure"} {
		t.Run(mode, func(t *testing.T) {
			f := newConsentFixture(t)
			f.approve(t, "openid")
			before := f.storedConsent(t)
			extra := url.Values{"prompt": {"consent"}, "scope": {"openid email"}}
			if mode == "stale-authentication" {
				extra.Set("max_age", "0")
			}
			location := consentLocation(t, f.authorize(t, "reader-session", extra))
			id := location.Query().Get("authRequestID")
			var err error
			switch mode {
			case "disabled-client":
				err = f.db.Model(&f.client).Update("disabled_at", f.now).Error
			case "expired-request":
				err = f.db.Model(&model.OIDCAuthRequest{}).Where("id = ?", id).Update("expires_at", f.now.Add(-time.Hour)).Error
			case "consent-write-failure":
				err = f.db.Exec("ALTER TABLE o_id_c_consents ADD CONSTRAINT consent_scope_guard CHECK (scopes = 'openid')").Error
			case "request-write-failure":
				err = f.db.Exec("ALTER TABLE o_id_c_auth_requests ADD CONSTRAINT approval_guard CHECK (NOT approved OR id <> '" + id + "')").Error
			}
			if err != nil {
				t.Fatal(err)
			}
			allow := "yes"
			if mode == "decline" {
				allow = "no"
			}
			response := f.request(t, http.MethodPost, "/oidc/consent", "reader-session", url.Values{"request_id": {id}, "allow": {allow}})
			switch mode {
			case "decline":
				if next := consentLocation(t, response); next.Query().Get("error") != "access_denied" {
					t.Fatalf("decline did not deny access: %s", next)
				}
			case "stale-authentication":
				if next := consentLocation(t, response); next.Path != "/login" {
					t.Fatalf("stale authentication did not require login: %s", next)
				}
			case "disabled-client", "expired-request":
				if response.StatusCode != http.StatusBadRequest {
					t.Fatalf("invalid approval status: %d", response.StatusCode)
				}
			default:
				if response.StatusCode != http.StatusInternalServerError {
					t.Fatalf("storage failure status: %d", response.StatusCode)
				}
			}
			if got := f.storedConsent(t); !reflect.DeepEqual(got, before) {
				t.Fatalf("failed approval mutated consent: %+v", got)
			}
			var approved int64
			if err := f.db.Model(&model.OIDCAuthRequest{}).Where("id = ? AND approved = true", id).Count(&approved).Error; err != nil || approved != 0 {
				t.Fatal("failed approval persisted request", err)
			}
		})
	}
}

func TestPostgresOIDCDurableConsentTokenRevocationAndLogoutPreserve(t *testing.T) {
	for _, operation := range []string{"access-revoke", "refresh-revoke", "logout"} {
		t.Run(operation, func(t *testing.T) {
			f := newConsentFixture(t)
			f.grant(t, f.reader.ID, f.client.ID, "openid email", f.now.Add(time.Hour), true)
			before := f.storedConsent(t)
			var access model.OIDCAccessToken
			if err := f.db.Take(&access).Error; err != nil {
				t.Fatal(err)
			}
			storage := &oidcStorage{db: f.db}
			switch operation {
			case "access-revoke":
				if err := storage.RevokeToken(t.Context(), access.ID, f.reader.ID, f.client.ID); err != nil {
					t.Fatal(err)
				}
			case "refresh-revoke":
				if err := storage.RevokeToken(t.Context(), access.ID, "", f.client.ID); err != nil {
					t.Fatal(err)
				}
			case "logout":
				if err := storage.TerminateSession(t.Context(), f.reader.ID, f.client.ID); err != nil {
					t.Fatal(err)
				}
			}
			var count int64
			if err := f.db.Model(&model.OIDCAccessToken{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatal("token not removed", err)
			}
			if operation != "access-revoke" {
				if err := f.db.Model(&model.OIDCRefreshToken{}).Count(&count).Error; err != nil || count != 0 {
					t.Fatal("refresh token not removed", err)
				}
			}
			if got := f.storedConsent(t); !reflect.DeepEqual(got, before) {
				t.Fatal("credential revocation erased consent")
			}
			location := consentLocation(t, f.authorize(t, "reader-session", url.Values{"prompt": {"none"}}))
			if location.Path != "/oidc/consent" {
				t.Fatalf("consent not reusable: %s", location)
			}
		})
	}
}

func TestPostgresOIDCDurableConsentApprovalWaitsForRevocation(t *testing.T) {
	for _, mode := range []string{"prompt-consent", "additional-scope", "return-from-login"} {
		t.Run(mode, func(t *testing.T) {
			f := newConsentFixture(t)
			f.grant(t, f.reader.ID, f.client.ID, "openid", f.now.Add(time.Hour), false)
			cookie := "reader-session"
			extra := url.Values{}
			if mode == "prompt-consent" {
				extra.Set("prompt", "consent")
			}
			if mode == "return-from-login" {
				cookie = ""
			}
			location := consentLocation(t, f.authorize(t, cookie, extra))
			if mode == "return-from-login" {
				if response := f.request(t, http.MethodGet, location.RequestURI(), "reader-session", nil); response.StatusCode != http.StatusOK {
					t.Fatalf("consent UI after login: %d", response.StatusCode)
				}
			}
			id := location.Query().Get("authRequestID")
			var schema string
			if err := f.db.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
				t.Fatal(err)
			}
			config, err := pgx.ParseConfig(os.Getenv("SOLITUDES_TEST_POSTGRES_DSN"))
			if err != nil {
				t.Fatal(err)
			}
			config.RuntimeParams["search_path"] = schema
			connection := stdlib.OpenDB(*config)
			defer connection.Close()
			peer, err := gorm.Open(postgres.New(postgres.Config{Conn: connection}), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			tx := peer.Begin()
			defer tx.Rollback()
			if _, err := lockOIDCClient(tx, f.client.ID, true); err != nil {
				t.Fatal(err)
			}
			if err := deleteOIDCCredentials(tx, f.client.ID, f.reader.ID); err != nil {
				t.Fatal(err)
			}
			var approvalPID int
			if err := f.db.Raw("SELECT pg_backend_pid()").Scan(&approvalPID).Error; err != nil {
				t.Fatal(err)
			}
			type approvalResult struct {
				response *http.Response
				err      error
			}
			result := make(chan approvalResult, 1)
			req := httptest.NewRequest(http.MethodPost, "/oidc/consent", strings.NewReader(url.Values{"request_id": {id}, "allow": {"yes"}}.Encode()))
			req.Host = "localhost:8080"
			req.Header.Set("Origin", "http://localhost:8080")
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(&http.Cookie{Name: solitudes.AuthCookie, Value: "reader-session"})
			go func() { response, err := f.app.Test(req, -1); result <- approvalResult{response, err} }()
			// Observe a real PostgreSQL lock wait, not a goroutine scheduling delay.
			deadline := time.Now().Add(5 * time.Second)
			waiting := false
			for time.Now().Before(deadline) {
				if err := tx.Exec("SELECT pg_stat_clear_snapshot()").Error; err != nil {
					t.Fatal(err)
				}
				// A wait on the deleted request row would also block without the client
				// lock. Assert the exact connection is waiting in the client query.
				if err := tx.Raw(`SELECT pg_backend_pid() = ANY(pg_blocking_pids(?)) AND EXISTS (
SELECT 1 FROM pg_stat_activity WHERE pid = ? AND query LIKE '%o_id_c_clients%'
)`, approvalPID, approvalPID).Scan(&waiting).Error; err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !waiting {
				t.Fatal("explicit approval did not wait on client revocation lock")
			}
			if err := tx.Commit().Error; err != nil {
				t.Fatal(err)
			}
			select {
			case completed := <-result:
				if completed.err != nil {
					t.Fatal(completed.err)
				}
				defer completed.response.Body.Close()
				if completed.response.StatusCode < 400 {
					t.Fatalf("revoked pending approval succeeded: %d", completed.response.StatusCode)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("approval deadlocked")
			}

			callback := f.request(t, http.MethodGet, "/authorize/callback?id="+url.QueryEscape(id), "reader-session", nil)
			destination, err := url.Parse(callback.Header.Get("Location"))
			if err != nil || destination.Query().Get("code") != "" {
				t.Fatal("revoked request produced an authorization code", err)
			}
			storage := &oidcStorage{db: f.db}
			if err := storage.SaveAuthCode(t.Context(), id, "revoked-request-code"); err == nil {
				t.Fatal("revoked request accepted a code")
			}
			exchange := f.request(t, http.MethodPost, "/oauth/token", "", url.Values{"grant_type": {"authorization_code"}, "client_id": {f.client.ID}, "code": {"revoked-request-code"}, "redirect_uri": {"http://localhost:9090/callback"}, "code_verifier": {strings.Repeat("v", 64)}})
			if exchange.StatusCode != http.StatusBadRequest {
				t.Fatalf("revoked code exchange status: %d", exchange.StatusCode)
			}

			for _, table := range []interface{}{&model.OIDCConsent{}, &model.OIDCAuthRequest{}, &model.OIDCAccessToken{}, &model.OIDCRefreshToken{}} {
				var count int64
				if err := f.db.Model(table).Where("client_id = ?", f.client.ID).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("revoked state survived in %T: %d %v", table, count, err)
				}
			}

		})
	}
}

func TestPostgresOIDCDurableConsentOnlyApplicationRevocation(t *testing.T) {
	f := newConsentFixture(t)
	if err := f.db.Create(&[]model.OIDCConsent{{AccountID: f.reader.ID, ClientID: f.client.ID, Scopes: "openid email"}, {AccountID: f.owner.ID, ClientID: f.client.ID, Scopes: "openid"}}).Error; err != nil {
		t.Fatal(err)
	}
	f.app.Post("/account/oidc/authorizations/:id/revoke", requireAccount, revokeApplicationGrant)
	response := f.request(t, http.MethodPost, "/account/oidc/authorizations/"+f.client.ID+"/revoke", "reader-session", url.Values{})
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("consent-only application cannot be revoked: %d", response.StatusCode)
	}
	var rows []model.OIDCConsent
	if err := f.db.Find(&rows).Error; err != nil || len(rows) != 1 || rows[0].AccountID != f.owner.ID {
		t.Fatalf("revocation isolation failed: %+v %v", rows, err)
	}
	location := consentLocation(t, f.authorize(t, "reader-session", url.Values{"prompt": {"none"}}))
	if location.Query().Get("error") != "consent_required" {
		t.Fatalf("revoked consent reused: %s", location)
	}
}

func TestPostgresOIDCPendingConsentOwnership(t *testing.T) {
	for _, guest := range []bool{false, true} {
		name := "authenticated-request"
		if guest {
			name = "return-from-login"
		}
		t.Run(name, func(t *testing.T) {
			f := newConsentFixture(t)
			cookie := "reader-session"
			if guest {
				cookie = ""
			}
			location := consentLocation(t, f.authorize(t, cookie, url.Values{"prompt": {"consent"}}))
			if guest {
				if response := f.request(t, http.MethodGet, location.RequestURI(), "reader-session", nil); response.StatusCode != http.StatusOK {
					t.Fatalf("consent UI status: %d", response.StatusCode)
				}
			}
			id := location.Query().Get("authRequestID")
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				target := location.RequestURI()
				var form url.Values
				if method == http.MethodPost {
					target = "/oidc/consent"
					form = url.Values{"request_id": {id}, "allow": {"yes"}}
				}
				response := f.request(t, method, target, "owner-session", form)
				if next := consentLocation(t, response); next.Path != "/login" {
					t.Fatalf("another account took pending request via %s: %s", method, next)
				}
			}
			var row model.OIDCAuthRequest
			if err := f.db.Take(&row, "id = ?", id).Error; err != nil {
				t.Fatal(err)
			}
			if row.AccountID == nil || *row.AccountID != f.reader.ID || row.Approved || row.AuthTime != nil {
				t.Fatalf("pending owner must not imply approval: %+v", row)
			}
			var count int64
			if err := f.db.Model(&model.OIDCConsent{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatal("pending request created consent", err)
			}
			response := f.request(t, http.MethodPost, "/oidc/consent", "reader-session", url.Values{"request_id": {id}, "allow": {"yes"}})
			if next := consentLocation(t, response); next.Path != "/authorize/callback" {
				t.Fatalf("rightful owner could not approve: %s", next)
			}
			if err := f.db.Model(&model.OIDCConsent{}).Count(&count).Error; err != nil || count != 1 {
				t.Fatal("wrong consent count", err)
			}
			if grant := f.storedConsent(t); grant.AccountID != f.reader.ID {
				t.Fatal("wrong consent owner")
			}
		})
	}
}

func TestPostgresOIDCPendingConsentBindsOnlyAfterFreshAuthentication(t *testing.T) {
	for _, extra := range []url.Values{{"prompt": {"login"}}, {"prompt": {"select_account"}}, {"max_age": {"0"}}} {
		t.Run(extra.Encode(), func(t *testing.T) {
			f := newConsentFixture(t)
			location := consentLocation(t, f.authorize(t, "reader-session", extra))
			id := location.Query().Get("authRequestID")
			var row model.OIDCAuthRequest
			if err := f.db.Take(&row, "id = ?", id).Error; err != nil {
				t.Fatal(err)
			}
			if row.AccountID != nil || row.Approved {
				t.Fatal("stale session prematurely bound the account")
			}
			if next := consentLocation(t, f.request(t, http.MethodGet, location.RequestURI(), "reader-session", nil)); next.Path != "/login" {
				t.Fatal("fresh login not required")
			}
			// A fresh sign-in may choose another account when there is no subject hint.
			if err := f.db.Model(&model.LoginSession{}).Where("token_hash = ?", secretHash("owner-session")).Update("created_at", time.Now()).Error; err != nil {
				t.Fatal(err)
			}
			if response := f.request(t, http.MethodGet, location.RequestURI(), "owner-session", nil); response.StatusCode != http.StatusOK {
				t.Fatalf("fresh account did not reach consent: %d", response.StatusCode)
			}
			if err := f.db.Take(&row, "id = ?", id).Error; err != nil {
				t.Fatal(err)
			}
			if row.AccountID == nil || *row.AccountID != f.owner.ID || row.Approved {
				t.Fatal("fresh account did not own pending consent")
			}
			if next := consentLocation(t, f.request(t, http.MethodPost, "/oidc/consent", "owner-session", url.Values{"request_id": {id}, "allow": {"yes"}})); next.Path != "/authorize/callback" {
				t.Fatal("fresh account could not approve")
			}
		})
	}
}

func TestPostgresOIDCExplicitConsentAuditIsAtomic(t *testing.T) {
	for _, mode := range []string{"first-approval-audit-failure", "scope-expansion-audit-failure", "request-update-failure"} {
		t.Run(mode, func(t *testing.T) {
			f := newConsentFixture(t)
			var before *model.OIDCConsent
			if mode == "scope-expansion-audit-failure" {
				f.approve(t, "openid")
				saved := f.storedConsent(t)
				before = &saved
			}
			var priorSuccess int64
			if err := f.db.Model(&model.AuditEvent{}).Where("action = ? AND outcome = ?", "oidc.consent", "success").Count(&priorSuccess).Error; err != nil {
				t.Fatal(err)
			}
			location := consentLocation(t, f.authorize(t, "reader-session", url.Values{"prompt": {"consent"}, "scope": {"openid email"}}))
			id := location.Query().Get("authRequestID")
			if mode == "request-update-failure" {
				if err := f.db.Exec("ALTER TABLE o_id_c_auth_requests ADD CONSTRAINT reject_request_approval CHECK (NOT approved OR id <> '" + id + "')").Error; err != nil {
					t.Fatal(err)
				}
			} else {
				// Reject only the success event under test, not unrelated or failure audits.
				if err := f.db.Exec(`CREATE FUNCTION reject_successful_oidc_consent() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
 IF NEW.action = 'oidc.consent' AND NEW.outcome = 'success' THEN
 RAISE EXCEPTION 'consent success audit unavailable' USING ERRCODE = '23514';
 END IF;
 RETURN NEW;
 END $$;
 CREATE TRIGGER reject_successful_oidc_consent BEFORE INSERT ON audit_events
 FOR EACH ROW EXECUTE FUNCTION reject_successful_oidc_consent();`).Error; err != nil {
					t.Fatal(err)
				}
			}
			form := url.Values{"request_id": {id}, "allow": {"yes"}}
			response := f.request(t, http.MethodPost, "/oidc/consent", "reader-session", form)
			if response.StatusCode != http.StatusInternalServerError || strings.Contains(response.Header.Get("Location"), "/authorize/callback") {
				t.Errorf("failed audit/request write still approved: status=%d location=%s", response.StatusCode, response.Header.Get("Location"))
			}
			var grants []model.OIDCConsent
			if err := f.db.Find(&grants).Error; err != nil {
				t.Fatal(err)
			}
			if before == nil {
				if len(grants) != 0 {
					t.Errorf("failed initial approval left consent: %+v", grants)
				}
			} else if len(grants) != 1 || !reflect.DeepEqual(grants[0], *before) {
				t.Errorf("failed expansion changed scopes/timestamps: %+v", grants)
			}
			var pending model.OIDCAuthRequest
			if err := f.db.Take(&pending, "id = ?", id).Error; err != nil {
				t.Fatal(err)
			}
			if pending.Approved || pending.AuthTime != nil {
				t.Error("failed transaction approved request or persisted auth time")
			}
			var success int64
			if err := f.db.Model(&model.AuditEvent{}).Where("action = ? AND outcome = ?", "oidc.consent", "success").Count(&success).Error; err != nil || success != priorSuccess {
				t.Errorf("failed transaction retained success audit: %d %v", success, err)
			}
			if mode == "request-update-failure" {
				if err := f.db.Exec("ALTER TABLE o_id_c_auth_requests DROP CONSTRAINT reject_request_approval").Error; err != nil {
					t.Fatal(err)
				}
			} else if err := f.db.Exec("DROP TRIGGER reject_successful_oidc_consent ON audit_events").Error; err != nil {
				t.Fatal(err)
			}
			response = f.request(t, http.MethodPost, "/oidc/consent", "reader-session", form)
			if next := consentLocation(t, response); next.Path != "/authorize/callback" {
				t.Fatalf("same valid request cannot retry: %s", next)
			}
			if got := f.storedConsent(t); got.Scopes != "email openid" {
				t.Fatalf("retry scopes: %s", got.Scopes)
			}
			if err := f.db.Take(&pending, "id = ?", id).Error; err != nil || !pending.Approved || pending.AuthTime == nil {
				t.Fatal("retry did not approve request", err)
			}
			if err := f.db.Model(&model.AuditEvent{}).Where("action = ? AND outcome = ?", "oidc.consent", "success").Count(&success).Error; err != nil || success != priorSuccess+1 {
				t.Fatalf("success audit duplicated/missing: %d %v", success, err)
			}
			var event model.AuditEvent
			if err := f.db.Take(&event, "request_id = ? AND action = ? AND outcome = ?", response.Header.Get("X-Request-ID"), "oidc.consent", "success").Error; err != nil {
				t.Fatal(err)
			}
			if event.ActorID != f.reader.ID || event.ClientID != f.client.ID || event.Method != http.MethodPost || event.Route != "/oidc/consent" || event.Status != http.StatusFound {
				t.Fatalf("incorrect approval audit identity/route: %+v", event)
			}
		})
	}
}

func TestPostgresOIDCConcurrentExplicitApprovalsMergeScopes(t *testing.T) {
	f := newConsentFixture(t)
	var requestIDs []string
	for _, scope := range []string{"openid email", "openid profile"} {
		location := consentLocation(t, f.authorize(t, "reader-session", url.Values{"prompt": {"consent"}, "scope": {scope}}))
		requestIDs = append(requestIDs, location.Query().Get("authRequestID"))
	}
	var schema string
	if err := f.db.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	config, err := pgx.ParseConfig(os.Getenv("SOLITUDES_TEST_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	config.RuntimeParams["search_path"] = schema
	config.RuntimeParams["application_name"] = schema
	connection := stdlib.OpenDB(*config)
	defer connection.Close()
	connection.SetMaxOpenConns(3)
	peer, err := gorm.Open(postgres.New(postgres.Config{Conn: connection}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	previousDB := solitudes.System.DB
	solitudes.System.DB = peer
	defer func() { solitudes.System.DB = previousDB }()
	// Hold only the first pending request row. The first real POST must acquire
	// the client lock before it blocks here; a second real POST then waits on it.
	blocker := peer.Begin()
	defer blocker.Rollback()
	if err := blocker.Exec("SELECT id FROM o_id_c_auth_requests WHERE id = ? FOR UPDATE", requestIDs[0]).Error; err != nil {
		t.Fatal(err)
	}
	var blockerPID int
	if err := blocker.Raw("SELECT pg_backend_pid()").Scan(&blockerPID).Error; err != nil {
		t.Fatal(err)
	}
	type approvalResult struct {
		response *http.Response
		err      error
	}
	results := make(chan approvalResult, 2)
	launch := func(id string) {
		req := httptest.NewRequest(http.MethodPost, "/oidc/consent", strings.NewReader(url.Values{"request_id": {id}, "allow": {"yes"}}.Encode()))
		req.Host = "localhost:8080"
		req.Header.Set("Origin", "http://localhost:8080")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: solitudes.AuthCookie, Value: "reader-session"})
		go func() { response, err := f.app.Test(req, -1); results <- approvalResult{response, err} }()
	}
	waitForQuery := func(fragment string, blockedBy int) int {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			var pid int
			if err := f.db.Raw(`SELECT pid FROM pg_stat_activity WHERE application_name = ?
 AND wait_event_type = 'Lock' AND query LIKE ? AND ? = ANY(pg_blocking_pids(pid))`, schema, "%"+fragment+"%", blockedBy).Scan(&pid).Error; err != nil {
				t.Fatal(err)
			}
			if pid != 0 {
				return pid
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("no real PostgreSQL wait for %s", fragment)
		return 0
	}
	launch(requestIDs[0])
	firstPID := waitForQuery("o_id_c_auth_requests", blockerPID)
	launch(requestIDs[1])
	waitForQuery("o_id_c_clients", firstPID)
	if err := blocker.Commit().Error; err != nil {
		t.Fatal(err)
	}
	for range requestIDs {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatal(result.err)
			}
			result.response.Body.Close()
			if result.response.StatusCode != http.StatusFound || !strings.HasPrefix(result.response.Header.Get("Location"), "/authorize/callback") {
				t.Fatalf("concurrent approval failed: %d", result.response.StatusCode)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent approvals deadlocked")
		}
	}
	if consent := f.storedConsent(t); consent.Scopes != "email openid profile" {
		t.Fatalf("concurrent approval lost scopes: %s", consent.Scopes)
	}
	for _, id := range requestIDs {
		var request model.OIDCAuthRequest
		if err := f.db.Take(&request, "id = ?", id).Error; err != nil || !request.Approved || request.AccountID == nil || *request.AccountID != f.reader.ID || request.AuthTime == nil {
			t.Fatal("concurrent request not approved for original account", err)
		}
	}
}
