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
	}{
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
		{name: "expired-access", cookie: "reader-session", expired: true, want: "consent"},
		{name: "expired-refresh", cookie: "reader-session", expired: true, refresh: true, prompt: "none", want: "consent_required"},
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
				mutate(f.db.Model(&model.OIDCAccessToken{}).Where("client_id = ?", f.client.ID).Update("scopes", "openid email.read"))
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

func TestPostgresOIDCConsentCombinesOnlyLiveScopes(t *testing.T) {
	f := newConsentFixture(t)
	f.grant(t, f.reader.ID, f.client.ID, "openid", f.now.Add(time.Hour), false)
	f.grant(t, f.reader.ID, f.client.ID, "email", f.now.Add(time.Hour), true)
	f.grant(t, f.reader.ID, f.client.ID, "profile", f.now, false)
	location := consentLocation(t, f.authorize(t, "reader-session", url.Values{"prompt": {"none"}}))
	if location.Path != "/oidc/consent" {
		t.Fatal("live scopes were not combined")
	}
	location = consentLocation(t, f.authorize(t, "reader-session", url.Values{"prompt": {"none"}, "scope": {"openid email profile"}}))
	if location.Query().Get("error") != "consent_required" {
		t.Fatal("expired scope was reused")
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
	if err := f.db.Migrator().DropTable(&model.OIDCAccessToken{}); err != nil {
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
