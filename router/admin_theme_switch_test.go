package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"gopkg.in/yaml.v3"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

// Two installed themes are created only in an isolated filesystem. Shipping
// one theme must not disable theme discovery, selection or hot reload.
func TestAdminThemeSwitchReloadsTemplatesTranslationsAndPersists(t *testing.T) {
	t.Chdir(t.TempDir())
	previousSystem, previousEngine, previousTranslations := solitudes.System, globalDynamicEngine.engine, translator.Trans
	t.Cleanup(func() {
		solitudes.System = previousSystem
		globalDynamicEngine.engine = previousEngine
		translator.Trans = previousTranslations
	})
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("resource/themes/site/cactus/metadata.json", `{"id":"cactus"}`)
	write("resource/themes/site/cactus/templates/marker.html", `{{define "site/marker"}}SITE{{end}}`)
	for _, name := range []string{"default", "custom-admin"} {
		root := filepath.Join("resource", "themes", "admin", name)
		write(filepath.Join(root, "metadata.json"), `{"id":"`+name+`","name":"`+name+`"}`)
		write(filepath.Join(root, "templates", "marker.html"), `{{define "admin/marker"}}`+name+`|{{.Tr.T "switch_marker"}}{{end}}`)
		write(filepath.Join(root, "templates", "settings.html"), `{{define "admin/settings"}}{{range .Data.themeListAdmin}}{{.ID}} {{end}}{{end}}`)
		write(filepath.Join(root, "translations", "en.json"), `[{"locale":"en","key":"switch_marker","trans":"`+name+`-en"},{"locale":"en","key":"site_settings","trans":"Settings"}]`)
		write(filepath.Join(root, "translations", "zh.json"), `[{"locale":"zh","key":"switch_marker","trans":"`+name+`-zh"}]`)
		write(filepath.Join(root, "static", "marker.css"), name+"-asset")
	}
	config := &model.Config{ConfigFilePath: "config.yml"}
	config.Site.Theme, config.Admin.Theme = "cactus", "default"
	config.Site.ThemeConfig = map[string]interface{}{"retained": "site-value"}
	solitudes.System = &solitudes.SysVariable{Config: config}
	if err := LoadTemplates(); err != nil {
		t.Fatal(err)
	}
	role := model.RoleAdmin
	app := fiber.New(fiber.Config{Views: globalDynamicEngine})
	app.Use(trans, func(c *fiber.Ctx) error {
		c.Locals(solitudes.CtxAccount, &model.Account{Role: role})
		return c.Next()
	})
	app.Get("/admin/settings", requireAdmin, settings)
	app.Post("/admin/settings", requireAdmin, settingsHandler)
	app.Get("/admin/marker", requireAdmin, func(c *fiber.Ctx) error {
		return c.Render("admin/marker", injectSiteData(c, fiber.Map{}))
	})
	app.Get("/site-marker", func(c *fiber.Ctx) error { return c.Render("site/marker", fiber.Map{}) })
	app.Get("/static/:kind/:theme/*", themeStaticHandler)
	request := func(method, path, body, locale string, status int) string {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
		req.Header.Set(fiber.HeaderAcceptLanguage, locale)
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		result, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != status {
			t.Fatalf("%s %s: %d, want %d: %s", method, path, resp.StatusCode, status, result)
		}
		return string(result)
	}
	list := request(http.MethodGet, "/admin/settings", "", "en", 200)
	if !strings.Contains(list, "default") || !strings.Contains(list, "custom-admin") {
		t.Fatalf("installed themes missing from settings: %q", list)
	}
	for _, name := range []string{"custom-admin", "default", "custom-admin"} {
		body, _ := json.Marshal(settingsRequest{AdminTheme: name})
		request(http.MethodPost, "/admin/settings", string(body), "en", 200)
		if config.Admin.Theme != name || config.Site.Theme != "cactus" || config.Site.ThemeConfig["retained"] != "site-value" {
			t.Fatalf("switch changed unrelated site settings: %+v", config)
		}
		for _, locale := range []string{"en", "zh"} {
			if got := request(http.MethodGet, "/admin/marker", "", locale, 200); got != name+"|"+name+"-"+locale {
				t.Fatalf("templates/translations not reloaded: %q", got)
			}
		}
		if got := request(http.MethodGet, "/site-marker", "", "en", 200); got != "SITE" {
			t.Fatalf("admin switch changed site rendering: %q", got)
		}
		if got := request(http.MethodGet, "/static/admin/"+name+"/marker.css", "", "en", 200); got != name+"-asset" {
			t.Fatalf("theme asset not served: %q", got)
		}
		content, err := os.ReadFile("config.yml")
		if err != nil {
			t.Fatal(err)
		}
		var saved model.Config
		if err := yaml.Unmarshal(content, &saved); err != nil || saved.Admin.Theme != name {
			t.Fatalf("theme selection not persisted: %v / %q", err, saved.Admin.Theme)
		}
	}
	before, err := os.ReadFile("config.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"not-installed", "../default"} {
		body, _ := json.Marshal(settingsRequest{AdminTheme: name})
		request(http.MethodPost, "/admin/settings", string(body), "en", 400)
	}
	for _, denied := range []model.Role{model.RoleEditor, model.RoleUser} {
		role = denied
		request(http.MethodPost, "/admin/settings", `{"admin_theme":"default"}`, "en", 403)
		request(http.MethodGet, "/admin/settings", "", "en", 403)
	}
	after, err := os.ReadFile("config.yml")
	if err != nil || string(before) != string(after) || config.Admin.Theme != "custom-admin" {
		t.Fatal("rejected switch changed saved or running configuration")
	}
}
