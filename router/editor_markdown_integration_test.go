//go:build !postgres_test

package router

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func TestEditorPublishHandlerRejectsExecutableMarkdown(t *testing.T) {
	db := newIdentityTestDB(t)
	withIdentityDB(t, db)
	account := testAccount(t, db, model.RoleEditor)
	if err := db.Exec(`CREATE TABLE articles (id TEXT PRIMARY KEY, author_id TEXT, slug TEXT, title TEXT, content TEXT, template_id INTEGER, version INTEGER, created_at DATETIME, updated_at DATETIME, comment_num INTEGER, read_num INTEGER, visibility TEXT DEFAULT 'public', disable_comment BOOLEAN, tags TEXT)`).Error; err != nil {
		t.Fatal(err)
	}
	articleID := "10000000-0000-4000-8000-000000000010"
	if err := db.Exec(`INSERT INTO articles (id, author_id, slug, title, content, template_id, version) VALUES (?, ?, ?, ?, ?, ?, ?)`, articleID, account.ID, "owned", "Owned", "original", 1, 1).Error; err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error { c.Locals(solitudes.CtxAccount, &account); return c.Next() })
	app.Post("/admin/publish", publishHandler)
	for _, tc := range []struct{ name, id, slug, content string }{
		{"new article", "", "new-article", `<script>alert(1)</script>`},
		{"existing article", articleID, "owned", `original\n[click](javascript:alert%281%29)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := url.Values{"id": {tc.id}, "slug": {tc.slug}, "title": {"Owned"}, "content": {tc.content}, "template": {"1"}}
			req := httptest.NewRequest(http.MethodPost, "/admin/publish", strings.NewReader(fields.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", resp.StatusCode)
			}
		})
	}
	if err := db.Exec("UPDATE articles SET content = ? WHERE id = ?", "before <script>alert(1)</script> after", articleID).Error; err != nil {
		t.Fatal(err)
	}
	fields := url.Values{"id": {articleID}, "slug": {"owned"}, "title": {"Owned"},
		"content": {"before <script>alert(2)</script> after"}, "template": {"1"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/publish", strings.NewReader(fields.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("inline script change status = %d, want 403", resp.StatusCode)
	}
	var content string
	if err := db.Raw("SELECT content FROM articles WHERE id = ?", articleID).Scan(&content).Error; err != nil || content != "before <script>alert(1)</script> after" {
		t.Fatalf("rejected update changed stored content: %q, %v", content, err)
	}
}
