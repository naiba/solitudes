package router

import (
	"encoding/json"
	"encoding/xml"
	"github.com/PuerkitoBio/goquery"
	"github.com/naiba/solitudes/pkg/translator"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func TestPostgresSitemapOnlyIncludesPublicURLsAndEscapesXML(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}); err != nil {
		t.Fatal(err)
	}
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	config := &model.Config{}
	config.Site.Domain = "community.example"
	solitudes.System = &solitudes.SysVariable{Config: config, DB: db}
	created := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	public := model.Article{Title: "Public", Slug: "public&story", RawTags: "Reading & writing,中文,shared", CreatedAt: created}
	private := model.Article{Title: "Secret", Slug: "secret-story", RawTags: "private-only,shared", Visibility: model.VisibilityPrivate}
	for _, a := range []*model.Article{&public, &private} {
		if err := db.Create(a).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Legacy rows without an update time use their creation date, never year 0001.
	if err := db.Model(&public).UpdateColumn("updated_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Get("/sitemap.xml", sitemapHandler)
	readLocations := func() map[string]string {
		t.Helper()
		resp, err := app.Test(httptest.NewRequest("GET", "/sitemap.xml", nil), -1)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "application/xml") ||
			strings.Contains(string(body), "private-only") || strings.Contains(string(body), "secret-story") {
			t.Fatalf("invalid or private sitemap: status=%d body=%s", resp.StatusCode, body)
		}
		var sitemap struct {
			XMLName xml.Name
			URLs    []struct {
				Location string `xml:"loc"`
				LastMod  string `xml:"lastmod"`
			} `xml:"url"`
		}
		if err := xml.Unmarshal(body, &sitemap); err != nil {
			t.Fatalf("sitemap is not valid XML: %v: %s", err, body)
		}
		if sitemap.XMLName.Space != "http://www.sitemaps.org/schemas/sitemap/0.9" {
			t.Fatalf("invalid sitemap namespace: %+v", sitemap.XMLName)
		}
		locations := make(map[string]string)
		for _, entry := range sitemap.URLs {
			if _, exists := locations[entry.Location]; exists {
				t.Fatalf("duplicate sitemap URL: %s", entry.Location)
			}
			locations[entry.Location] = entry.LastMod
		}
		return locations
	}
	locations := readLocations()
	for _, path := range []string{"/", "/posts/", "/books/", "/tags/", "/public&story", "/tags/Reading%20&%20writing/", "/tags/%E4%B8%AD%E6%96%87/", "/tags/shared/"} {
		if _, exists := locations["https://community.example"+path]; !exists {
			t.Errorf("missing public URL %s: %+v", path, locations)
		}
	}
	if locations["https://community.example/public&story"] != "2026-01-02" {
		t.Fatal("missing creation-date fallback")
	}
	if _, exists := locations["https://community.example/readers/"]; exists {
		t.Fatal("empty reader directory should not be indexed")
	}
	member := model.Account{Nickname: "Reader", Email: "reader@example.test", Role: model.RoleUser, EmailVerifiedAt: &created}
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	if _, exists := readLocations()["https://community.example/readers/"]; !exists {
		t.Fatal("public reader directory missing")
	}
}

func TestPostgresArchiveSEOAndArticleTagLinks(t *testing.T) {
	db := newPostgresIdentityTestDB(t)
	if err := db.AutoMigrate(&model.Article{}, &model.Comment{}); err != nil {
		t.Fatal(err)
	}
	tag := "C#/Go? & 中文"
	title := "A \"quoted\" title & <script>"
	story := model.Article{Title: title, Slug: "seo-story", Content: "Description with \"quotes\" & <symbols>.", RawTags: tag, TemplateID: solitudes.ArticleTemplateID, Version: 1}
	if err := db.Create(&story).Error; err != nil {
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
			conf.Site.Theme = theme
			conf.Admin.Theme = "default"
			conf.Site.Domain = "example.test"
			solitudes.System = &solitudes.SysVariable{Config: conf, DB: db}
			if err := LoadTemplates(); err != nil {
				t.Fatal(err)
			}
			app := fiber.New(fiber.Config{Views: globalDynamicEngine})
			app.Use(trans)
			app.Get("/posts/:page?/", posts)
			app.Get("/books/:page?/", book)
			app.Get("/tags/:tag/:page?/", tags)
			app.Get("/:slug", article)
			for _, tc := range []struct {
				path   string
				status int
			}{
				{"/posts/", 200}, {"/books/", 200}, {"/posts/2/", 404}, {"/books/2/", 404}, {"/tags/" + url.PathEscape(tag) + "/2/", 404},
			} {
				resp, err := app.Test(httptest.NewRequest("GET", tc.path, nil), -1)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode != tc.status {
					t.Errorf("%s status=%d want %d", tc.path, resp.StatusCode, tc.status)
				}
			}
			resp, err := app.Test(httptest.NewRequest("GET", "/seo-story", nil), -1)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := goquery.NewDocumentFromReader(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != 200 {
				t.Fatalf("article status %d", resp.StatusCode)
			}
			links := doc.Find("a").FilterFunction(func(_ int, s *goquery.Selection) bool { return s.Text() == tag })
			if links.Length() != 1 {
				t.Fatalf("article tag links %d", links.Length())
			}
			href, _ := links.Attr("href")
			want := "https://example.test/tags/" + url.PathEscape(tag) + "/"
			if href != want {
				t.Errorf("tag link=%q want %q", href, want)
			}
			if href == want {
				tagResp, err := app.Test(httptest.NewRequest("GET", strings.TrimPrefix(href, "https://example.test"), nil), -1)
				if err != nil {
					t.Fatal(err)
				}
				tagResp.Body.Close()
				if tagResp.StatusCode != 200 {
					t.Errorf("tag destination status %d", tagResp.StatusCode)
				}
			}
			scripts := doc.Find(`script[type="application/ld+json"]`)
			if scripts.Length() != 2 {
				t.Fatalf("article structured metadata count %d", scripts.Length())
			}
			foundArticle := false
			scripts.Each(func(_ int, s *goquery.Selection) {
				var data map[string]interface{}
				if err := json.Unmarshal([]byte(s.Text()), &data); err != nil {
					t.Error(err)
					return
				}
				if data["@type"] == "BlogPosting" || data["@type"] == "Article" {
					foundArticle = true
					if data["headline"] != title {
						t.Errorf("headline=%q want %q", data["headline"], title)
					}
					if data["description"] != mdExcerpt(story.Content, 150) {
						t.Errorf("description=%q", data["description"])
					}
					keywords, ok := data["keywords"].([]interface{})
					if !ok || len(keywords) != 1 || keywords[0] != tag {
						t.Errorf("keywords=%+v", data["keywords"])
					}
				}
			})
			if !foundArticle {
				t.Error("missing Article metadata")
			}
		})
	}
}
