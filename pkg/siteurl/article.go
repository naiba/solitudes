// Package siteurl keeps public article paths and reserved system paths consistent
// across publishing, HTML routes, redirects and machine-readable representations.
package siteurl

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxPathLength = 2048

func IsSystemPath(path string) bool {
	// Match Fiber's ASCII-only route folding, not Unicode lookalikes (for
	// example Kelvin sign K is not the fixed route letter K).
	path = strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, strings.TrimRight(path, "/"))
	if path == "" {
		return true
	}
	first := strings.Split(strings.TrimPrefix(path, "/"), "/")[0]
	switch first {
	case "admin", "account", "auth", "oidc", "api", "r", "static", "upload", "read", "users", "posts", "books", "tags", "search", "readers", "feed", "captcha", "login", "logout", "register", "resend-verification", "verify-email", ".well-known", "authorize", "oauth", "userinfo", "revoke", "end_session", "keys", "healthz", "ready", "robots.txt", "sitemap.xml", "llms.txt", "favicon.ico", "logo.png":
		return true
	}
	return false
}

func ArticlePath(slug string) string { return "/" + url.PathEscape(slug) }

func validArticleSegment(slug string) bool {
	return slug != "" && utf8.ValidString(slug) && slug != "." && slug != ".." &&
		!strings.ContainsAny(slug, "/\\") && !strings.ContainsFunc(slug, unicode.IsControl)
}

// Publishing stores the literal slug, not an escaped URL. Unicode and symbols
// such as +, %, ? and # are safe when encoded at the URL boundary; rejecting all
// non-ASCII characters would unnecessarily break existing article addresses.
func ValidateArticleSlug(slug string) error {
	if !validArticleSegment(slug) || strings.TrimSpace(slug) != slug || len(ArticlePath(slug)) > MaxPathLength {
		return errors.New("article slug must be a nonempty single URL segment without slashes or control characters, at most 2048 encoded path bytes")
	}
	if IsSystemPath("/" + slug) {
		return errors.New("article slug conflicts with a system path")
	}
	return nil
}

// DecodeArticleSlug applies exactly one path decoding, never query decoding:
// '+' is a literal plus, and a stored '%2F' must not become a slash on a second
// decode. Fiber's UnescapePath remains disabled to preserve route boundaries.
func DecodeArticleSlug(raw string) (string, error) {
	slug, err := url.PathUnescape(raw)
	if err != nil {
		return "", err
	}
	if !validArticleSegment(slug) {
		return "", errors.New("invalid article path segment")
	}
	return slug, nil
}
