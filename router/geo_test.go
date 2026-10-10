package router

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/88250/lute"
	"github.com/PuerkitoBio/goquery"
	"github.com/gofiber/fiber/v2"
	html "github.com/gofiber/template/html/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func TestMachineMarkdownExternalLinks(t *testing.T) {
	raw := "## Evidence\n\n[Source](https://outside.test/p?a=1&b=2) and https://outside.test/auto\n\n[Reference][source]\n\n[source]: https://outside.test/reference\n\n<a href=\"https://outside.test/html\">HTML source</a>\n\n![Chart](https://outside.test/chart.png \"Chart caption\")\n\n```go\nfmt.Println(\"https://example.test/code\")\n```\n\n```access:members\nMEMBER SECRET\n```\n"
	result, err := machineMarkdown(raw, "https://blog.test/story")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result, "MEMBER SECRET") || !strings.Contains(result, "Chart caption") || !strings.Contains(result, "fmt.Println") {
		t.Fatalf("lost public content or leaked secret: %s", result)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(lute.New().MarkdownStr("", result)))
	if err != nil {
		t.Fatal(err)
	}
	links := doc.Find("a")
	if links.Length() != 4 {
		t.Fatalf("links=%d: %s", links.Length(), result)
	}
	links.Each(func(_ int, s *goquery.Selection) {
		if !strings.HasPrefix(s.AttrOr("href", ""), "https://blog.test/r/go?url=") {
			t.Fatalf("external link bypassed redirect: %s", result)
		}
	})
	if doc.Find("script,iframe").Length() != 0 {
		t.Fatal("executable markup")
	}
	if _, err := machineMarkdown("```access:invalid\nsecret\n```", "https://blog.test/story"); err == nil {
		t.Fatal("malformed access block not rejected")
	}
}

func TestRedirectPageNeverReflectsRequestTarget(t *testing.T) {
	t.Chdir("..")
	previous, translations := solitudes.System, translator.Trans
	t.Cleanup(func() { solitudes.System = previous; translator.Trans = translations })
	config := &model.Config{}
	config.Site.Domain = "blog.test"
	solitudes.System = &solitudes.SysVariable{Config: config}
	for _, theme := range []string{"cactus", "folio"} {
		translator.Reload(theme, "default")
		engine := html.New("resource/themes/site/"+theme+"/templates", ".html")
		setFuncMap(engine)
		if err := engine.Load(); err != nil {
			t.Fatal(err)
		}
		app := fiber.New(fiber.Config{Views: engine})
		app.Use(trans)
		app.Get("/r/go", goRedirect)
		for _, target := range []string{"https://outside.test/report?x=1&y=2", "https://例子.测试/文章", "javascript:alert(1)"} {
			t.Run(theme+"/"+target, func(t *testing.T) {
				resp, err := app.Test(httptest.NewRequest("GET", externalLink(target), nil))
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				doc, err := goquery.NewDocumentFromReader(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != 200 {
					t.Fatalf("status %d", resp.StatusCode)
				}
				if doc.Find("#continue-link").AttrOr("href", "") != "#" || doc.Find("#target-url").Text() != "" {
					t.Fatal("server reflected a user-controlled target")
				}
				raw, err := doc.Html()
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(raw, target) || strings.Contains(raw, strings.TrimPrefix(externalLink(target), "/r/go?url=")) {
					t.Fatal("request target leaked into server-rendered HTML")
				}

				if doc.Find(`meta[name="robots"]`).AttrOr("content", "") != "noindex" {
					t.Fatal("interstitial indexable")
				}
			})
		}
	}
}

func TestMachineMarkdownPreservesPublicStructure(t *testing.T) {
	raw := "# Heading\n\nParagraph with **emphasis**, `inline code`, and a [local page](/local#section).\n\n| A | B |\n| --- | --- |\n| One | Two |\n\n- First\n- Second\n\n```go\nfmt.Println(\"https://outside.test/not-a-link\")\n```\n\n<script>alert('do-not-export')</script>\n\n<a href=\"//outside.test/protocol-relative\">Reference</a>"
	output, err := machineMarkdown(raw, "https://blog.test/story")
	if err != nil {
		t.Fatal(err)
	}
	engine := lute.New()
	engine.SetGFMAutoLink(true)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(engine.MarkdownStr("", output)))
	if err != nil {
		t.Fatal(err)
	}
	for _, selector := range []string{"h1", "strong", "table", "ul", "pre code.language-go"} {
		if doc.Find(selector).Length() != 1 {
			t.Fatalf("missing %s: %s", selector, output)
		}
	}
	if strings.Contains(output, "do-not-export") || !strings.Contains(output, "fmt.Println") {
		t.Fatal("script exported or code removed")
	}
	if doc.Find(`a[href="https://blog.test/local#section"]`).Length() != 1 {
		t.Fatal("internal link changed")
	}
	doc.Find("a").Each(func(_ int, s *goquery.Selection) {
		if !strings.HasPrefix(s.AttrOr("href", ""), "https://blog.test/") {
			t.Fatalf("direct outbound link %s", s.AttrOr("href", ""))
		}
	})
}

func TestMachineMetadataCannotCreateDirectOutboundLinks(t *testing.T) {
	for _, text := range []string{"https://outside.test/source", "[Click](https://outside.test/source)", "Source <https://outside.test/source>", "https://outside.test/with`tick"} {
		engine := lute.New()
		engine.SetGFMAutoLink(true)
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(engine.MarkdownStr("", markdownText(text))))
		if err != nil {
			t.Fatal(err)
		}
		if doc.Find("a").Length() != 0 {
			t.Fatalf("metadata produced a direct link: %s", markdownText(text))
		}
	}
}
