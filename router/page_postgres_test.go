package router

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func TestPostgresStandalonePageSemanticsAndEditPermission(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.Comment{}); err != nil {
		t.Fatal(err)
	}
	owner := model.Account{Email: "page-owner@example.test", Role: model.RoleEditor}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	a := model.Article{AuthorID: &owner.ID, Slug: "about", Title: "About this site", Content: "# Custom Markdown heading\n\nA standalone page.", TemplateID: solitudes.PageTemplateID, Version: 1}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	t.Chdir("..")
	previous, engine, translations := solitudes.System, globalDynamicEngine.engine, translator.Trans
	t.Cleanup(func() {
		solitudes.System, globalDynamicEngine.engine, translator.Trans = previous, engine, translations
	})
	for _, theme := range []string{"cactus", "folio"} {
		t.Run(theme, func(t *testing.T) {
			conf := &model.Config{}
			conf.Site.Theme, conf.Admin.Theme = theme, "default"
			solitudes.System = &solitudes.SysVariable{Config: conf, DB: db}
			if err := LoadTemplates(); err != nil {
				t.Fatal(err)
			}
			var viewer *model.Account
			app := fiber.New(fiber.Config{Views: globalDynamicEngine})
			app.Use(trans)
			app.Use(func(c *fiber.Ctx) error { c.Locals(solitudes.CtxAccount, viewer); return c.Next() })
			app.Get("/:slug", article)
			for _, tc := range []struct {
				name    string
				account *model.Account
				edit    bool
			}{
				{"guest", nil, false},
				{"member", &model.Account{Role: model.RoleUser}, false},
				{"demoted-owner", &model.Account{ID: owner.ID, Role: model.RoleUser}, false},
				{"other-editor", &model.Account{Role: model.RoleEditor}, false},
				{"owner-editor", &owner, true},
				{"admin", &model.Account{Role: model.RoleAdmin}, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					viewer = tc.account
					resp, err := app.Test(httptest.NewRequest("GET", "/about", nil), -1)
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					if resp.StatusCode != 200 {
						t.Fatalf("status %d", resp.StatusCode)
					}
					doc, err := goquery.NewDocumentFromReader(resp.Body)
					if err != nil {
						t.Fatal(err)
					}
					if label, _ := doc.Find(`[data-testid="site-page"]`).Attr("aria-label"); label != a.Title {
						t.Fatal("missing accessible page name")
					}
					if doc.Find(`[data-testid="site-page"] h1`).Length() != 1 || doc.Find(`[data-reading-content] h1`).Text() != "Custom Markdown heading" {
						t.Fatal("page must preserve Markdown headings without injecting its own title")
					}
					for _, selector := range []string{`[data-testid="site-article"]`, `[data-testid="article-byline"]`, `[data-share-open]`, `#header-post`, `#article-share-dialog`, `meta[property^="article:"]`} {
						if doc.Find(selector).Length() != 0 {
							t.Errorf("unexpected article UI: %s", selector)
						}
					}
					if value, _ := doc.Find(`meta[property="og:type"]`).Attr("content"); value != "website" {
						t.Errorf("og:type = %q", value)
					}
					if (doc.Find(`[data-testid="page-edit-link"]`).Length() == 1) != tc.edit {
						t.Errorf("wrong edit permission for %s", tc.name)
					}
					if doc.Find(`[data-testid="comment-form"]`).Length() != 1 {
						t.Fatal("missing page comments")
					}
				})
			}
			created := time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)
			for _, tc := range []struct {
				name       string
				updated    time.Time
				showUpdate bool
			}{
				{"unchanged", created, false},
				{"same-minute", created.Add(30 * time.Second), false},
				{"same-day-edit", created.Add(10 * time.Minute), true},
				{"later-day-edit", created.Add(48 * time.Hour), true},
				{"missing-update", time.Time{}, false},
				{"older-update", created.Add(-time.Hour), false},
			} {
				t.Run("dates/"+tc.name, func(t *testing.T) {
					if err := db.Model(&a).UpdateColumns(map[string]interface{}{"created_at": created, "updated_at": tc.updated}).Error; err != nil {
						t.Fatal(err)
					}
					resp, err := app.Test(httptest.NewRequest("GET", "/about", nil), -1)
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					if resp.StatusCode != 200 {
						t.Fatalf("status %d", resp.StatusCode)
					}
					doc, err := goquery.NewDocumentFromReader(resp.Body)
					if err != nil {
						t.Fatal(err)
					}
					stamp, _ := doc.Find(`[data-testid="page-created-at"]`).Attr("datetime")
					if stamp != created.Format(time.RFC3339) {
						t.Errorf("created datetime = %q", stamp)
					}
					updated := doc.Find(`[data-testid="page-updated-at"]`)
					if (updated.Length() == 1) != tc.showUpdate {
						t.Errorf("incorrect update visibility")
					}
					if tc.showUpdate {
						stamp, _ = updated.Attr("datetime")
						if stamp != tc.updated.Format(time.RFC3339) {
							t.Errorf("updated datetime = %q", stamp)
						}
					}
				})
			}
		})
	}
}

func TestPostgresArticleNeighborsExcludeStandalonePages(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}); err != nil {
		t.Fatal(err)
	}
	previous := solitudes.System
	solitudes.System = &solitudes.SysVariable{DB: db}
	t.Cleanup(func() { solitudes.System = previous })
	var entries []model.Article
	for i, templateID := range []byte{solitudes.ArticleTemplateID, solitudes.PageTemplateID, solitudes.ArticleTemplateID, solitudes.PageTemplateID, solitudes.ArticleTemplateID} {
		a := model.Article{Slug: fmt.Sprintf("neighbor-%d", i), Title: "Neighbor", TemplateID: templateID, CreatedAt: time.Now().Add(time.Duration(i) * time.Hour)}
		if err := db.Create(&a).Error; err != nil {
			t.Fatal(err)
		}
		entries = append(entries, a)
	}
	relatedSiblingArticle(&entries[2], nil)
	if got := entries[2].SibilingArticle; got.Prev.ID != entries[0].ID || got.Next.ID != entries[4].ID {
		t.Fatalf("wrong article neighbors: %+v", got)
	}
}
