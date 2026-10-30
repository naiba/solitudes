package router

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func TestPostgresOAuthAccountCanSetFirstPassword(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.ExternalIdentity{}, &model.OIDCAccessToken{}, &model.OIDCRefreshToken{}); err != nil {
		t.Fatal(err)
	}
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	config := &model.Config{}
	config.Site.Domain = "localhost:8080"
	solitudes.System = &solitudes.SysVariable{DB: db, Config: config}
	t.Setenv("SOLITUDES_E2E", "1")
	attempt := &model.OAuthAttempt{Provider: "github"}
	identity := upstreamIdentity{Subject: "oauth-new-reader", Email: "OAuth.Reader@example.com", Name: "OAuth reader", Verified: true}
	account, err := findOrLinkExternalIdentity(attempt, identity, nil)
	if err != nil {
		t.Fatal(err)
	}
	if account.Email != "oauth.reader@example.com" || account.EmailVerifiedAt == nil || account.PasswordHash != "" || account.Role != model.RoleUser {
		t.Fatal("OAuth registration must create a verified ordinary account without a password")
	}
	var linked model.ExternalIdentity
	if err := db.Take(&linked, "account_id = ?", account.ID).Error; err != nil || linked.Provider != "github" || linked.Subject != identity.Subject {
		t.Fatalf("external identity not bound: %+v, %v", linked, err)
	}
	for _, token := range []string{"oauth-current-session", "oauth-other-session"} {
		if err := db.Create(&model.LoginSession{AccountID: account.ID, TokenHash: secretHash(token), ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	app := newTestRouter()
	app.Use(auth, csrfGuard)
	app.Post("/login", guestRequired, loginHandler)
	app.Post("/account/password", requireAccount, changeAccountPassword)
	post := func(path, token, origin string, fields url.Values) *http.Response {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "http://localhost:8080"+path, strings.NewReader(fields.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", origin)
		if token != "" {
			req.AddCookie(&http.Cookie{Name: solitudes.AuthCookie, Value: token})
		}
		resp, err := testRequest(app, req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	password := "reader-first-password"
	loginFields := url.Values{"email": {account.Email}, "password": {password}, "captchaId": {"test"}, "captcha": {"test"}}
	if resp := post("/login", "", "http://localhost:8080", loginFields); resp.StatusCode != http.StatusUnauthorized || len(resp.Cookies()) != 0 {
		t.Fatalf("passwordless account logged in with a password: %d", resp.StatusCode)
	}
	for _, trial := range []struct {
		name, token, origin, password string
		status                        int
	}{
		{"anonymous", "", "http://localhost:8080", password, http.StatusFound},
		{"cross origin", "oauth-current-session", "https://other.example", password, http.StatusForbidden},
		{"too short", "oauth-current-session", "http://localhost:8080", "short", http.StatusBadRequest},
		{"too long", "oauth-current-session", "http://localhost:8080", strings.Repeat("p", 73), http.StatusBadRequest},
	} {
		t.Run(trial.name, func(t *testing.T) {
			resp := post("/account/password", trial.token, trial.origin, url.Values{"new_password": {trial.password}})
			if resp.StatusCode != trial.status {
				t.Fatalf("status=%d want=%d", resp.StatusCode, trial.status)
			}
			var stored model.Account
			if err := db.Take(&stored, "id = ?", account.ID).Error; err != nil || stored.PasswordHash != "" {
				t.Fatalf("rejected request changed the password: %v", err)
			}
		})
	}
	resp := post("/account/password", "oauth-current-session", "http://localhost:8080", url.Values{"new_password": {password}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/account?password_changed=1" {
		t.Fatalf("first password was not set: status=%d location=%q", resp.StatusCode, resp.Header.Get("Location"))
	}
	var stored model.Account
	if err := db.Take(&stored, "id = ?", account.ID).Error; err != nil || bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte(password)) != nil {
		t.Fatalf("first password not securely stored: %v", err)
	}
	if _, err := findLoginSession("oauth-other-session"); err == nil {
		t.Fatal("other session survived password setup")
	}
	if current, err := findLoginSession("oauth-current-session"); err != nil || current.ID != account.ID {
		t.Fatalf("current session was revoked: %v", err)
	}
	if resp := post("/login", "", "http://localhost:8080", loginFields); resp.StatusCode != http.StatusFound || len(resp.Cookies()) == 0 {
		t.Fatalf("new password cannot sign in: %d", resp.StatusCode)
	}
	if again, err := findOrLinkExternalIdentity(attempt, identity, nil); err != nil || again.ID != account.ID {
		t.Fatalf("OAuth login stopped working after setting a password: %v", err)
	}
	var count int64
	if err := db.Model(&model.Account{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("returning OAuth login duplicated the account: count=%d error=%v", count, err)
	}
	// A request may have authenticated before another request set the password.
	// Its old passwordless snapshot must not authorize overwriting that password.
	stale := newTestRouter()
	stale.Use(func(w http.ResponseWriter, r *http.Request) error {
		requestValue(r, solitudes.CtxAccount, account)
		return nil
	})
	stale.Post("/account/password", requireAccount, changeAccountPassword)
	req := httptest.NewRequest(http.MethodPost, "/account/password", strings.NewReader(url.Values{"new_password": {"replacement-password"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	result := httptest.NewRecorder()
	stale.ServeHTTP(result, req)
	if result.Code != http.StatusForbidden {
		t.Fatalf("stale passwordless snapshot overwrote a password: %d", result.Code)
	}
	var unchanged model.Account
	if err := db.Take(&unchanged, "id = ?", account.ID).Error; err != nil || unchanged.PasswordHash != stored.PasswordHash {
		t.Fatalf("stale request changed stored credentials: %v", err)
	}
}
