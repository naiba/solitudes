package router

import (
	"net/http"
	"strconv"
	"strings"
)

func canonicalURLRedirect(writer http.ResponseWriter, httpRequest *http.Request) error {
	// SEO redirects are for reads only: a 301 on a form/API write can discard
	// the method and body. Let the normal routing and CSRF checks handle writes.
	if httpRequest.Method != http.MethodGet && httpRequest.Method != http.MethodHead {
		return nil
	}
	path := requestPath(httpRequest)
	target := canonicalPath(path)
	if target != path {
		if query := string(httpRequest.URL.RawQuery); query != "" {
			target += "?" + query
		}
		return redirectResponse(writer, httpRequest, target, http.StatusMovedPermanently)
	}
	return nil
}

// Mirror the GET route shapes in router.go, preserving registration order where
// a literal route precedes a parameter (r/go and static/i). Parameters and file
// wildcards deliberately remain opaque, including their original escaping.
var canonicalSystemRoutePatterns = [][]string{
	{"favicon.ico"}, {"logo.png"}, {"feed", ":format?"},
	{"posts", ":page?"}, {"books", ":page?"}, {"search"}, {"readers"},
	{"users", ":id"}, {"tags", ":tag", ":page?"}, {"tags"},
	{"r", "go"}, {"robots.txt"}, {"sitemap.xml"}, {"llms.txt"},
	{"read", ":id", "article.md"}, {"captcha"}, {"api", "commenter-info"},
	{"r", ":token"}, {"static", "i", ":token"},
	{"static", ":kind", ":theme", "*"}, {"upload", "*"},
	{"login"}, {"register"}, {"verify-email"}, {"account"},
	{"account", "oidc", "clients"}, {"account", "oidc", "authorizations"},
	{"oidc", "consent"}, {"auth", ":provider", "callback"}, {"auth", ":provider"},
	{"admin"}, {"admin", "users"}, {"admin", "users", ":id"},
	{"admin", "audit"}, {"admin", "auth", "providers"},
	{"admin", "oidc", "clients"}, {"admin", "oidc", "clients", ":id"},
	{"admin", "publish"}, {"admin", "comments"}, {"admin", "articles"},
	{"admin", "media"}, {"admin", "settings"}, {"admin", "redirects"}, {"admin", "tags"},
	{"admin", "api", "search-tags"}, {"admin", "api", "search-books"},
	{"admin", "theme", "preview", ":kind", ":name"},
}

func canonicalSystemPathParts(parts []string) {
	// System route literals match case-insensitively, but crawlers do not.
	// Lowercasing the whole path would corrupt case-sensitive slugs, tags,
	// identity tokens and asset names; normalize only a matched route's literals.
	// Use ASCII folding, not Unicode folding (e.g. ſ must not become s).
	for _, pattern := range canonicalSystemRoutePatterns {
		if len(parts) > len(pattern) && pattern[len(pattern)-1] != "*" {
			continue
		}
		matched := true
		for i, segment := range pattern {
			if segment == "*" {
				break
			}
			if i >= len(parts) {
				matched = strings.HasSuffix(segment, "?")
				break
			}
			if strings.HasPrefix(segment, ":") {
				if parts[i] == "" {
					matched = false
					break
				}
			} else if !equalFoldRouteLiteral(parts[i], segment) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		for i, segment := range pattern {
			if i >= len(parts) || segment == "*" {
				break
			}
			if !strings.HasPrefix(segment, ":") {
				parts[i] = segment
			}
		}
		return
	}
}

// Redirect keys and destinations must use the same path normalization as the
// system middleware, otherwise a rule can redirect straight back to itself.
func canonicalPath(path string) string {
	if len(path) <= 1 || strings.HasPrefix(path, "//") {
		return path
	}
	target := strings.TrimRight(path, "/")
	parts := strings.Split(strings.TrimPrefix(target, "/"), "/")
	canonicalSystemPathParts(parts)
	target = strings.Join(parts, "/")
	if strings.HasPrefix(path, "/") {
		target = "/" + target
	}
	list := target == "/search" || target == "/readers" || target == "/tags" ||
		parts[0] == "posts" || parts[0] == "books" || strings.HasPrefix(target, "/tags/")
	// Archive page one and zero-padded page numbers represent the same content.
	// Match route shapes, not numeric tags, and keep escaped tags intact.
	archivePage := (len(parts) == 2 && (parts[0] == "posts" || parts[0] == "books")) ||
		(len(parts) == 3 && parts[0] == "tags")
	if archivePage {
		if page, err := listPage(parts[len(parts)-1]); err == nil {
			if page == 1 {
				parts = parts[:len(parts)-1]
			} else {
				parts[len(parts)-1] = strconv.Itoa(page)
			}
			target = "/" + strings.Join(parts, "/")
		}
	}
	if list {
		target += "/"
	}
	return target
}
