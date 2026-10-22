package router

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gofiber/fiber/v2"
	html "github.com/gofiber/template/html/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

// Fixtures provide actual articles/comments to the real templates, including
// branches that otherwise disappear when template queries return no rows.
type articleLinkQueries struct {
	article, topic *model.Article
	comment        *model.Comment
}

func (q articleLinkQueries) Articles(_ string, _ int, filters ...string) []*model.Article {
	if len(filters) > 0 && filters[0] == "tag" {
		return []*model.Article{q.topic}
	}
	return []*model.Article{q.article}
}
func (q articleLinkQueries) Comments(_ string, _ int) []*model.Comment {
	return []*model.Comment{q.comment}
}
func (q articleLinkQueries) Users(_ string, _ int) []readerCircleMember { return nil }
func (q articleLinkQueries) RecentComments(_ int) []publicComment {
	return []publicComment{{ID: q.comment.ID, ArticleSlug: q.article.Slug, ArticleTitle: q.article.Title, Content: q.comment.Content}}
}

func TestThemeArticleSlugLinks(t *testing.T) {
	t.Chdir("..")
	previous := translator.Trans
	t.Cleanup(func() { translator.Trans = previous })
	previousSystem := solitudes.System
	t.Cleanup(func() { solitudes.System = previousSystem })
	const slug = "中文 C#? % & +"
	const title = "中文 title #? % & +"
	articlePath := "/" + url.PathEscape(slug)
	articleURL := "https://example.test" + articlePath
	for _, theme := range []string{"cactus", "folio"} {
		t.Run(theme, func(t *testing.T) {
			translator.Reload(theme, "default")
			catalog, _ := translator.Trans.GetTranslator("en")
			tr := &translator.Translator{Translator: catalog, Trans: catalog}
			engine := html.New("resource/themes/site/"+theme+"/templates", ".html")
			setFuncMap(engine)
			if err := engine.Load(); err != nil {
				t.Fatal(err)
			}
			conf := &model.Config{}
			conf.Site.Domain = "example.test"
			solitudes.System = &solitudes.SysVariable{Config: conf}
			story := &model.Article{ID: "story", Slug: slug, Title: title, Content: "Article text", Version: 3}
			neighbor := model.Article{ID: "neighbor", Slug: slug, Title: title}
			story.SibilingArticle = &model.SibilingArticle{Prev: neighbor, Next: neighbor}
			story.Book = &neighbor
			story.IsBook = true
			story.Chapters = []*model.Article{&neighbor}
			comment := &model.Comment{ID: "comment", Article: story, Version: 2, ReplyCount: 1, Content: "Comment text"}
			story.Comments = []*model.Comment{comment}
			topic := *story
			topic.RawTags = "Topic"
			queries := articleLinkQueries{article: story, topic: &topic, comment: comment}
			render := func(t *testing.T, name string, data fiber.Map) *goquery.Document {
				t.Helper()
				data["navigation"] = pageNavigation{}
				data["comment_navigation"] = pageNavigation{}
				binding := fiber.Map{"Tr": tr, "Conf": conf, "Theme": fiber.Map{}, "Path": articlePath, "Data": data, "Queries": queries, "article": story, "tr": tr}
				if name == "article" {
					binding["OgType"] = "article"
				}
				var output bytes.Buffer
				if err := engine.Render(&output, "site/"+name, binding); err != nil {
					t.Fatal(err)
				}
				doc, err := goquery.NewDocumentFromReader(&output)
				if err != nil {
					t.Fatal(err)
				}
				return doc
			}
			checkLinks := func(t *testing.T, doc *goquery.Document, selector, suffix, query, fragment string) {
				t.Helper()
				links := doc.Find(selector)
				if links.Length() == 0 {
					t.Fatalf("missing links: %s", selector)
				}
				links.Each(func(_ int, node *goquery.Selection) {
					raw := node.AttrOr("href", node.AttrOr("action", ""))
					parsed, err := url.Parse(raw)
					if err != nil {
						t.Errorf("invalid URL %q: %v", raw, err)
						return
					}
					if parsed.Path != "/"+slug+suffix || parsed.RawQuery != query || parsed.Fragment != fragment {
						t.Errorf("%s URL %q: path=%q query=%q fragment=%q", selector, raw, parsed.Path, parsed.RawQuery, parsed.Fragment)
					}
				})
			}
			for _, name := range []string{"article", "page"} {
				t.Run(name, func(t *testing.T) {
					doc := render(t, name, fiber.Map{"article": story, "can_read_history": true})
					for _, auth := range []string{"sign-in", "register"} {
						link := doc.Find(`[data-testid="comment-` + auth + `"]`)
						if link.Length() != 1 {
							t.Fatalf("missing %s link", auth)
						}
						parsed, err := url.Parse(link.AttrOr("href", ""))
						if err != nil {
							t.Fatal(err)
						}
						if got := parsed.Query().Get("return_to"); got != articlePath+"#reply" {
							t.Errorf("%s return_to=%q, want %q", auth, got, articlePath+"#reply")
						}
					}
					if got := doc.Find("#id_slug").AttrOr("value", ""); got != slug {
						t.Errorf("raw comment slug=%q", got)
					}
					countMatches := regexp.MustCompile(`fetch\(("/api/count[^"\n]+")`).FindAllStringSubmatch(doc.Find("script:not([src])").Text(), -1)
					if len(countMatches) != 1 {
						t.Fatalf("count requests=%d", len(countMatches))
					}
					var countURL string
					if err := json.Unmarshal([]byte(countMatches[0][1]), &countURL); err != nil {
						t.Fatal(err)
					}
					count, err := url.Parse(countURL)
					if err != nil {
						t.Fatal(err)
					}
					if count.Query().Get("slug") != slug || len(count.Query()) != 2 || count.Fragment != "" {
						t.Errorf("count URL corrupted slug: %q", countURL)
					}
					checkLinks(t, doc, `[data-testid="comment-thread-link"]`, "", "thread=comment", "comments")
					checkLinks(t, doc, `[data-testid="comment-entry"] a[href*="/v2"]`, "/v2", "", "")
					foundSchema := false
					doc.Find(`script[type="application/ld+json"]`).Each(func(_ int, script *goquery.Selection) {
						var schema map[string]any
						if err := json.Unmarshal([]byte(script.Text()), &schema); err != nil {
							t.Fatal(err)
						}
						if schema["@type"] != "BlogPosting" && schema["@type"] != "WebPage" {
							return
						}
						foundSchema = true
						if schema["url"] != articleURL {
							t.Errorf("schema url=%q, want %q", schema["url"], articleURL)
						}
						if name == "article" {
							if schema["@id"] != articleURL+"#article" || schema["hasPart"].(map[string]any)["@id"] != articleURL+"#article-body" || schema["mainEntityOfPage"].(map[string]any)["@id"] != articleURL {
								t.Errorf("article schema IDs corrupted: %+v", schema)
							}
						}
					})
					if !foundSchema {
						t.Fatal("missing article/page structured data")
					}
					if name == "article" {
						for _, selector := range []string{`[data-testid="book-parent"]`, `[data-testid="book-next"]`, `[data-testid="book-previous"]`, `[data-testid="book-chapter-link"]`} {
							checkLinks(t, doc, selector, "", "", "")
						}
						checkLinks(t, doc, `[data-testid="article-history"] a[href$="/v2"]`, "/v2", "", "")
						checkLinks(t, doc, `[data-testid="article-compare-latest"]`, "/compare", "", "")
						for _, selector := range []string{`[data-testid="article-share"]`, `[data-testid="article-share-dialog"]`} {
							if got := doc.Find(selector).AttrOr("data-url", ""); got != articleURL {
								t.Errorf("%s data-url=%q", selector, got)
							}
						}
						if got := doc.Find("#article-share-url").AttrOr("value", ""); got != articleURL {
							t.Errorf("share input=%q", got)
						}
						for _, share := range []struct{ selector, urlKey, titleKey string }{{`[data-share-email]`, "body", "subject"}, {`a[href^="https://x.com/intent/post"]`, "url", "text"}} {
							link := doc.Find(share.selector)
							if link.Length() != 1 {
								t.Fatalf("missing share link %s", share.selector)
							}
							parsed, err := url.Parse(link.AttrOr("href", ""))
							if err != nil {
								t.Fatal(err)
							}
							if parsed.Query().Get(share.urlKey) != articleURL || parsed.Query().Get(share.titleKey) != title {
								t.Errorf("share query double-encoded or corrupted: %s", parsed)
							}
						}
					}
				})
			}
			t.Run("old_revision", func(t *testing.T) {
				story.NewVersion = 4
				defer func() { story.NewVersion = 0 }()
				doc := render(t, "article", fiber.Map{"article": story})
				checkLinks(t, doc, `[data-testid="article-latest-version"]`, "", "", "")
				checkLinks(t, doc, `[data-testid="article-compare-link"]`, "/compare/v3...v4", "", "")
			})
			t.Run("siblings", func(t *testing.T) {
				story.Book = nil
				defer func() { story.Book = &neighbor }()
				doc := render(t, "article", fiber.Map{"article": story})
				selector := ".sibling-prev, .sibling-next"
				if theme == "cactus" {
					selector = `[data-testid="article-previous"], [data-testid="article-next"]`
				}
				checkLinks(t, doc, selector, "", "", "")
			})
			for _, tc := range []struct {
				name, selector, suffix, query, fragment string
				data                                    fiber.Map
			}{
				{"article_compare", `[data-testid="compare-article-link"]`, "", "", "", fiber.Map{"article": story, "before": story, "after": story}},
				{"article_compare", `[data-testid="compare-form"]`, "/compare", "", "", fiber.Map{"article": story, "before": story, "after": story}},
				{"article_compare", `.compare-titles a`, "/v3", "", "", fiber.Map{"article": story, "before": story, "after": story}},
				{"article_list_entry", `[data-testid="article-list-link"]`, "", "", "", fiber.Map{}},
				{"search", `.search-result-title`, "", "", "", fiber.Map{"results": []*model.Article{story}}},
				{"user_profile", `[data-testid="public-profile-article"]`, "", "", "", fiber.Map{"profile": model.Account{Nickname: "Reader"}, "articles": []*model.Article{story}, "comments": []*model.Comment{comment}}},
				{"user_profile", `[data-testid="public-profile-comment-article"]`, "", "thread=comment", "comment-comment", fiber.Map{"profile": model.Account{Nickname: "Reader"}, "comments": []*model.Comment{comment}}},
				{"index", `[data-testid="home-comment-link"], [data-testid="topic-comment-link"]`, "", "thread=comment", "comment-comment", fiber.Map{}},
				{"index", `[data-testid="topic-reply-link"]`, "", "", "", fiber.Map{}},
				{"reader_circle", `[data-testid="reader-circle-activity-link"]`, "", "thread=comment", "comment-comment", fiber.Map{"page": 1, "activity": []fiber.Map{{"Kind": "comment", "ArticleSlug": slug, "CommentID": "comment", "HappenedAt": time.Time{}}}}},
			} {
				t.Run(tc.name+"/"+tc.selector, func(t *testing.T) {
					checkLinks(t, render(t, tc.name, tc.data), tc.selector, tc.suffix, tc.query, tc.fragment)
				})
			}
			t.Run("index_article_attributes", func(t *testing.T) {
				doc := render(t, "index", fiber.Map{})
				selector := `[data-testid="article-list-link"]`
				if theme == "folio" {
					selector = `.hero-title a, .journal-read-link, .journal-secondary-story h2 a`
				}
				checkLinks(t, doc, selector, "", "", "")
				attrs := doc.Find("[data-topic-slug], [data-story-slug]")
				if attrs.Length() == 0 {
					t.Fatal("missing raw slug attributes")
				}
				attrs.Each(func(_ int, node *goquery.Selection) {
					if got := node.AttrOr("data-topic-slug", node.AttrOr("data-story-slug", "")); got != slug {
						t.Errorf("raw slug encoded: %q", got)
					}
				})
			})
		})
	}
}

func TestAdminArticleSlugLinks(t *testing.T) {
	t.Chdir("..")
	previous := translator.Trans
	t.Cleanup(func() { translator.Trans = previous })
	translator.Reload("cactus", "default")
	catalog, _ := translator.Trans.GetTranslator("en")
	tr := &translator.Translator{Translator: catalog, Trans: catalog}
	engine := html.New("resource/themes/admin/default/templates", ".html")
	setFuncMap(engine)
	if err := engine.Load(); err != nil {
		t.Fatal(err)
	}
	const slug = "中文 C#? % & +"
	story := &model.Article{ID: "story", Slug: slug, Title: "Article", Version: 2}
	conf := &model.Config{}
	conf.Site.Domain = "example.test"
	for _, name := range []string{"articles", "comments", "media", "index"} {
		t.Run(name, func(t *testing.T) {
			data := fiber.Map{
				"articles":   []*model.Article{story},
				"comments":   []*model.Comment{{ID: "comment", Article: story, Version: 2}},
				"medias":     []fiber.Map{{"Article": story, "Filename": "image.png", "UploadedAt": time.Time{}}},
				"page":       fiber.Map{"Page": 1, "PrevPage": 0, "NextPage": 0},
				"navigation": pageNavigation{},
			}
			var output bytes.Buffer
			if err := engine.Render(&output, "admin/"+name, fiber.Map{"Path": "/admin/" + name, "Tr": tr, "Conf": conf, "Account": &model.Account{Role: model.RoleAdmin}, "Data": data}); err != nil {
				t.Fatal(err)
			}
			doc, err := goquery.NewDocumentFromReader(&output)
			if err != nil {
				t.Fatal(err)
			}
			if name == "index" {
				if !strings.Contains(doc.Find("script:not([src])").Text(), `window.location.href = "/" + encodeURIComponent(data.Slug)`) {
					t.Error("quick publish redirect must encode its slug")
				}
				return
			}
			links := doc.Find(`a[href^="//example.test/"]`)
			if links.Length() != 1 {
				t.Fatalf("article links=%d, want 1", links.Length())
			}
			parsed, err := url.Parse(links.AttrOr("href", ""))
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Path != "/"+slug+"/v2" || parsed.RawQuery != "" || parsed.Fragment != "" {
				t.Errorf("admin article URL corrupted: %+v", parsed)
			}
			if name == "articles" && doc.Find(".article-primary small").Text() != "/"+slug {
				t.Error("displayed slug must remain unencoded")
			}
		})
	}
}

func TestPublishArticleSlugHint(t *testing.T) {
	t.Chdir("..")
	previous := translator.Trans
	t.Cleanup(func() { translator.Trans = previous })
	translator.Reload("cactus", "default")
	engine := html.New("resource/themes/admin/default/templates", ".html")
	setFuncMap(engine)
	if err := engine.Load(); err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "zh"} {
		t.Run(locale, func(t *testing.T) {
			catalog, _ := translator.Trans.GetTranslator(locale)
			tr := &translator.Translator{Translator: catalog, Trans: catalog}
			var output bytes.Buffer
			const slug = "中文 C#? % & +"
			if err := engine.Render(&output, "admin/publish", fiber.Map{"Path": "/admin/publish", "Tr": tr, "Conf": &model.Config{}, "Account": &model.Account{Role: model.RoleAdmin}, "Data": fiber.Map{"article": &model.Article{Slug: slug}}}); err != nil {
				t.Fatal(err)
			}
			doc, err := goquery.NewDocumentFromReader(&output)
			if err != nil {
				t.Fatal(err)
			}
			if doc.Find("#inputSlug").AttrOr("value", "") != slug {
				t.Error("editor changed raw slug")
			}
			hint := tr.T("article_slug_hint")
			if hint == "" || hint == "article_slug_hint" || !strings.Contains(doc.Find("#inputSlug").Parent().Text(), hint) {
				t.Errorf("missing translated Unicode slug hint: %q", hint)
			}
			if !strings.Contains(doc.Find("script:not([src])").Text(), `window.location.href = "/" + encodeURIComponent(data.Slug)`) {
				t.Error("publish redirect must encode the slug as one path segment")
			}
		})
	}
}
