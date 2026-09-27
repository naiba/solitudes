package router

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A shared browser selector must have the same meaning in every theme. These
// checks also catch template edits that would silently break Playwright tests.
func TestThemeInteractionSelectors(t *testing.T) {
	contracts := map[string]map[string][]string{
		"admin": {
			"header.html":                      {"admin-nav-publish", "admin-nav-content-menu", "admin-nav-identity-menu", "admin-nav-providers"},
			"login.html":                       {"auth-login-form", "auth-email", "auth-password", "auth-captcha", "auth-submit", "auth-register-link"},
			"login_providers.html":             {"provider-settings-form", "provider-github-id", "provider-github-secret", "provider-google-id", "provider-google-secret", "provider-oidc-issuer", "provider-oidc-id", "provider-oidc-secret", "provider-settings-save"},
			"register.html":                    {"register-form", "register-email", "register-nickname", "register-password", "register-captcha", "register-submit", "resend-form", "resend-email", "resend-captcha", "resend-submit"},
			"account.html":                     {"account-password-form", "account-current-password", "account-new-password", "account-password-submit", "account-passkey-add", "account-oidc-clients", "account-logout"},
			"account_oidc_clients.html":        {"oidc-client-form", "oidc-client-name", "oidc-client-redirects", "oidc-client-public", "oidc-client-submit"},
			"account_oidc_client_created.html": {"oidc-created-id", "oidc-created-back"},
			"publish.html":                     {"publish-title", "publish-slug", "publish-tags", "publish-template", "publish-content", "publish-private", "publish-submit"},
			"users.html":                       {"user-role-form", "user-role-select", "user-role-save"},
			"consent.html":                     {"oidc-consent-form", "oidc-consent-allow", "oidc-consent-deny"},
			"oidc_clients.html":                {"oidc-client-form", "oidc-client-name", "oidc-client-redirects", "oidc-client-public", "oidc-client-submit", "oidc-rotate-keys"},
			"oidc_client_created.html":         {"oidc-created-id", "oidc-created-back"},
			"articles.html":                    {"article-edit", "article-delete"},
		},
		"site": {
			"search.html":         {"site-search-input", "site-search-submit"},
			"article.html":        {"site-article", "comment-form", "comment-content", "comment-nickname", "comment-email", "comment-submit"},
			"page.html":           {"site-article", "comment-form", "comment-content", "comment-nickname", "comment-email", "comment-submit"},
			"comments_entry.html": {"comment-reply"},
		},
	}
	for kind, themes := range map[string][]string{"admin": {"default", "glacie"}, "site": {"cactus", "folio"}} {
		for _, theme := range themes {
			for template, selectors := range contracts[kind] {
				t.Run(kind+"/"+theme+"/"+template, func(t *testing.T) {
					file := template
					if kind == "site" && theme == "cactus" && file == "search.html" {
						file = "search_form.html" // shared by the home and search pages
					}
					path := filepath.Join("..", "resource", "themes", kind, theme, "templates", file)
					content, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					for _, selector := range selectors {
						target := `data-testid="` + selector + `"`
						if count := strings.Count(string(content), target); count != 1 {
							t.Errorf("%q occurs %d times; expected once", target, count)
						}
					}
					for _, match := range regexp.MustCompile(`data-testid="([^"]+)"`).FindAllSubmatch(content, -1) {
						if !regexp.MustCompile(`^[a-z][a-z0-9-]+$`).Match(match[1]) {
							t.Errorf("invalid data-testid %q", match[1])
						}
					}
				})
			}
		}
	}
}
