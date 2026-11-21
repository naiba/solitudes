//go:build !postgres_test

package router

import (
	"errors"
	"net/http"
	"testing"
)

// "Disable comments" used to hide the form in the templates only. The comment
// endpoint never looked at the flag, so anyone could still post by calling it
// directly.
func TestVerifyArticleRefusesCommentsOnArticleWithCommentsDisabled(t *testing.T) {
	db := newIdentityTestDB(t)
	withIdentityDB(t, db)
	if err := db.Exec("CREATE TABLE articles (id TEXT PRIMARY KEY, version INTEGER, title TEXT, slug TEXT, visibility TEXT, author_id TEXT, disable_comment BOOL, created_at datetime)").Error; err != nil {
		t.Fatal(err)
	}
	for slug, disabled := range map[string]bool{"comments-closed": true, "comments-open": false} {
		if err := db.Exec("INSERT INTO articles (id, version, title, slug, visibility, disable_comment, created_at) VALUES (?, 1, ?, ?, 'public', ?, CURRENT_TIMESTAMP)", slug, slug, slug, disabled).Error; err != nil {
			t.Fatal(err)
		}
	}

	_, err := verifyArticle(nil, &commentForm{Slug: "comments-closed", Version: 1})
	var refused *httpError
	if !errors.As(err, &refused) || refused.Code != http.StatusForbidden {
		t.Fatalf("comment on an article with comments disabled: err = %v, want HTTP 403", err)
	}

	article, err := verifyArticle(nil, &commentForm{Slug: "comments-open", Version: 1})
	if err != nil || article.ID != "comments-open" {
		t.Fatalf("comment on an ordinary article: article = %+v, err = %v", article, err)
	}
}

// Browsers strip tabs and newlines out of a URL before resolving it, so
// "/\t/evil.example" is navigated to as "//evil.example": another site.
func TestSafeReturnPathRejectsControlCharacters(t *testing.T) {
	for _, unsafe := range []string{
		"/\t/evil.example", "/\n/evil.example", "/\r/evil.example", "/\x00/evil.example", "/\x7f/evil.example",
		"//evil.example", "/\\evil.example", "https://evil.example", "",
	} {
		if got := safeReturnPath(unsafe); got != "" {
			t.Errorf("safeReturnPath(%q) = %q, want it rejected", unsafe, got)
		}
	}
	for _, safe := range []string{"/", "/admin/", "/posts/hello-world?page=2#comments", "/文章/你好"} {
		if got := safeReturnPath(safe); got != safe {
			t.Errorf("safeReturnPath(%q) = %q, want it kept", safe, got)
		}
	}
}
