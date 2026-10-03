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
			"header.html":              {"admin-nav-publish", "admin-nav-content-menu", "admin-nav-identity-menu", "admin-nav-providers"},
			"login_providers.html":     {"provider-settings-form", "provider-github-id", "provider-github-secret", "provider-google-id", "provider-google-secret", "provider-oidc-issuer", "provider-oidc-id", "provider-oidc-secret", "provider-settings-save"},
			"publish.html":             {"publish-title", "publish-slug", "publish-tags", "publish-template", "publish-content", "publish-visibility", "publish-submit"},
			"users.html":               {"user-role-form", "user-role-select", "user-role-save"},
			"oidc_clients.html":        {"oidc-client-form", "oidc-client-name", "oidc-client-description", "oidc-client-homepage", "oidc-client-redirects", "oidc-client-public", "oidc-client-submit", "oidc-rotate-keys"},
			"client_detail.html":       {"oidc-client-edit-form", "oidc-client-edit-save", "client-audit-link", "client-login-count", "client-login-users"},
			"audit.html":               {"audit-filter-form", "audit-event", "audit-request-filter"},
			"oidc_client_created.html": {"oidc-created-id", "oidc-created-back"},
			"articles.html":            {"article-edit", "article-delete"},
		},
		"site": {
			"index.html":                       {"reader-circle-home", "reader-circle-more", "home-recent-comments", "home-recent-comment", "home-comment-link"},
			"login.html":                       {"site-auth-page", "auth-login-form", "auth-email", "auth-password", "auth-captcha", "auth-submit", "auth-register-link"},
			"register.html":                    {"site-auth-page", "register-form", "register-email", "register-nickname", "register-password", "register-captcha", "register-submit", "resend-form", "resend-email", "resend-captcha", "resend-submit"},
			"consent.html":                     {"site-auth-page", "oidc-consent-form", "oidc-consent-allow", "oidc-consent-deny"},
			"oidc_application.html":            {"oidc-application-info", "oidc-application-name", "oidc-application-description", "oidc-application-owner", "oidc-application-homepage"},
			"account.html":                     {"account-summary", "account-role", "account-workspace", "account-manage-articles", "account-compose", "account-admin-dashboard", "account-directory-listed", "account-password-form", "account-current-password", "account-new-password", "account-password-submit", "account-profile-form", "account-profile-nickname", "account-profile-bio", "account-profile-save", "account-public-profile", "account-passkey-add", "account-oidc-clients", "account-logout"},
			"account_oidc_clients.html":        {"oidc-client-form", "oidc-client-name", "oidc-client-description", "oidc-client-homepage", "oidc-client-redirects", "oidc-client-public", "oidc-client-submit", "oidc-client-edit-form", "oidc-client-edit-save"},
			"account_oidc_client_created.html": {"oidc-created-id", "oidc-created-back"},
			"search.html":                      {"site-search-input", "site-search-submit"},
			"article.html":                     {"site-article", "article-author-link", "article-edit-link", "comment-form", "comment-content", "comment-nickname", "comment-email", "comment-submit"},
			"page.html":                        {"site-article", "article-author-link", "article-edit-link", "comment-form", "comment-content", "comment-nickname", "comment-email", "comment-submit"},
			"article_list_entry.html":          {"article-author-link", "comment-author-link", "topic-entry", "topic-bubble", "topic-content", "topic-byline", "topic-comments", "topic-comment", "topic-comment-link"},
			"comments_entry.html":              {"comment-reply", "comment-author-link", "comment-role"},
			"user_profile.html":                {"public-profile", "public-profile-bio", "public-profile-article", "public-profile-comment", "public-profile-comment-article", "profile-reader-circle", "profile-edit-link"},
		},
	}
	for kind, themes := range map[string][]string{"admin": {"default"}, "site": {"cactus", "folio"}} {
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

func TestCactusHomeSecondaryContentPriority(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "resource", "themes", "site", "cactus", "templates", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(content)
	mostRead := strings.Index(html, `class="home-most-read-section"`)
	comments := strings.Index(html, `data-testid="home-recent-comments"`)
	readers := strings.Index(html, `data-testid="reader-circle-home"`)
	if mostRead < 0 || comments < 0 || readers < 0 || !(mostRead < comments && comments < readers) {
		t.Fatalf("Cactus secondary column must order most-read, recent comments, then reader circle")
	}
}

func TestSiteAccountNavigationContract(t *testing.T) {
	for theme, file := range map[string]string{"cactus": "menu.html", "folio": "header.html"} {
		content, err := os.ReadFile(filepath.Join("..", "resource", "themes", "site", theme, "templates", file))
		if err != nil {
			t.Fatal(err)
		}
		if count := strings.Count(string(content), `data-testid="site-account-nav"`); count != 1 {
			t.Errorf("%s account navigation count=%d", theme, count)
		}
		if strings.Contains(string(content), "site-manage-nav") || strings.Contains(string(content), `href="/admin/articles"`) {
			t.Errorf("%s top navigation must leave workspace access to the account center", theme)
		}
		for _, selector := range []string{"site-reader-circle-nav"} {
			if count := strings.Count(string(content), `data-testid="`+selector+`"`); count != 1 {
				t.Errorf("%s %s navigation count=%d", theme, selector, count)
			}
		}
		if theme == "folio" && strings.Count(string(content), `data-testid="site-account-nav-mobile"`) != 1 {
			t.Error("folio mobile account navigation missing")
		}
		if theme == "folio" {
			for _, selector := range []string{"site-reader-circle-nav-mobile"} {
				if count := strings.Count(string(content), `data-testid="`+selector+`"`); count != 1 {
					t.Errorf("folio %s navigation count=%d", selector, count)
				}
			}
		}
	}
}
