package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func TestPostgresAuditRoutineRequestsDoNotHideSecurityEvents(t *testing.T) {
	db, admin, _ := auditTestDB(t)
	for _, tc := range []struct {
		method, path, reason, outcome, want string
		status                              int
		recorded                            bool
	}{
		{"GET", "/admin/audit", "", "", "", 200, false},
		{"GET", "/authorize", "", "", "", 302, false},
		{"GET", "/authorize/callback", "", "", "", 302, false},
		{"POST", "/oauth/token", "", "", "", 200, false},
		{"POST", "/oauth/introspect", "", "", "", 200, false},
		{"GET", "/userinfo", "", "", "", 200, false},
		{"POST", "/userinfo", "", "", "", 200, false},
		{"POST", "/auth/passkey/login/begin", "", "", "", 200, false},
		{"POST", "/account/passkeys/begin", "", "", "", 200, false},
		{"GET", "/admin/login", "", "", "", 404, false},
		{"HEAD", "/admin/login", "", "", "", 404, false},
		{"GET", "/account/missing-page", "", "", "", 404, false},
		{"GET", "/admin/users/missing-user", "", "", "", 404, false},
		{"GET", "/accountant", "", "", "", 403, false},
		{"GET", "/admin/audit", "", "", "audit.view", 403, false},
		{"POST", "/oauth/token", "invalid_grant", "failure", "oidc.request", 400, false},
		{"POST", "/oauth/introspect", "invalid_client", "failure", "oidc.request", 401, false},
		{"GET", "/userinfo", "invalid_token", "failure", "oidc.request", 401, false},
		{"POST", "/userinfo", "insufficient_scope", "failure", "oidc.request", 403, false},
		{"GET", "/authorize", "invalid_request", "failure", "oidc.authorize", 302, false},
		{"GET", "/authorize/callback", "access_denied", "failure", "oidc.authorize", 302, false},
		{"GET", "/authorize", "", "denied", "oidc.authorize", 302, false},
		{"POST", "/auth/passkey/login/begin", "", "", "passkey.operation", 400, false},
		{"POST", "/account/passkeys/begin", "", "", "passkey.operation", 403, false},
		{"POST", "/auth/passkey/login/begin", "", "", "passkey.operation", 429, false},
		{"POST", "/account/passkeys/finish", "", "", "passkey.operation", 201, false},
		{"DELETE", "/account/passkeys/key-id", "", "", "passkey.operation", 200, false},
		{"POST", "/oidc/consent", "", "", "oidc.consent", 302, false},
		{"POST", "/oidc/consent", "consent_declined", "", "oidc.consent", 302, false},
		{"POST", "/revoke", "", "", "oidc.request", 200, false},
		{"GET", "/end_session", "", "", "oidc.logout", 302, false},
		{"POST", "/account/password", "", "", "account.password.update", 302, false},
		{"POST", "/admin/settings", "", "", "settings.update", 200, false},
		{"POST", "/account/oidc/clients/missing/delete", "", "", "client.delete", 404, false},
		{"GET", "/admin/audit", "", "", "request.error", 500, false},
		{"GET", "/authorize", "server_error", "failure", "request.error", 500, false},
		{"POST", "/oauth/token", "", "", "request.error", 500, true},
		{"POST", "/admin/settings", "", "", "request.error", 500, true},
	} {
		t.Run(tc.method+tc.path+"/"+http.StatusText(tc.status)+"/"+tc.reason, func(t *testing.T) {
			app := fiber.New()
			app.Use(auditMiddleware)
			app.Add(tc.method, tc.path, func(c *fiber.Ctx) error {
				c.Locals(solitudes.CtxAccount, &admin)
				if tc.reason != "" {
					c.Locals("audit_reason", tc.reason)
				}
				if tc.outcome != "" {
					c.Locals("audit_outcome", tc.outcome)
				}
				c.Locals("audit_recorded", tc.recorded)
				return c.SendStatus(tc.status)
			})
			req := httptest.NewRequest(tc.method, tc.path+"?page=2&token=never-record", nil)
			req.Header.Set("Authorization", "Bearer never-record")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			var events []model.AuditEvent
			if err := db.Where("request_id = ?", resp.Header.Get("X-Request-ID")).Find(&events).Error; err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if len(events) != 0 {
					t.Fatalf("routine request wrote audit: %+v", events)
				}
				return
			}
			if len(events) != 1 || events[0].Action != tc.want || events[0].Status != tc.status || events[0].ActorID != admin.ID {
				t.Fatalf("security event lost or duplicated: %+v", events)
			}
			if tc.reason != "" && events[0].Reason != tc.reason {
				t.Fatalf("reason lost: %+v", events[0])
			}
			if tc.outcome != "" && events[0].Outcome != tc.outcome {
				t.Fatalf("outcome lost: %+v", events[0])
			}
			if tc.reason == "consent_declined" && events[0].Outcome != "denied" {
				t.Fatal("consent denial recorded as success")
			}
			encoded, err := json.Marshal(events)
			if err != nil || strings.Contains(string(encoded), "never-record") {
				t.Fatalf("audit leaked request data: %v", err)
			}
		})
	}
}

func TestPostgresAuditSessionLoginRemainsExactlyOnce(t *testing.T) {
	db, admin, _ := auditTestDB(t)
	for _, tc := range []struct{ method, path, reason string }{
		{"POST", "/login", "password"},
		{"POST", "/auth/passkey/login/finish", "passkey"},
		{"GET", "/auth/github/callback", "oauth"},
	} {
		app := fiber.New()
		app.Use(auditMiddleware)
		app.Add(tc.method, tc.path, func(c *fiber.Ctx) error {
			if err := issueLoginSession(c, &admin, false); err != nil {
				return err
			}
			return c.SendStatus(http.StatusOK)
		})
		resp, err := app.Test(httptest.NewRequest(tc.method, tc.path, nil))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		var events []model.AuditEvent
		if err := db.Where("request_id = ?", resp.Header.Get("X-Request-ID")).Find(&events).Error; err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 || len(events) != 1 || events[0].Action != "session.login" || events[0].Reason != tc.reason || events[0].ActorID != admin.ID {
			t.Fatalf("login audit changed: status=%d events=%+v", resp.StatusCode, events)
		}
	}
}
