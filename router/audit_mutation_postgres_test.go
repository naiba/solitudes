package router

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func TestPostgresAuditUnchangedMutations(t *testing.T) {
	db, admin, reader := auditTestDB(t)
	client := model.OIDCClient{ID: "unchanged-client", OwnerID: &admin.ID, Name: "Application", HomepageURL: "https://example.com", RedirectURIsJSON: `["https://example.com/callback"]`, PostLogoutURIsJSON: `[]`}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Use(auditMiddleware, func(c *fiber.Ctx) error { c.Locals(solitudes.CtxAccount, &admin); return c.Next() })
	app.Post("/admin/users/:id/role", setUserRole)
	app.Post("/admin/oidc/clients/:id/metadata", updateOIDCClient)
	app.Post("/account/profile", updateAccountProfile)
	for _, tc := range []struct {
		path, field, initial, changed, action string
		form                                  url.Values
	}{
		{"/admin/users/" + reader.ID + "/role", "role", "user", "editor", "user.role.update", url.Values{}},
		{"/admin/oidc/clients/" + client.ID + "/metadata", "name", client.Name, "Renamed application", "client.update", url.Values{"homepage_url": {client.HomepageURL}, "redirect_uris": {"https://example.com/callback"}}},
		{"/account/profile", "nickname", admin.Nickname, "Updated nickname", "account.profile.update", url.Values{"directory_visible": {"on"}}},
	} {
		t.Run(tc.action, func(t *testing.T) {
			for i, value := range []string{tc.initial, tc.changed, tc.changed} {
				tc.form.Set(tc.field, value)
				req := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				resp, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode != 303 {
					t.Fatalf("status %d", resp.StatusCode)
				}
				var events []model.AuditEvent
				if err := db.Where("action = ?", tc.action).Find(&events).Error; err != nil {
					t.Fatal(err)
				}
				want := 1
				if i == 0 {
					want = 0
				}
				if len(events) != want {
					t.Fatalf("step %d events=%+v", i, events)
				}
				if want == 1 && (events[0].Outcome != "success" || events[0].ActorID != admin.ID) {
					t.Fatalf("incorrect event: %+v", events)
				}
			}
		})
	}
	var account model.Account
	if err := db.Take(&account, "id = ?", reader.ID).Error; err != nil || account.Role != model.RoleEditor {
		t.Fatalf("role not updated: %+v %v", account, err)
	}
	if err := db.Take(&client, "id = ?", client.ID).Error; err != nil || client.Name != "Renamed application" {
		t.Fatalf("client not updated: %+v %v", client, err)
	}
	var profile model.Account
	if err := db.Take(&profile, "id = ?", admin.ID).Error; err != nil || profile.Nickname != "Updated nickname" {
		t.Fatalf("profile not updated: %+v %v", profile, err)
	}
}

func TestPostgresAuditTokenRevocationRecordsOnlyDeletion(t *testing.T) {
	db, _, reader := auditTestDB(t)
	storage := &oidcStorage{db: db}
	for _, refresh := range []bool{false, true} {
		access := model.OIDCAccessToken{ClientID: "revocation-client", AccountID: reader.ID, ExpiresAt: time.Now().Add(time.Hour)}
		if err := db.Create(&access).Error; err != nil {
			t.Fatal(err)
		}
		raw, subject := access.ID, reader.ID
		if refresh {
			raw = "refresh-value"
			subject = ""
			if err := db.Create(&model.OIDCRefreshToken{TokenHash: secretHash(raw), ClientID: access.ClientID, AccountID: reader.ID, AccessID: access.ID, ExpiresAt: access.ExpiresAt}).Error; err != nil {
				t.Fatal(err)
			}
		}
		for i, client := range []string{"other-client", access.ClientID, access.ClientID} {
			ctx := context.WithValue(t.Context(), auditContextKey{}, &auditRequest{ID: access.ID})
			if err := storage.RevokeToken(ctx, raw, subject, client); err != nil {
				t.Fatal(err)
			}
			var events []model.AuditEvent
			if err := db.Where("request_id = ?", access.ID).Find(&events).Error; err != nil {
				t.Fatal(err)
			}
			want := 1
			if i == 0 {
				want = 0
			}
			if len(events) != want {
				t.Fatalf("refresh=%v step=%d events=%+v", refresh, i, events)
			}
			if want == 1 && (events[0].Action != "oidc.request" || events[0].ActorID != reader.ID || events[0].ClientID != access.ClientID || events[0].Outcome != "success") {
				t.Fatalf("incorrect revocation: %+v", events)
			}
			var count int64
			if err := db.Model(&model.OIDCAccessToken{}).Where("id = ?", access.ID).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			expected := int64(0)
			if i == 0 {
				expected = 1
			}
			if count != expected {
				t.Fatalf("token count=%d", count)
			}
		}
	}
}

func TestPostgresAuditVerificationDeliveryAndLinkStart(t *testing.T) {
	db, admin, _ := auditTestDB(t)
	port, messages := startMailCatcher(t)
	solitudes.System.Config.Email.Host = "127.0.0.1"
	solitudes.System.Config.Email.Port = port
	solitudes.System.Config.Email.User = "test@localhost"
	solitudes.System.Config.Auth.GitHub.ClientID = "test-client"
	solitudes.System.Config.Auth.GitHub.ClientSecret = "test-secret"
	if err := db.AutoMigrate(&model.OAuthAttempt{}); err != nil {
		t.Fatal(err)
	}
	pending := model.Account{Email: "pending@audit.test", Nickname: "Pending", Role: model.RoleUser}
	if err := db.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Use(auditMiddleware)
	app.Post("/resend-verification", resendVerification)
	app.Post("/auth/:provider/link", func(c *fiber.Ctx) error { c.Locals(solitudes.CtxAccount, &admin); return beginOAuthLink(c) })
	post := func(path string, form url.Values, status int) string {
		t.Helper()
		req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != status {
			t.Fatalf("status=%d want=%d", resp.StatusCode, status)
		}
		return resp.Header.Get("X-Request-ID")
	}
	check := func(id string, want int) {
		t.Helper()
		var events []model.AuditEvent
		if err := db.Where("request_id = ?", id).Find(&events).Error; err != nil {
			t.Fatal(err)
		}
		if len(events) != want {
			t.Fatalf("events=%+v want=%d", events, want)
		}
	}
	for index, email := range []string{"missing@audit.test", admin.Email, pending.Email, pending.Email, pending.Email, pending.Email} {
		captchaID := "verification-" + email
		captchaStore.Set(captchaID, "valid")
		id := post("/resend-verification", url.Values{"email": {email}, "captchaId": {captchaID}, "captcha": {"valid"}}, 200)
		want := 0
		if index >= 2 && index <= 4 {
			want = 1
			select {
			case <-messages:
			case <-time.After(5 * time.Second):
				t.Fatal("verification email was not delivered")
			}
		} else {
			select {
			case <-messages:
				t.Fatal("no-op unexpectedly sent email")
			default:
			}
		}
		check(id, want)
	}
	var sent int64
	if err := db.Model(&model.EmailAction{}).Where("account_id = ?", pending.ID).Count(&sent).Error; err != nil || sent != 3 {
		t.Fatalf("deliveries=%d err=%v", sent, err)
	}
	var audits int64
	if err := db.Model(&model.AuditEvent{}).Where("action = ?", "account.verification.send").Count(&audits).Error; err != nil || audits != 3 {
		t.Fatalf("delivery audits=%d err=%v", audits, err)
	}
	check(post("/resend-verification", url.Values{"email": {pending.Email}}, 400), 1)
	check(post("/auth/github/link", nil, 302), 0)
	var attempts int64
	if err := db.Model(&model.OAuthAttempt{}).Count(&attempts).Error; err != nil || attempts != 1 {
		t.Fatalf("link did not start: %d %v", attempts, err)
	}
	check(post("/auth/unknown/link", nil, 404), 1)
}

func TestPostgresAuditRevocationFailureRollsBackTokens(t *testing.T) {
	db, _, reader := auditTestDB(t)
	access := model.OIDCAccessToken{ClientID: "rollback-client", AccountID: reader.ID, ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.Create(&access).Error; err != nil {
		t.Fatal(err)
	}
	refresh := model.OIDCRefreshToken{ClientID: access.ClientID, AccountID: reader.ID, AccessID: access.ID, TokenHash: secretHash("rollback-refresh"), ExpiresAt: access.ExpiresAt}
	if err := db.Create(&refresh).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE audit_events ADD CONSTRAINT reject_revocation CHECK (reason <> 'token_revoked')").Error; err != nil {
		t.Fatal(err)
	}
	storage := &oidcStorage{db: db}
	for _, token := range []struct{ value, subject string }{{access.ID, reader.ID}, {"rollback-refresh", ""}} {
		if err := storage.RevokeToken(t.Context(), token.value, token.subject, access.ClientID); err == nil {
			t.Fatal("revocation ignored audit failure")
		}
		var accessCount, refreshCount int64
		if err := db.Model(&model.OIDCAccessToken{}).Count(&accessCount).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&model.OIDCRefreshToken{}).Count(&refreshCount).Error; err != nil {
			t.Fatal(err)
		}
		if accessCount != 1 || refreshCount != 1 {
			t.Fatalf("credentials not rolled back: %d %d", accessCount, refreshCount)
		}
	}
}

func TestPostgresAuditUnchangedClientStillChecksOwnership(t *testing.T) {
	db, admin, reader := auditTestDB(t)
	client := model.OIDCClient{ID: "private-client", OwnerID: &admin.ID, Name: "Private", HomepageURL: "https://example.com", RedirectURIsJSON: `["https://example.com/callback"]`, PostLogoutURIsJSON: `[]`}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Use(auditMiddleware, func(c *fiber.Ctx) error { c.Locals(solitudes.CtxAccount, &reader); return c.Next() })
	app.Post("/account/oidc/clients/:id/metadata", updateOIDCClient)
	form := url.Values{"name": {client.Name}, "homepage_url": {client.HomepageURL}, "redirect_uris": {"https://example.com/callback"}}
	req := httptest.NewRequest("POST", "/account/oidc/clients/"+client.ID+"/metadata", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var events []model.AuditEvent
	if err := db.Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Outcome != "failure" || events[0].ActorID != reader.ID {
		t.Fatalf("ownership failure missing: %+v", events)
	}
}
