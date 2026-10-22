package router

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/gofiber/fiber/v2"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func articleURLFixture(t *testing.T) (*gorm.DB, *fiber.App) {
	t.Helper()
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.ArticleHistory{}, &model.Comment{}, &model.FeedVisit{}); err != nil {
		t.Fatal(err)
	}
	previous, engine, translations := solitudes.System, globalDynamicEngine.engine, translator.Trans
	t.Cleanup(func() {
		solitudes.System, globalDynamicEngine.engine, translator.Trans = previous, engine, translations
	})
	t.Chdir("..")
	conf := &model.Config{}
	conf.Site.Domain = "example.test"
	conf.Site.Theme = "cactus"
	conf.Admin.Theme = "default"
	solitudes.System = &solitudes.SysVariable{DB: db, Config: conf, SafeCache: new(singleflight.Group)}
	if err := LoadTemplates(); err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{Views: globalDynamicEngine})
	app.Use(canonicalURLRedirect, trans)
	app.Get("/sitemap.xml", sitemapHandler)
	app.Get("/feed/:format?", feedHandler)
	app.Get("/read/:id/article.md", articleMachineContent)
	app.Use(registeredRedirect)
	app.Get("/:slug/compare/:versions?", articleCompare)
	app.Get("/:slug/:version?", article)
	return db, app
}

func TestPostgresArticleEncodedURLsResolveAndMatchCanonical(t *testing.T) {
	db, app := articleURLFixture(t)
	for _, tc := range []struct{ slug, path string }{
		{"中文文章", "/%E4%B8%AD%E6%96%87%E6%96%87%E7%AB%A0"},
		{"C#? +% &", "/C%23%3F%20+%25%20&"},
		{"%2F-literal", "/%252F-literal"},
		{"Mixed-Case", "/Mixed-Case"},
	} {
		t.Run(tc.slug, func(t *testing.T) {
			story := model.Article{Title: "Encoded story " + tc.slug, Slug: tc.slug, Content: "Current public body", Version: 2, TemplateID: solitudes.ArticleTemplateID, DisableComment: true}
			if err := db.Create(&story).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.ArticleHistory{ArticleID: story.ID, Version: 1, Title: "Earlier title", Content: "Earlier public body"}).Error; err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{tc.path, tc.path + "/v1", tc.path + "/compare/v1...v2"} {
				resp, err := app.Test(httptest.NewRequest("GET", path, nil), -1)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != 200 {
					t.Errorf("GET %s: %d", path, resp.StatusCode)
					continue
				}
				doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
				if err != nil {
					t.Fatal(err)
				}
				canonical := doc.Find(`link[rel="canonical"]`).AttrOr("href", "")
				if canonical != "https://example.test"+tc.path {
					t.Errorf("canonical=%q", canonical)
				}
				if path == tc.path && !strings.Contains(doc.Text(), "Current public body") {
					t.Fatal("wrong article body")
				}
			}
			for _, route := range []struct {
				suffix   string
				status   int
				location string
			}{
				{"/v2", 301, tc.path}, {"/compare", 303, tc.path + "/compare/v1...v2"},
			} {
				resp, err := app.Test(httptest.NewRequest("GET", tc.path+route.suffix, nil), -1)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode != route.status || resp.Header.Get("Location") != route.location {
					t.Errorf("redirect %s: %d %q", route.suffix, resp.StatusCode, resp.Header.Get("Location"))
				}
			}
			// A machine representation must cite the same encoded human-facing URL.
			resp, err := app.Test(httptest.NewRequest("GET", "/read/"+story.ID+"/article.md", nil), -1)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Link"), "<https://example.test"+tc.path+">; rel=\"canonical\"") {
				t.Errorf("machine canonical diverged: %s", resp.Header.Get("Link"))
			}
		})
	}
	resp, err := app.Test(httptest.NewRequest("GET", "/sitemap.xml", nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var sitemap struct {
		URLs []struct {
			Location string `xml:"loc"`
		} `xml:"url"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&sitemap); err != nil {
		t.Fatal(err)
	}
	reached := 0
	for _, entry := range sitemap.URLs {
		target, err := url.Parse(entry.Location)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(target.Path, "/") {
			continue
		}
		articleResp, err := app.Test(httptest.NewRequest("GET", target.EscapedPath(), nil), -1)
		if err != nil {
			t.Fatal(err)
		}
		articleResp.Body.Close()
		if articleResp.StatusCode != 200 {
			t.Errorf("sitemap article unreachable: %s status=%d", entry.Location, articleResp.StatusCode)
		}
		reached++
	}
	if reached != 4 {
		t.Fatalf("sitemap article count=%d", reached)
	}
}

func TestPostgresPublishRejectsUnroutableSlugsBeforeWriting(t *testing.T) {
	db, app, original := articleRedirectFixture(t)
	for _, slug := range []string{"", " ", "posts", "POSTS", "search", "robots.txt", "Admin", ".well-known", "chapter/part", "chapter\\part", ".", "..", "line\nbreak", strings.Repeat("中", 230)} {
		draft := &model.Article{Title: "Rejected slug", Content: "Body", TemplateID: solitudes.ArticleTemplateID, Visibility: model.VisibilityPublic}
		status := publishRedirectArticle(t, app, draft, slug)
		if status != 400 {
			t.Errorf("slug %q accepted: %d", slug, status)
		}
	}
	var count int64
	if err := db.Model(&model.Article{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("invalid publication wrote rows: %d %v", count, err)
	}
	var stored model.Article
	if err := db.Take(&stored, "id = ?", original.ID).Error; err != nil || stored.Slug != "original-story" {
		t.Fatal("existing article changed")
	}
}

func TestPostgresEncodedArticleRenameRemainsReadableAndDetectsRevisionLoops(t *testing.T) {
	db, app, story := articleRedirectFixture(t)
	if status := publishRedirectArticle(t, app, story, "中文 C#?"); status != 200 {
		t.Fatalf("rename failed: %d", status)
	}
	resp, err := app.Test(httptest.NewRequest("GET", "/original-story", nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 301 || resp.Header.Get("Location") != "/%E4%B8%AD%E6%96%87%20C%23%3F" {
		t.Fatalf("encoded alias: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	// Current-version virtual edges must use the same single decoding as article().
	rule := model.RedirectRule{SourcePath: "/%E4%B8%AD%E6%96%87%20C%23%3F", TargetPath: "/%E4%B8%AD%E6%96%87%20C%23%3F/v1"}
	if err := db.Create(&rule).Error; err != nil {
		t.Fatal(err)
	}
	if err := validateRedirectGraph(db); err == nil {
		t.Fatal("Unicode revision cycle was not detected")
	}
}

func TestPostgresLegacyUnroutableSlugCanBeRepaired(t *testing.T) {
	for _, oldSlug := range []string{"POSTS", "chapter/part"} {
		t.Run(oldSlug, func(t *testing.T) {
			db, app, story := articleRedirectFixture(t)
			if err := db.Model(story).Update("slug", oldSlug).Error; err != nil {
				t.Fatal(err)
			}
			if status := publishRedirectArticle(t, app, story, "recovered-story"); status != 200 {
				t.Fatalf("repair of legacy slug %q failed: %d", oldSlug, status)
			}
			var stored model.Article
			if err := db.Take(&stored, "id = ?", story.ID).Error; err != nil || stored.Slug != "recovered-story" {
				t.Fatalf("repair not persisted: %q %v", stored.Slug, err)
			}
			var aliases int64
			if err := db.Model(&model.RedirectRule{}).Where("article_id = ?", story.ID).Count(&aliases).Error; err != nil || aliases != 0 {
				t.Fatalf("unroutable legacy source registered: %d %v", aliases, err)
			}
		})
	}
}

func TestPostgresFeedArticleLinksUseEncodedPaths(t *testing.T) {
	db, app := articleURLFixture(t)
	story := model.Article{Title: "Special link", Slug: "C#? & 中文", Content: "Body", TemplateID: solitudes.ArticleTemplateID, Version: 1}
	if err := db.Create(&story).Error; err != nil {
		t.Fatal(err)
	}
	resp, err := app.Test(httptest.NewRequest("GET", "/feed/json", nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var feed struct {
		Items []struct {
			URL string `json:"url"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&feed); err != nil {
		t.Fatal(err)
	}
	want := "https://example.test/C%23%3F%20&%20%E4%B8%AD%E6%96%87"
	if len(feed.Items) != 1 || feed.Items[0].URL != want {
		t.Fatal(fmt.Sprintf("feed URL: %+v", feed.Items))
	}
}
