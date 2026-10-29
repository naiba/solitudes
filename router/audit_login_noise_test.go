//go:build !postgres_test

package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/naiba/solitudes/internal/model"
)

func TestAuditLoginNavigationAndProtocolInteractions(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, protocolError, action string
		status                                    int
	}{
		{"password navigation", "POST", "/login", "", "", 302},
		{"passkey navigation", "POST", "/auth/passkey/login/finish", "", "", 302},
		{"password rejection", "POST", "/login", "", "session.login", 401},
		{"passkey rejection", "POST", "/auth/passkey/login/finish", "", "session.login", 400},
		{"login required", "GET", "/authorize", "login_required", "", 400},
		{"consent required", "GET", "/authorize", "consent_required", "", 400},
		{"interaction required", "GET", "/authorize", "interaction_required", "", 302},
		{"callback interaction", "GET", "/authorize/callback", "interaction_required", "", 400},
		{"invalid request", "GET", "/authorize", "invalid_request", "oidc.authorize", 302},
		{"denied consent", "GET", "/authorize", "access_denied", "oidc.authorize", 302},
		{"unexpected token error", "POST", "/oauth/token", "login_required", "oidc.request", 400},
		{"unexpected forbidden", "GET", "/authorize", "login_required", "oidc.authorize", 403},
		{"server failure", "GET", "/authorize", "login_required", "request.error", 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newIdentityTestDB(t)
			withIdentityDB(t, db)
			app := newTestRouter()
			app.Use(auditMiddleware)
			handler := func(writer http.ResponseWriter, httpRequest *http.Request) error {
				return redirectResponse(writer, httpRequest, "/account", tc.status)
			}
			if tc.protocolError != "" {
				handler = oidcHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if tc.status == 302 {
						w.Header().Set("Location", "http://localhost/callback?error="+tc.protocolError)
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(`{"error":"` + tc.protocolError + `"}`))
				}))
			}
			app.Add(tc.method, tc.path, handler)
			resp, err := testRequest(app, httptest.NewRequest(tc.method, tc.path, nil))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.status {
				t.Fatalf("status=%d want=%d", resp.StatusCode, tc.status)
			}
			var events []model.AuditEvent
			if err := db.Find(&events).Error; err != nil {
				t.Fatal(err)
			}
			if tc.action == "" {
				if len(events) != 0 {
					t.Fatalf("routine interaction audited: %+v", events)
				}
			} else {
				if len(events) != 1 || events[0].Action != tc.action || events[0].Status != tc.status || events[0].Outcome == "success" {
					t.Fatalf("failure missing or incorrect: %+v", events)
				}
			}
		})
	}
}
