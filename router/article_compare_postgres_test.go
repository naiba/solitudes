package router

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/content"
	"github.com/naiba/solitudes/pkg/translator"
)

func TestComparisonVersionValidation(t *testing.T) {
	for _, s := range []string{"", "1", "v0", "v01", "v-1", "v+1", "v3", "v99999999999999999999999", "v1/../../admin", "v1...v2"} {
		if _, ok := comparisonVersion(s, 2); ok {
			t.Errorf("accepted version %q", s)
		}
	}
	for _, s := range []string{"v1", "v2"} {
		if _, ok := comparisonVersion(s, 2); !ok {
			t.Errorf("rejected %s", s)
		}
	}
}

func TestPostgresArticleComparisonSEOAndRenderedContent(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.ArticleHistory{}); err != nil {
		t.Fatal(err)
	}
	a := model.Article{Title: "Current title", Slug: "compare-story", Version: 2, Content: "## Same heading\n\n**New prose**\n\n| A | B |\n|---|---|\n| 1 | 2 |\n\n<script>window.diffXSS=1</script>", TemplateID: solitudes.ArticleTemplateID}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ArticleHistory{ArticleID: a.ID, Title: "Old title", Version: 1, Content: "## Same heading\n\n*Old prose*\n\n<script>window.diffXSS=2</script>"}).Error; err != nil {
		t.Fatal(err)
	}
	t.Chdir("..")
	previous, engine, translations := solitudes.System, globalDynamicEngine.engine, translator.Trans
	t.Cleanup(func() {
		solitudes.System, globalDynamicEngine.engine, translator.Trans = previous, engine, translations
	})
	for _, theme := range []string{"cactus", "folio"} {
		cfg := &model.Config{}
		cfg.Site.Theme, cfg.Admin.Theme, cfg.Site.Domain = theme, "default", "example.test"
		solitudes.System = &solitudes.SysVariable{DB: db, Config: cfg}
		if err := LoadTemplates(); err != nil {
			t.Fatal(err)
		}
		app := fiber.New(fiber.Config{Views: globalDynamicEngine})
		app.Use(trans)
		app.Get("/:slug/compare/:versions?", articleCompare)
		for _, tc := range []struct {
			path   string
			status int
		}{
			{"/compare-story/compare/v1...v2", 200}, {"/compare-story/compare/v2...v1", 200}, {"/compare-story/compare/v1...v1", 200},
			{"/compare-story/compare", 303}, {"/compare-story/compare?from=1&to=2", 303},
			{"/compare-story/compare?from=01&to=2", 404}, {"/compare-story/compare/v0...v2", 404}, {"/compare-story/compare/v1...v3", 404},
			{"/compare-story/compare/v1..v2", 404}, {"/missing/compare/v1...v2", 404},
		} {
			resp, err := app.Test(httptest.NewRequest("GET", tc.path, nil), -1)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := goquery.NewDocumentFromReader(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tc.status {
				t.Fatalf("%s %s: %d", theme, tc.path, resp.StatusCode)
			}
			if !strings.Contains(resp.Header.Get("Cache-Control"), "no-store") {
				t.Fatal("comparison cached")
			}
			if tc.status == 303 {
				if resp.Header.Get("Location") != "/compare-story/compare/v1...v2" {
					t.Fatal("incorrect comparison redirect")
				}
				continue
			}
			if tc.status != 200 {
				continue
			}
			if canonical, _ := doc.Find(`link[rel="canonical"]`).Attr("href"); canonical != "https://example.test/compare-story" {
				t.Fatalf("canonical: %s", canonical)
			}
			if robots, _ := doc.Find(`meta[name="robots"]`).Attr("content"); robots != "noindex" {
				t.Fatal("comparison indexable")
			}
			body := doc.Find(`[data-testid="compare-changes"]`)
			if body.Find("script,style,iframe,form").Length() != 0 || strings.Contains(body.Text(), "diffXSS") {
				t.Fatal("executable content in diff")
			}
			if strings.HasSuffix(tc.path, "v1...v2") {
				if doc.Find(`[data-testid="compare-before-title"]`).Text() != "Old title" || doc.Find(`[data-testid="compare-after-title"]`).Text() != "Current title" {
					t.Fatal("wrong revision title")
				}
				if body.Find("strong").Text() != "New prose" || body.Find("em").Text() != "Old prose" || body.Find("table").Length() != 1 {
					t.Fatal("diff displayed Markdown instead of rendered content")
				}
			}
			if strings.HasSuffix(tc.path, "v1...v1") && doc.Find(`[data-testid="compare-no-changes"]`).Length() != 1 {
				t.Fatal("identical version shown as changed")
			}
		}
	}
	if err := db.Model(&a).Update("content", strings.Repeat("x", content.MaxDiffBytes+1)).Error; err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Get("/:slug/compare/:versions?", articleCompare)
	resp, err := app.Test(httptest.NewRequest("GET", "/compare-story/compare/v2...v2", nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 413 {
		t.Fatalf("large diff was not bounded: %d", resp.StatusCode)
	}
}
