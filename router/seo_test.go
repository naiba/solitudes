package router

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func TestSiteMetadataUsesMeaningfulTextWithoutKeywordPadding(t *testing.T) {
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	for _, tc := range []struct {
		name, siteDesc, title, desc, wantTitle, wantDesc string
	}{
		{"home", "Stories together", "", "", "Community - Stories together", "Stories together"},
		{"article", "Stories together", "Short story", "A short excerpt.", "Short story - Community", "A short excerpt."},
		{"empty description", "", "", "", "Community", ""},
		{"duplicate description", "Community", "", "", "Community", "Community"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := &model.Config{}
			config.Site.SpaceName = "Community"
			config.Site.SpaceDesc = tc.siteDesc
			config.Site.SpaceKeywords = "keyword-padding-must-not-appear"
			solitudes.System = &solitudes.SysVariable{Config: config}
			app := fiber.New()
			app.Get("/", func(c *fiber.Ctx) error {
				c.Locals(solitudes.CtxTranslator, &translator.Translator{})
				data := injectSiteData(c, fiber.Map{"title": tc.title, "desc": tc.desc})
				return c.JSON(fiber.Map{"title": data["Title"], "desc": data["Desc"], "path": data["Path"]})
			})
			resp, err := app.Test(httptest.NewRequest("GET", "/?utm_source=newsletter", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var result map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			if result["title"] != tc.wantTitle || result["desc"] != tc.wantDesc || result["path"] != "/" {
				t.Fatalf("unexpected metadata: %+v", result)
			}
		})
	}
}
