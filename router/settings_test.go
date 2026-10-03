package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

func TestCactusSettings(t *testing.T) {
	t.Chdir(t.TempDir())
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	// Initialize config
	solitudes.System = &solitudes.SysVariable{
		Config: &model.Config{
			Site: struct {
				SpaceName     string
				SpaceDesc     string
				SpaceKeywords string
				Domain        string
				Theme         string
				ThemeConfig   map[string]interface{} `yaml:"theme_config"`
			}{
				Theme: "cactus",
			},
		},
	}

	app := fiber.New()
	// Mock config save
	solitudes.System.Config.ConfigFilePath = "settings.yml"
	app.Post("/settings", func(c *fiber.Ctx) error {
		// Ensure theme directories exist for validation
		os.MkdirAll("resource/themes/site/cactus", 0755)
		os.WriteFile("resource/themes/site/cactus/metadata.json", []byte(`{"name":"Cactus","id":"cactus","config":{"cactus.customcode":"string","cactus.headermenus":"array","cactus.footermenus":"array"}}`), 0644)
		os.MkdirAll("resource/themes/admin/default", 0755)
		os.WriteFile("resource/themes/admin/default/metadata.json", []byte(`{"name":"Default","id":"default"}`), 0644)
		return settingsHandler(c)
	})

	// Mock request payload
	themeConfig := map[string]interface{}{
		"cactus.customcode": "alert('test')",
		"cactus.headermenus": []map[string]interface{}{
			{"name": "Home", "link": "/"},
		},
		"cactus.footermenus": []map[string]interface{}{
			{"name": "About", "link": "/about"},
		},
	}
	tcBytes, _ := yaml.Marshal(themeConfig)

	reqBody := settingsRequest{
		SiteTheme:   "cactus",
		AdminTheme:  "default",
		ThemeConfig: string(tcBytes),
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/settings", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	assert.Nil(t, err)
	if resp.StatusCode != http.StatusOK {
		buf := new(strings.Builder)
		io.Copy(buf, resp.Body)
		t.Logf("Response body: %s", buf.String())
	}
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify settings were saved to theme config
	config := solitudes.System.Config.Site.ThemeConfig
	assert.Equal(t, "alert('test')", config["cactus.customcode"])

	// Assert slice length instead of type assertion on slice of concrete type
	headerMenus := config["cactus.headermenus"].([]interface{})
	assert.Equal(t, 1, len(headerMenus))
	menu0 := headerMenus[0].(map[string]interface{})
	assert.Equal(t, "Home", menu0["name"])
}

func TestInvalidSettingsDoNotChangeConfigOrSave(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("resource/themes/site/cactus", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("resource/themes/site/cactus/metadata.json", []byte(`{"id":"cactus"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("resource/themes/admin/default", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("resource/themes/admin/default/metadata.json", []byte(`{"id":"default"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	cfg := &model.Config{ConfigFilePath: "saved.yml"}
	cfg.Site.Theme = "cactus"
	cfg.Admin.Theme = "default"
	cfg.Site.SpaceName = "Original title"
	cfg.Site.ThemeConfig = map[string]interface{}{"keep": "unchanged"}
	solitudes.System = &solitudes.SysVariable{Config: cfg}
	app := fiber.New()
	app.Post("/settings", settingsHandler)

	for _, payload := range []string{
		`{"site_title":"Tampered","admin_theme":"glacie"}`,
		`{"site_title":"Tampered","admin_theme":"../default"}`,
		`{"site_title":"Tampered","site_theme":"../data","theme_config":"keep: changed"}`,
		`{"site_title":"Tampered","site_theme":"cactus","theme_config":"[not a map]"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(payload))
		req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Fatalf("invalid settings status = %d, payload = %s", resp.StatusCode, payload)
		}
		if cfg.Admin.Theme != "default" || cfg.Site.Theme != "cactus" || cfg.Site.SpaceName != "Original title" || cfg.Site.ThemeConfig["keep"] != "unchanged" {
			t.Fatalf("invalid settings changed configuration: %+v", cfg.Site)
		}
		if _, err := os.Stat("saved.yml"); !os.IsNotExist(err) {
			t.Fatalf("invalid settings wrote config: %v", err)
		}
	}
}
