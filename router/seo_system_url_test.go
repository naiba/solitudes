package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCanonicalSystemURLRedirect(t *testing.T) {
	for _, tc := range []struct {
		path, target, route, dynamic string
	}{
		{"/Posts", "/posts/", "/posts/:page?/", ""},
		{"/POSTS/001", "/posts/", "/posts/:page?/", ""},
		{"/POSTS/1/", "/posts/", "/posts/:page?/", ""},
		{"/Books/002///", "/books/2/", "/books/:page?/", "2"},
		{"/Tags/Topic/002", "/tags/Topic/2/", "/tags/:tag/:page?/", "Topic|2"},
		{"/TAGS/Topic/001/", "/tags/Topic/", "/tags/:tag/:page?/", "Topic"},
		{"/Tags/C%23%2FGo%3F/002", "/tags/C%23%2FGo%3F/2/", "/tags/:tag/:page?/", "C%23%2FGo%3F|2"},
		{"/Tags/001", "/tags/001/", "/tags/:tag/:page?/", "001"},
		{"/TAGS", "/tags/", "/tags/", ""},
		{"/Readers", "/readers/", "/readers/", ""},
		{"/Search///?q=Mixed+Case&next=%2FPosts&tag=Topic&tag=Other", "/search/?q=Mixed+Case&next=%2FPosts&tag=Topic&tag=Other", "/search/", ""},
		{"/ROBOTS.TXT/", "/robots.txt", "/robots.txt", ""},
		{"/SITEMAP.XML", "/sitemap.xml", "/sitemap.xml", ""},
		{"/LLMS.TXT", "/llms.txt", "/llms.txt", ""},
		{"/FAVICON.ICO", "/favicon.ico", "/favicon.ico", ""},
		{"/LOGO.PNG", "/logo.png", "/logo.png", ""},
		{"/Login/?next=%2FAdmin", "/login?next=%2FAdmin", "/login", ""},
		{"/REGISTER", "/register", "/register", ""},
		{"/VERIFY-EMAIL?token=AbC", "/verify-email?token=AbC", "/verify-email", ""},
		{"/CAPTCHA", "/captcha", "/captcha", ""},
		{"/FEED", "/feed", "/feed/:format?", ""},
		{"/FEED/RSS", "/feed/RSS", "/feed/:format?", "RSS"},
		{"/Users/AbCd-1234/", "/users/AbCd-1234", "/users/:id", "AbCd-1234"},
		{"/READ/AbCd-1234/ARTICLE.MD", "/read/AbCd-1234/article.md", "/read/:id/article.md", "AbCd-1234"},
		{"/R/GO?url=AbCd", "/r/go?url=AbCd", "/r/go", ""},
		{"/R/AbCd_Token", "/r/AbCd_Token", "/r/:token", "AbCd_Token"},
		{"/STATIC/I/AbCd_Token", "/static/i/AbCd_Token", "/static/i/:token", "AbCd_Token"},
		{"/STATIC/site/cactus/Images/Logo.PNG", "/static/site/cactus/Images/Logo.PNG", "/static/:kind/:theme/*", "site|cactus|Images/Logo.PNG"},
		{"/UPLOAD/Photos/Portrait.JPG", "/upload/Photos/Portrait.JPG", "/upload/*", "Photos/Portrait.JPG"},
		{"/ACCOUNT", "/account", "/account", ""},
		{"/ACCOUNT/OIDC/CLIENTS", "/account/oidc/clients", "/account/oidc/clients", ""},
		{"/ACCOUNT/OIDC/AUTHORIZATIONS", "/account/oidc/authorizations", "/account/oidc/authorizations", ""},
		{"/OIDC/CONSENT", "/oidc/consent", "/oidc/consent", ""},
		{"/AUTH/GitHub", "/auth/GitHub", "/auth/:provider", "GitHub"},
		{"/AUTH/GitHub/CALLBACK", "/auth/GitHub/callback", "/auth/:provider/callback", "GitHub"},
		{"/ADMIN/", "/admin", "/admin/", ""},
		{"/ADMIN/USERS", "/admin/users", "/admin/users", ""},
		{"/ADMIN/USERS/AbCd", "/admin/users/AbCd", "/admin/users/:id", "AbCd"},
		{"/ADMIN/AUDIT", "/admin/audit", "/admin/audit", ""},
		{"/ADMIN/AUTH/PROVIDERS", "/admin/auth/providers", "/admin/auth/providers", ""},
		{"/ADMIN/OIDC/CLIENTS", "/admin/oidc/clients", "/admin/oidc/clients", ""},
		{"/ADMIN/OIDC/CLIENTS/AbCd", "/admin/oidc/clients/AbCd", "/admin/oidc/clients/:id", "AbCd"},
		{"/ADMIN/PUBLISH", "/admin/publish", "/admin/publish", ""},
		{"/ADMIN/COMMENTS", "/admin/comments", "/admin/comments", ""},
		{"/ADMIN/ARTICLES", "/admin/articles", "/admin/articles", ""},
		{"/ADMIN/MEDIA", "/admin/media", "/admin/media", ""},
		{"/ADMIN/SETTINGS", "/admin/settings", "/admin/settings", ""},
		{"/ADMIN/REDIRECTS", "/admin/redirects", "/admin/redirects", ""},
		{"/ADMIN/TAGS", "/admin/tags", "/admin/tags", ""},
		{"/ADMIN/API/SEARCH-TAGS", "/admin/api/search-tags", "/admin/api/search-tags", ""},
		{"/ADMIN/API/SEARCH-BOOKS", "/admin/api/search-books", "/admin/api/search-books", ""},
		{"/ADMIN/THEME/PREVIEW/site/MyTheme", "/admin/theme/preview/site/MyTheme", "/admin/theme/preview/:kind/:name", "site|MyTheme"},
		{"/API/COMMENTER-INFO", "/api/commenter-info", "/api/commenter-info", ""},
		{"/MixedCaseArticle/", "/MixedCaseArticle", "/:slug/:version?", "MixedCaseArticle"},
		{"/MixedCaseArticle/AbCd/", "/MixedCaseArticle/AbCd", "/:slug/:version?", "MixedCaseArticle|AbCd"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			app := newTestRouter()
			app.Use(canonicalURLRedirect)
			app.Get(tc.route, func(writer http.ResponseWriter, httpRequest *http.Request) error {
				var values []string
				for _, name := range requestRoute(httpRequest).Params {
					if value := routeParam(httpRequest, name); value != "" {
						values = append(values, value)
					}
				}
				return writeText(writer, http.StatusOK, strings.Join(values, "|"))
			})
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				response, err := testRequest(app, httptest.NewRequest(method, tc.path, nil))
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				if response.StatusCode != http.StatusMovedPermanently || response.Header.Get("Location") != tc.target {
					t.Fatalf("%s status=%d location=%q; want 301 %q", method, response.StatusCode, response.Header.Get("Location"), tc.target)
				}
				response, err = testRequest(app, httptest.NewRequest(method, tc.target, nil))
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != http.StatusOK || response.Header.Get("Location") != "" {
					t.Fatalf("canonical target must resolve in one hop: status=%d location=%q", response.StatusCode, response.Header.Get("Location"))
				}
				if method == http.MethodGet && string(body) != tc.dynamic {
					t.Fatalf("dynamic route values=%q; want %q", body, tc.dynamic)
				}
			}
		})
	}
}

func TestCanonicalSystemURLPreservesDynamicCase(t *testing.T) {
	for _, path := range []string{
		"/MixedCaseArticle", "/MixedCaseArticle/AbCd", "/MixedCaseArticle/compare/AbCd",
		"/tags/Topic/", "/tags/001/", "/users/AbCd", "/r/AbCd_Token",
		"/static/site/cactus/Images/Logo.PNG", "/upload/Photos/Portrait.JPG",
		"/read/AbCd/article.md", "/auth/GitHub/callback", "/feed/RSS",
		"/PostsArticle", "/Unrelated/LOGIN", "/static/Site/MyTheme/Logo.PNG",
	} {
		t.Run(path, func(t *testing.T) {
			app := newTestRouter()
			app.Use(canonicalURLRedirect)
			app.Get("/*", func(writer http.ResponseWriter, httpRequest *http.Request) error {
				return writeText(writer, http.StatusOK, requestPath(httpRequest))
			})
			response, err := testRequest(app, httptest.NewRequest(http.MethodGet, path, nil))
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusOK || response.Header.Get("Location") != "" || string(body) != path {
				t.Fatalf("dynamic path changed: status=%d location=%q body=%q", response.StatusCode, response.Header.Get("Location"), body)
			}
		})
	}
}

func TestCanonicalSystemURLPreservesWriteMethodAndBody(t *testing.T) {
	app := newTestRouter()
	app.Use(canonicalURLRedirect)
	app.Post("/login", func(writer http.ResponseWriter, httpRequest *http.Request) error {
		return writeText(writer, http.StatusOK, httpRequest.Method+"|"+requestPath(httpRequest)+"|"+string(requestBody(httpRequest)))
	})
	response, err := testRequest(app, httptest.NewRequest(http.MethodPost, "/LOGIN/", strings.NewReader("password=MixedCase")))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Location") != "" || string(body) != "POST|/LOGIN/|password=MixedCase" {
		t.Fatalf("write changed: status=%d location=%q body=%q", response.StatusCode, response.Header.Get("Location"), body)
	}
}

func TestCanonicalSystemPathPreservesNonASCIISlugs(t *testing.T) {
	// System routes fold ASCII only; Unicode lookalikes remain article slugs.
	for _, path := range []string{"/Poſts", "/BOOKS", "/Regiſter"} {
		if target := canonicalPath(path); target != path {
			t.Errorf("article slug %q changed to %q", path, target)
		}
	}
}

func TestCanonicalSystemPathIsIdempotentAndLocal(t *testing.T) {
	for _, path := range []string{"/Posts", "/POSTS/001", "/Tags/Topic/002", "/ADMIN/USERS/AbCd/", "/READ/AbCd/ARTICLE.MD", "/MixedCaseArticle/", "/", "", "//", "///", "//EXAMPLE.test/Posts"} {
		target := canonicalPath(path)
		if again := canonicalPath(target); again != target {
			t.Errorf("not idempotent: %q -> %q -> %q", path, target, again)
		}
		if target != path && strings.HasPrefix(target, "//") {
			t.Errorf("created protocol-relative redirect: %q -> %q", path, target)
		}
	}
}
