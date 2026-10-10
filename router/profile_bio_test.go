package router

import (
	"bytes"
	"encoding/base64"
	"github.com/gofiber/fiber/v2"
	html "github.com/gofiber/template/html/v2"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestProfileBioLinks(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		urls       []string
	}{
		{"plain", "A writer & reader", nil},
		{"multiple", "Blog: https://example.com/posts?a=1&b=2 and http://example.org", []string{"https://example.com/posts?a=1&b=2", "http://example.org"}},
		{"punctuation", "主页：https://example.com。 (https://example.org/path).", []string{"https://example.com", "https://example.org/path"}},
		{"balanced parentheses", "https://example.com/wiki/Go_(language)", []string{"https://example.com/wiki/Go_(language)"}},
		{"HTML stays text", `<img src=x onerror=alert(1)> <script>alert(1)</script> https://example.com/`, []string{"https://example.com/"}},
		{"unsafe schemes", "javascript:alert(1) data:text/html,hello ftp://example.com", nil},
		{"invalid HTTP", "https:// http:///path https://user:secret@example.com", nil},
		{"uppercase", "HTTPS://EXAMPLE.COM/path#anchor", []string{"HTTPS://EXAMPLE.COM/path#anchor"}},
		{"unicode", "https://例子.测试/文章，欢迎", []string{"https://例子.测试/文章"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(profileBio(tc.text))))
			if err != nil {
				t.Fatal(err)
			}
			if doc.Find("body").Text() != tc.text {
				t.Fatalf("changed description: %q", doc.Find("body").Text())
			}
			if doc.Find("script,img,iframe,svg").Length() != 0 {
				t.Fatal("description became executable HTML")
			}
			links := doc.Find("a")
			if links.Length() != len(tc.urls) {
				t.Fatalf("links=%d want %d: %s", links.Length(), len(tc.urls), profileBio(tc.text))
			}
			links.Each(func(i int, a *goquery.Selection) {
				parsed, err := url.Parse(a.AttrOr("href", ""))
				if err != nil {
					t.Fatal(err)
				}
				if parsed.Path != "/r/go" || parsed.Host != "" {
					t.Fatalf("bypassed redirect: %s", parsed)
				}
				decoded, err := base64.URLEncoding.DecodeString(parsed.Query().Get("url"))
				if err != nil || string(decoded) != tc.urls[i] {
					t.Fatalf("redirect destination %q: %v", decoded, err)
				}
				if a.Text() != tc.urls[i] || a.AttrOr("rel", "") != "nofollow ugc noopener noreferrer" {
					t.Fatal("wrong label or missing untrusted-link protection")
				}
			})
		})
	}
}

func TestProfileBioThemeRendering(t *testing.T) {
	t.Chdir("..")
	previous := translator.Trans
	t.Cleanup(func() { translator.Trans = previous })
	for _, theme := range []string{"cactus", "folio"} {
		translator.Reload(theme, "default")
		tr, _ := translator.Trans.GetTranslator("en")
		engine := html.New("resource/themes/site/"+theme+"/templates", ".html")
		setFuncMap(engine)
		if err := engine.Load(); err != nil {
			t.Fatal(err)
		}
		bio := `<img src=x onerror=alert(1)> Homepage: https://example.com/?a=1&b=2`
		profile := model.Account{ID: "reader", Nickname: "Reader", Bio: bio, Role: model.RoleUser}
		reader := fiber.Map{"ID": profile.ID, "Nickname": profile.Nickname, "Role": profile.Role, "Bio": bio, "CreatedAt": profile.CreatedAt, "ActivityCount": 1}
		for _, view := range []string{"site/user_profile", "site/reader_circle"} {
			t.Run(theme+"/"+view, func(t *testing.T) {
				var output bytes.Buffer
				data := fiber.Map{"Tr": &translator.Translator{Translator: tr, Trans: tr}, "Conf": &model.Config{}, "Path": "/readers/", "Theme": fiber.Map{}, "Data": fiber.Map{"profile": profile, "active": []fiber.Map{reader}, "latest": []fiber.Map{reader}, "page": 1, "articles_page": 1, "comments_page": 1}}
				if err := engine.Render(&output, view, data); err != nil {
					t.Fatal(err)
				}
				doc, err := goquery.NewDocumentFromReader(&output)
				if err != nil {
					t.Fatal(err)
				}
				bios := doc.Find(`[data-testid="public-profile-bio"], [data-testid="reader-circle-bio"]`)
				want := 1
				if view == "site/reader_circle" {
					want = 2
				}
				if bios.Length() != want {
					t.Fatalf("bios=%d want %d", bios.Length(), want)
				}
				bios.Each(func(_ int, s *goquery.Selection) {
					if s.Text() != bio || s.Find("img,script").Length() != 0 || s.Find("a").Length() != 1 {
						t.Fatalf("unsafe or missing bio: %s", s.Text())
					}
					if s.Find("a").AttrOr("href", "") != externalLink("https://example.com/?a=1&b=2") {
						t.Fatal("bio bypassed redirect")
					}
				})
			})
		}
	}
}
