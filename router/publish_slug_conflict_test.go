//go:build !postgres_test

package router

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

// Publishing a new article with a slug that is already taken used to be
// treated as an edit of the existing article: its text was replaced without a
// history entry and without the author having opened it.
func TestPublishNewArticleRefusesSlugOfExistingArticle(t *testing.T) {
	db := newIdentityTestDB(t)
	withIdentityDB(t, db)
	account := testAccount(t, db, model.RoleAdmin)
	if err := db.Exec("CREATE TABLE articles (id TEXT PRIMARY KEY, author_id TEXT, slug TEXT, title TEXT, content TEXT, template_id INTEGER, version INTEGER, created_at DATETIME, updated_at DATETIME, comment_num INTEGER, read_num INTEGER, visibility TEXT DEFAULT 'public', disable_comment BOOLEAN, tags TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO articles (id, author_id, slug, title, content, template_id, version) VALUES (?, ?, ?, ?, ?, ?, ?)", "10000000-0000-4000-8000-000000000020", account.ID, "taken-slug", "Existing", "existing text", 1, 3).Error; err != nil {
		t.Fatal(err)
	}
	app := newTestRouter()
	app.Use(func(writer http.ResponseWriter, httpRequest *http.Request) error {
		requestValue(httpRequest, solitudes.CtxAccount, &account)
		return nil
	})
	app.Post("/admin/publish", publishHandler)

	fields := url.Values{"id": {""}, "slug": {"taken-slug"}, "title": {"Brand new"}, "content": {"new text"}, "template": {"1"}, "visibility": {"public"}}
	request := httptest.NewRequest(http.MethodPost, "/admin/publish", strings.NewReader(fields.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := testRequest(app, request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409 Conflict", response.StatusCode)
	}
	var stored struct{ Title, Content string }
	if err := db.Raw("SELECT title, content FROM articles WHERE slug = ?", "taken-slug").Scan(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Title != "Existing" || stored.Content != "existing text" {
		t.Errorf("existing article was changed to %+v", stored)
	}
	var articles int64
	if err := db.Raw("SELECT COUNT(*) FROM articles").Scan(&articles).Error; err != nil {
		t.Fatal(err)
	}
	if articles != 1 {
		t.Errorf("articles after the refused publish = %d, want 1", articles)
	}
}
