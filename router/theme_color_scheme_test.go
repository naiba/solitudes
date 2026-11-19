package router

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	html "github.com/gofiber/template/html/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

// Both site themes honour the cross-theme `solitudes_theme` preference. Cactus
// has one light and one dark palette and offers the reader three choices:
// follow the device, light, or dark. It once had a `cactus.colorscheme`
// setting with six palettes, most of which could not follow the device at all.
func TestSiteThemesExposeVisitorColorScheme(t *testing.T) {
	t.Chdir("..")
	render := func(t *testing.T, theme string, themeConfig viewData) (*goquery.Document, string) {
		t.Helper()
		return renderSiteErrorPage(t, theme, themeConfig)
	}

	t.Run("cactus", func(t *testing.T) {
		// A leftover setting from an older installation changes nothing.
		for _, themeConfig := range []viewData{{}, {"cactus.colorscheme": "dracula"}} {
			doc, source := render(t, "cactus", themeConfig)
			group := doc.Find(`[data-testid="site-theme-toggle"]`)
			if group.Length() != 1 || strings.TrimSpace(group.AttrOr("aria-label", "")) == "" {
				t.Fatalf("want one labelled colour scheme switch, got %d", group.Length())
			}
			choices := group.Find("button[data-theme-choice]").Map(func(_ int, choice *goquery.Selection) string {
				if strings.TrimSpace(choice.Text()) == "" {
					t.Errorf("choice %q has no translated label", choice.AttrOr("data-theme-choice", ""))
				}
				return choice.AttrOr("data-theme-choice", "")
			})
			if strings.Join(choices, ",") != "auto,light,dark" {
				t.Errorf("choices = %v, want auto, light, dark", choices)
			}
			if pressed := group.Find(`button[aria-pressed="true"]`); pressed.Length() != 1 || pressed.AttrOr("data-theme-choice", "") != "auto" {
				t.Error("following the device is the choice shown as current until the script reads a stored one")
			}
			// The stored choice must be applied in <head>, before the stylesheet paints.
			head, _ := doc.Find("head").Html()
			script := strings.Index(head, "localStorage.getItem('solitudes_theme')")
			stylesheet := strings.Index(head, "/static/site/cactus/css/color-scheme.css")
			if script < 0 || stylesheet < 0 || script > stylesheet {
				t.Fatalf("stored preference must be read before color-scheme.css loads (script=%d stylesheet=%d)", script, stylesheet)
			}
			if doc.Find(`link[href*="/static/site/cactus/css/theme-"]`).Length() != 0 {
				t.Error("no per-palette stylesheet is linked any more")
			}
			if !strings.Contains(source, "isDarkPalette()") {
				t.Error("diagram rendering must follow the painted palette, not the theme id")
			}
		}
		metadata, err := os.ReadFile("resource/themes/site/cactus/metadata.json")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(metadata), "colorscheme") {
			t.Error("the colour scheme is the reader's choice, not a theme setting")
		}
		stylesheets, _ := os.ReadDir("resource/themes/site/cactus/static/css")
		for _, stylesheet := range stylesheets {
			if strings.HasPrefix(stylesheet.Name(), "theme-") {
				t.Errorf("unused palette stylesheet %s", stylesheet.Name())
			}
		}
	})

	t.Run("folio", func(t *testing.T) {
		doc, source := render(t, "folio", viewData{})
		if count := doc.Find(`[data-testid="site-theme-toggle"]`).Length(); count != 1 {
			t.Fatalf("folio renders %d desktop theme toggles; want 1", count)
		}
		if count := doc.Find(`[data-testid="site-theme-toggle-mobile"]`).Length(); count != 1 {
			t.Fatalf("folio renders %d mobile theme toggles; want 1", count)
		}
		if !strings.Contains(source, "localStorage.getItem('solitudes_theme')") {
			t.Error("folio must read the shared preference key")
		}
	})
}

// Folio's reset and web fonts must not depend on third-party requests that
// block or reflow the page: Tailwind stays optional and fonts load without
// @import chains. See the base reset comment in style.css.
func TestFolioRendersWithoutBlockingThirdPartyStyles(t *testing.T) {
	t.Chdir("..")
	asset, err := os.ReadFile("resource/themes/site/folio/static/css/style.css")
	if err != nil {
		t.Fatal(err)
	}
	style := string(asset)
	if strings.Contains(style, "@import") {
		t.Error("style.css must not @import remote stylesheets; they block first paint")
	}
	for _, rule := range []string{
		"*, ::before, ::after { box-sizing: border-box; border-width: 0; border-style: solid;",
		"blockquote, dl, dd, h1, h2, h3, h4, h5, h6, hr, figure, p, pre { margin: 0; }",
		"ol, ul { margin: 0; padding: 0; list-style: none; }",
		"h1, h2, h3, h4, h5, h6 { font-size: inherit; font-weight: inherit; }",
	} {
		if !strings.Contains(style, rule) {
			t.Errorf("base reset misses %q", rule)
		}
	}
	// The grain belongs to the page background; a fixed layer above the content
	// tinted text, images and code.
	if strings.Contains(style, "body::before") {
		t.Error("paper grain must not be a pseudo-element layered above the content")
	}
	doc, _ := renderSiteErrorPage(t, "folio", viewData{})
	for _, host := range []string{"cdn.jsdelivr.net/npm/tailwindcss", "fonts.googleapis.cn/css2"} {
		link := doc.Find(`head > link[rel="stylesheet"][href*="` + host + `"]`)
		if link.Length() != 1 {
			t.Fatalf("%s stylesheet links = %d; want 1 outside <noscript>", host, link.Length())
		}
		if link.AttrOr("media", "") != "print" || !strings.Contains(link.AttrOr("onload", ""), "this.media='all'") {
			t.Errorf("%s must load without blocking first paint", host)
		}
	}
}

// renderSiteErrorPage renders the smallest complete page (header, navigation
// and footer) of a site theme with the given theme configuration.
func renderSiteErrorPage(t *testing.T, theme string, themeConfig viewData) (*goquery.Document, string) {
	t.Helper()
	previous := translator.Trans
	t.Cleanup(func() { translator.Trans = previous })
	previousSystem := solitudes.System
	t.Cleanup(func() { solitudes.System = previousSystem })
	conf := &model.Config{}
	conf.Site.Domain = "example.test"
	solitudes.System = &solitudes.SysVariable{Config: conf}
	translator.Reload(theme, "default")
	catalog, _ := translator.Trans.GetTranslator("en")
	tr := &translator.Translator{Translator: catalog, Trans: catalog}
	engine := html.New("resource/themes/site/"+theme+"/templates", ".html")
	setFuncMap(engine)
	if err := engine.Load(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	binding := viewData{"Tr": tr, "Conf": conf, "Theme": themeConfig, "Path": "/missing",
		"Data": viewData{"title": "404 Page Not Found", "msg": "Missing"}}
	if err := engine.Render(&output, "site/error", binding); err != nil {
		t.Fatal(err)
	}
	source := output.String()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return doc, source
}
