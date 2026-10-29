package router

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/PuerkitoBio/goquery"
	html "github.com/gofiber/template/html/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func TestThemeBreadcrumbStructuredData(t *testing.T) {
	t.Chdir("..")
	previousSystem, previousTranslations := solitudes.System, translator.Trans
	t.Cleanup(func() { solitudes.System, translator.Trans = previousSystem, previousTranslations })
	for _, theme := range []string{"cactus", "folio"} {
		t.Run(theme, func(t *testing.T) {
			translator.Reload(theme, "default")
			engine := html.New("resource/themes/site/"+theme+"/templates", ".html")
			setFuncMap(engine)
			if err := engine.Load(); err != nil {
				t.Fatal(err)
			}
			for _, language := range []struct {
				locale, home, books, readers, posts, tags, search string
			}{
				{"en", "Home", "Books", "Reader circle", "Posts", "Tags", "Search"},
				{"zh", "首页", "专栏", "读者圈", "文章", "标签", "搜索"},
			} {
				t.Run(language.locale, func(t *testing.T) {
					catalog, ok := translator.Trans.GetTranslator(language.locale)
					if !ok {
						t.Fatalf("missing %s translation catalog", language.locale)
					}
					tr := &translator.Translator{Translator: catalog, Trans: catalog}
					conf := &model.Config{}
					conf.Site.Domain = "example.test"
					conf.Site.SpaceName = "Community"
					solitudes.System = &solitudes.SysVariable{Config: conf}
					profile := model.Account{ID: "063f2596-68b3-4514-b4ec-ce4ab72ae8bb", Nickname: `Reader "昵称" & </script>`}
					story := &model.Article{ID: "b8fb9b84-c36e-4b3b-986d-85a601f697b5", Slug: "chapter#?%/中文", Title: `A "quoted" story & <book>`}
					storyPath := "/chapter%23%3F%25%2F%E4%B8%AD%E6%96%87"
					for _, tc := range []struct {
						name, path, ogType string
						data               viewData
						names, paths       []string
					}{
						{"books", "/books/", "website", viewData{"what": "books"}, []string{language.home, language.books}, []string{"/", "/books/"}},
						{"readers", "/readers/", "website", nil, []string{language.home, language.readers}, []string{"/", "/readers/"}},
						// publicUserProfile supplies the account in profile, not user or account.
						{"profile", "/users/" + profile.ID, "website", viewData{"profile": profile, "title": profile.Nickname}, []string{language.home, profile.Nickname}, []string{"/", "/users/" + profile.ID}},
						{"unknown", "/unrecognized/", "website", viewData{"title": "Not a known breadcrumb"}, nil, nil},
						{"profile_without_account", "/users/" + profile.ID, "website", nil, nil, nil},
						{"home", "/", "website", nil, nil, nil},
						{"tags", "/tags/", "website", nil, []string{language.home, language.tags}, []string{"/", "/tags/"}},
						{"tag", "/tags/C%23%2FGo%3F/", "website", viewData{"tag": "C#/Go?"}, []string{language.home, language.tags, "C#/Go?"}, []string{"/", "/tags/", "/tags/C%23%2FGo%3F/"}},
						{"posts", "/posts/", "website", nil, []string{language.home, language.posts}, []string{"/", "/posts/"}},
						{"search", "/search/", "website", nil, []string{language.home, language.search}, []string{"/", "/search/"}},
						{"article", storyPath, "article", viewData{"article": story}, []string{language.home, language.posts, story.Title}, []string{"/", "/posts/", storyPath}},
						{"article_page", storyPath, "website", viewData{"article": story}, []string{language.home, story.Title}, []string{"/", storyPath}},
					} {
						for _, noindex := range []bool{false, true} {
							name := tc.name
							if noindex {
								name += "/noindex"
							}
							t.Run(name, func(t *testing.T) {
								var output bytes.Buffer
								binding := viewData{
									"Tr": tr, "Conf": conf, "Theme": viewData{},
									"Path": tc.path, "Noindex": noindex, "OgType": tc.ogType,
									"Title": "Decorated page title - Community", "Data": tc.data,
								}
								if err := engine.Render(&output, "site/header", binding); err != nil {
									t.Fatal(err)
								}
								doc, err := goquery.NewDocumentFromReader(&output)
								if err != nil {
									t.Fatal(err)
								}
								scripts := doc.Find(`script[type="application/ld+json"]`)
								wantScripts := 1
								if noindex || len(tc.names) == 0 {
									wantScripts = 0
								}
								if scripts.Length() != wantScripts {
									t.Fatalf("JSON-LD scripts=%d, want %d: %s", scripts.Length(), wantScripts, scripts.Text())
								}
								if wantScripts == 0 {
									return
								}
								var breadcrumb struct {
									Context string `json:"@context"`
									Type    string `json:"@type"`
									Items   []struct {
										Type     string `json:"@type"`
										Position int    `json:"position"`
										Name     string `json:"name"`
										Item     string `json:"item"`
									} `json:"itemListElement"`
								}
								if err := json.Unmarshal([]byte(scripts.Text()), &breadcrumb); err != nil {
									t.Fatalf("invalid breadcrumb JSON: %v; %s", err, scripts.Text())
								}
								if breadcrumb.Context != "https://schema.org" || breadcrumb.Type != "BreadcrumbList" {
									t.Fatalf("unexpected schema: %+v", breadcrumb)
								}
								if len(breadcrumb.Items) != len(tc.names) {
									t.Fatalf("breadcrumb items=%d, want %d: %+v", len(breadcrumb.Items), len(tc.names), breadcrumb.Items)
								}
								for i, item := range breadcrumb.Items {
									wantURL := "https://example.test" + tc.paths[i]
									if item.Type != "ListItem" || item.Position != i+1 || item.Name != tc.names[i] || item.Item != wantURL {
										t.Errorf("item %d=%+v, want ListItem position=%d name=%q item=%q", i, item, i+1, tc.names[i], wantURL)
									}
								}
							})
						}
					}
				})
			}
		})
	}
}
