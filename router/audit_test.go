package router

import (
	"strings"
	"testing"
)

func TestAuditIdentifiersAndActions(t *testing.T) {
	for _, value := range []string{"unsafe\nvalue", "?token=secret", strings.Repeat("x", 65), "<script>"} {
		if auditIdentifier(value) != "" {
			t.Fatalf("unsafe audit identifier accepted: %q", value)
		}
	}
	for _, value := range []string{"client-1", "12345678-1234-1234-1234-123456789012", "legacy_app"} {
		if auditIdentifier(value) != value {
			t.Fatalf("valid ID rejected: %s", value)
		}
	}
	for _, tc := range []struct{ method, path, want string }{{"POST", "/login", "session.login"}, {"POST", "/account/password", "account.password.update"}, {"POST", "/admin/users/id/role", "user.role.update"}, {"POST", "/account/oidc/clients/id/disable", "client.disable"}, {"POST", "/oauth/token", "oidc.request"}, {"GET", "/auth/github/callback", "identity.callback"}, {"GET", "/static/site/cactus/js/main.js", ""}, {"POST", "/api/count", ""}} {
		if got := auditAction(tc.method, tc.path); got != tc.want {
			t.Errorf("%s %s: %s", tc.method, tc.path, got)
		}
	}
}

func TestRoutineAuditSuccessHasNarrowScope(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		status       int
		want         bool
	}{
		{"GET", "/admin/audit", 200, true},
		{"GET", "/authorize", 302, true},
		{"GET", "/authorize/callback", 302, true},
		{"POST", "/oauth/token", 200, true},
		{"POST", "/oauth/introspect", 200, true},
		{"GET", "/userinfo", 200, true},
		{"POST", "/userinfo", 200, true},
		{"POST", "/auth/passkey/login/begin", 200, true},
		{"POST", "/account/passkeys/begin", 200, true},
		{"POST", "/account/passkeys/begin", 302, false},
		{"POST", "/account/passkeys/finish", 201, false},
		{"DELETE", "/account/passkeys/begin", 200, false},
		{"POST", "/auth/passkey/login/finish", 200, false},
		{"POST", "/oidc/consent", 302, false},
		{"POST", "/revoke", 200, false},
		{"GET", "/end_session", 302, false},
		{"POST", "/login", 302, false},
		{"POST", "/admin/settings", 200, false},
		{"POST", "/account/password", 200, false},
		{"GET", "/auth/github/callback", 302, false},
		{"GET", "/verify-email", 302, false},
		{"GET", "/admin/audit-export", 200, false},
	} {
		if got := routineAuditSuccess(tc.method, tc.path, tc.status); got != tc.want {
			t.Errorf("%s %s (%d): skip=%v, want %v", tc.method, tc.path, tc.status, got, tc.want)
		}
		for _, failure := range []int{400, 401, 403, 404, 429, 500, 503} {
			if routineAuditSuccess(tc.method, tc.path, failure) {
				t.Errorf("failure suppressed: %s %s (%d)", tc.method, tc.path, failure)
			}
		}
	}
}

func TestAuditDeniedResourceDoesNotStoreRawPaths(t *testing.T) {
	route, target, client := auditResource("/admin/oidc/clients/application-1/disable")
	if route != "/admin/oidc/clients/:id/disable" || target != "application-1" || client != target {
		t.Fatalf("missing denied resource: %s %s %s", route, target, client)
	}
	for _, path := range []string{"/admin/users/<script>", "/admin/oidc/clients/" + strings.Repeat("s", 100), "/admin/users/id/unknown-operation"} {
		r, targ, cl := auditResource(path)
		if strings.Contains(r, "<script>") || strings.Contains(r, strings.Repeat("s", 100)) || targ != "" || cl != "" {
			t.Fatalf("raw path leaked: %s %s %s", r, targ, cl)
		}
	}
}
