package router

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func TestLoginProviderConfigurationValidation(t *testing.T) {
	config := &model.Config{}
	form := loginProviderForm{GitHubID: "github-client", GitHubSecret: "first-secret",
		OIDCID: "oidc-client", OIDCSecret: "oidc-secret", OIDCIssuer: "https://login.example.com/issuer/"}
	if err := applyLoginProviderForm(config, form); err != nil {
		t.Fatal(err)
	}
	if config.Auth.OIDC.Issuer != "https://login.example.com/issuer" {
		t.Fatalf("OIDC issuer not normalized: %s", config.Auth.OIDC.Issuer)
	}
	form.GitHubSecret, form.OIDCSecret = "", ""
	if err := applyLoginProviderForm(config, form); err != nil || config.Auth.GitHub.ClientSecret != "first-secret" {
		t.Fatalf("blank secret should preserve saved credential: %v", err)
	}
	for _, tc := range []struct {
		name string
		form loginProviderForm
	}{
		{"changed ID without new secret", loginProviderForm{GitHubID: "other-client"}},
		{"secret without ID", loginProviderForm{GitHubSecret: "secret"}},
		{"insecure issuer", loginProviderForm{OIDCID: "oidc-client", OIDCIssuer: "http://identity.example.com"}},
		{"issuer with credentials", loginProviderForm{OIDCID: "oidc-client", OIDCIssuer: "https://user:pass@identity.example.com"}},
		{"issuer with query", loginProviderForm{OIDCID: "oidc-client", OIDCIssuer: "https://identity.example.com/?redirect=evil"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := *config
			if err := applyLoginProviderForm(&candidate, tc.form); err == nil {
				t.Fatal("accepted invalid login provider configuration")
			}
			if config.Auth.GitHub.ClientSecret != "first-secret" {
				t.Fatal("invalid input changed running configuration")
			}
		})
	}
	form.GitHubDisable = true
	form.OIDCDisable = true
	if err := applyLoginProviderForm(config, form); err != nil {
		t.Fatal(err)
	}
	if config.Auth.GitHub.ClientID != "" || config.Auth.GitHub.ClientSecret != "" || config.Auth.OIDC.Issuer != "" {
		t.Fatal("disabling a provider retained credentials")
	}
}

func TestLoginProviderSettingsRestrictedToAdministrators(t *testing.T) {
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	previousLookup := sessionLookup
	t.Cleanup(func() { sessionLookup = previousLookup })
	sessionLookup = func(token string) (*model.Account, error) {
		switch token {
		case "admin":
			return &model.Account{Role: model.RoleAdmin}, nil
		case "editor":
			return &model.Account{Role: model.RoleEditor}, nil
		case "reader":
			return &model.Account{Role: model.RoleUser}, nil
		}
		return nil, errNoAccount
	}
	path := filepath.Join(t.TempDir(), "conf.yml")
	solitudes.System = &solitudes.SysVariable{Config: &model.Config{ConfigFilePath: path}}
	app := fiber.New()
	app.Use(auth, csrfGuard)
	app.Post("/admin/auth/providers", loginRequired, requireAdmin, saveLoginProviders)
	form := url.Values{"github_id": {"github-client"}, "github_secret": {"secret"}}
	for _, tc := range []struct {
		token string
		want  int
	}{{"", http.StatusFound}, {"reader", http.StatusForbidden}, {"editor", http.StatusForbidden}, {"admin", http.StatusSeeOther}} {
		req := httptest.NewRequest(http.MethodPost, "/admin/auth/providers", strings.NewReader(form.Encode()))
		req.Host = "blog.example.com"
		req.Header.Set("Host", "blog.example.com")
		req.Header.Set("Origin", "http://blog.example.com")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if tc.token != "" {
			req.AddCookie(&http.Cookie{Name: solitudes.AuthCookie, Value: tc.token})
		}
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Fatalf("role %q: got %d, want %d", tc.token, resp.StatusCode, tc.want)
		}
	}
	if solitudes.System.Config.Auth.GitHub.ClientSecret != "secret" {
		t.Fatal("administrator could not configure the provider")
	}
	contents, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(contents), "secret") {
		t.Fatalf("provider config was not saved: %v", err)
	}
	invalid := url.Values{"github_id": {"new-id"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/auth/providers", strings.NewReader(invalid.Encode()))
	req.Host = "blog.example.com"
	req.Header.Set("Host", "blog.example.com")
	req.Header.Set("Origin", "http://blog.example.com")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: solitudes.AuthCookie, Value: "admin"})
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || solitudes.System.Config.Auth.GitHub.ClientID != "github-client" {
		t.Fatalf("invalid update status=%d client_id=%q", resp.StatusCode, solitudes.System.Config.Auth.GitHub.ClientID)
	}
}
