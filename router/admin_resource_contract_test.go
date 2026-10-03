package router

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultAdminDoesNotBundleFrontAccountPages(t *testing.T) {
	root := filepath.Join("..", "resource", "themes", "admin", "default")
	for _, path := range []string{
		"templates/login.html", "templates/register.html", "templates/account.html",
		"templates/account_oidc_clients.html", "templates/account_oidc_client_created.html",
		"templates/consent.html", "templates/oidc_application.html", "static/passkeys.js",
	} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Errorf("front-only resource must not be bundled in default admin: %s (stat: %v)", path, err)
		}
	}
	css, err := os.ReadFile(filepath.Join(root, "static", "solitudes.css"))
	if err != nil {
		t.Fatal(err)
	}
	for _, selector := range []string{".login-container", ".login-box", ".login-logo", ".captcha-container", ".oidc-app-info", ".oidc-scope-list"} {
		if strings.Contains(string(css), selector) {
			t.Errorf("front-only style remains in default admin: %s", selector)
		}
	}
}
