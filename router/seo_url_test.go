package router

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/gofiber/fiber/v2"
	html "github.com/gofiber/template/html/v2"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func TestCanonicalURLRedirect(t *testing.T) {
	app := fiber.New()
	app.Use(canonicalURLRedirect)
	app.Use(func(c *fiber.Ctx) error { return c.SendString(c.Method() + ":" + string(c.Body())) })
	for _, tc := range []struct {
		method, path, target string
		status               int
	}{
		{"GET", "/posts", "/posts/", 301},
		{"HEAD", "/story/", "/story", 301},
		{"GET", "/posts///?utm_source=mail", "/posts/?utm_source=mail", 301},
		{"GET", "/posts/1/", "/posts/", 301},
		{"GET", "/books/001", "/books/", 301},
		{"GET", "/posts/002/", "/posts/2/", 301},
		{"GET", "/tags/C%23%2FGo%3F/01/", "/tags/C%23%2FGo%3F/", 301},
		{"GET", "/tags/1/", "", 200},
		{"GET", "/tags/C%23%2FGo%3F/", "", 200},
		{"GET", "/posts/2/", "", 200},
		{"GET", "/", "", 200},
		{"GET", "/story", "", 200},
		{"POST", "/api/comment/", "", 200},
		{"DELETE", "/account/passkeys/key/", "", 200},
		{"OPTIONS", "/api/comment/", "", 200},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			resp, err := app.Test(httptest.NewRequest(tc.method, tc.path, strings.NewReader("preserved-body")))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.status || resp.Header.Get("Location") != tc.target {
				t.Fatalf("status=%d location=%q; want %d %q", resp.StatusCode, resp.Header.Get("Location"), tc.status, tc.target)
			}
			if tc.status == 200 && tc.method != "HEAD" {
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
				if string(body) != tc.method+":preserved-body" {
					t.Fatalf("method/body changed: %q", body)
				}
			}
			if tc.target != "" {
				next, err := app.Test(httptest.NewRequest(tc.method, tc.target, nil))
				if err != nil {
					t.Fatal(err)
				}
				defer next.Body.Close()
				if next.StatusCode != 200 {
					t.Fatalf("redirect chain: %s -> %s", tc.target, next.Header.Get("Location"))
				}
			}
		})
	}
}

func TestThemeCanonicalAndStructuredMetadata(t *testing.T) {
	previous, translations := solitudes.System, translator.Trans
	t.Cleanup(func() { solitudes.System, translator.Trans = previous, translations })
	translator.Init()
	for _, theme := range []string{"cactus", "folio"} {
		t.Run(theme, func(t *testing.T) {
			conf := &model.Config{}
			conf.Site.Domain = "example.test"
			conf.Site.SpaceName = "Community"
			solitudes.System = &solitudes.SysVariable{Config: conf}
			engine := html.New("../resource/themes/site/"+theme+"/templates", ".html")
			setFuncMap(engine)
			if err := engine.Load(); err != nil {
				t.Fatal(err)
			}
			app := fiber.New(fiber.Config{Views: engine})
			app.Use(trans)
			tag := "C#/Go? & 中文"
			app.Get("/tags/:tag/", func(c *fiber.Ctx) error { return c.Render("site/header", injectSiteData(c, fiber.Map{"tag": tag})) })
			app.Get("/story/v1", func(c *fiber.Ctx) error {
				return c.Render("site/header", injectSiteData(c, fiber.Map{"noindex": true, "canonical_path": "/story"}))
			})
			for _, path := range []string{"/tags/" + url.PathEscape(tag) + "/", "/story/v1"} {
				resp, err := app.Test(httptest.NewRequest("GET", path, nil))
				if err != nil {
					t.Fatal(err)
				}
				doc, err := goquery.NewDocumentFromReader(resp.Body)
				resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != 200 {
					t.Fatalf("status %d", resp.StatusCode)
				}
				want := "https://example.test" + path
				if path == "/story/v1" {
					want = "https://example.test/story"
				}
				for _, selector := range []string{`link[rel="canonical"]`, `meta[property="og:url"]`} {
					attr := "content"
					if strings.HasPrefix(selector, "link") {
						attr = "href"
					}
					value, _ := doc.Find(selector).Attr(attr)
					if value != want || doc.Find(selector).Length() != 1 {
						t.Errorf("%s=%q want %q", selector, value, want)
					}
				}
				if path == "/story/v1" {
					if value, _ := doc.Find(`meta[name="robots"]`).Attr("content"); value != "noindex" {
						t.Error("history must remain noindex")
					}
					continue
				}
				scripts := doc.Find(`script[type="application/ld+json"]`)
				if scripts.Length() != 1 {
					t.Fatalf("breadcrumb count %d", scripts.Length())
				}
				var breadcrumb struct {
					Items []struct {
						Name string `json:"name"`
						URL  string `json:"item"`
					} `json:"itemListElement"`
				}
				if err := json.Unmarshal([]byte(scripts.Text()), &breadcrumb); err != nil {
					t.Fatal(err)
				}
				if len(breadcrumb.Items) != 3 {
					t.Fatalf("breadcrumb items: %+v", breadcrumb)
				}
				last := breadcrumb.Items[2]
				if last.URL != want || last.Name != tag {
					t.Errorf("breadcrumb=%+v want %q %q", last, tag, want)
				}
			}
		})
	}
}

func TestArticleCitationMetadataAndServerRenderedCaptions(t *testing.T) {
	previous, translations := solitudes.System, translator.Trans
	t.Cleanup(func() { solitudes.System, translator.Trans = previous, translations })
	translator.Init()
	for _, theme := range []string{"cactus", "folio"} {
		t.Run(theme, func(t *testing.T) {
			conf := &model.Config{}
			conf.Site.Domain = "example.test"
			conf.Site.SpaceName = "Community & stories"
			solitudes.System = &solitudes.SysVariable{Config: conf}
			engine := html.New("../resource/themes/site/"+theme+"/templates", ".html")
			setFuncMap(engine)
			if err := engine.Load(); err != nil {
				t.Fatal(err)
			}
			story := &model.Article{Title: `A "quoted" story`, Slug: "image-story", Content: `![Description](/photo.png "Caption & source")`, Version: 3, DisableComment: true, SibilingArticle: &model.SibilingArticle{}}
			app := fiber.New(fiber.Config{Views: engine})
			app.Use(trans)
			app.Get("/:slug", func(c *fiber.Ctx) error {
				return c.Render("site/article", injectSiteData(c, fiber.Map{"article": story, "og_type": "article", "noindex": c.Query("private") == "true"}))
			})
			for _, private := range []bool{false, true} {
				target := "/image-story"
				if private {
					target += "?private=true"
				}
				resp, err := app.Test(httptest.NewRequest("GET", target, nil))
				if err != nil {
					t.Fatal(err)
				}
				doc, err := goquery.NewDocumentFromReader(resp.Body)
				resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != 200 {
					t.Fatalf("status %d: %s", resp.StatusCode, doc.Text())
				}
				if doc.Find("#article-body figure figcaption").Text() != "Caption & source" {
					t.Fatal("caption not server rendered")
				}
				found := 0
				doc.Find(`script[type="application/ld+json"]`).Each(func(_ int, s *goquery.Selection) {
					var data map[string]interface{}
					if err := json.Unmarshal([]byte(s.Text()), &data); err != nil {
						t.Fatal(err)
					}
					if data["@type"] != "BlogPosting" {
						return
					}
					found++
					if data["@id"] != "https://example.test/image-story#article" || data["version"] != "3" || data["headline"] != story.Title {
						t.Fatalf("citation identity: %+v", data)
					}
					part, ok := data["hasPart"].(map[string]interface{})
					if !ok || part["@id"] != "https://example.test/image-story#article-body" {
						t.Fatalf("body reference: %+v", part)
					}
					blog, ok := data["isPartOf"].(map[string]interface{})
					if !ok || blog["name"] != conf.Site.SpaceName || blog["url"] != "https://example.test/" {
						t.Fatalf("blog identity: %+v", blog)
					}
				})
				robots := doc.Find(`meta[name="robots"]`).AttrOr("content", "")
				if private {
					if found != 0 || robots != "noindex" {
						t.Fatal("restricted metadata indexed")
					}
				} else if found != 1 || robots != "max-image-preview:large" {
					t.Fatalf("public metadata count=%d robots=%s", found, robots)
				}
			}
		})
	}
}
