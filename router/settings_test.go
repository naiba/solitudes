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
	solitudes.System.Config.ConfigFilePath = "/tmp/solitudes-test-config.yml"
	app.Post("/settings", func(c *fiber.Ctx) error {
		// Ensure theme directories exist for validation
		os.MkdirAll("resource/themes/site/cactus", 0755)
		os.WriteFile("resource/themes/site/cactus/metadata.json", []byte(`{"name":"Cactus","id":"cactus","config":{"cactus.customcode":"string","cactus.headermenus":"array","cactus.footermenus":"array"}}`), 0644)
		os.MkdirAll("resource/themes/admin/default", 0755)
		os.WriteFile("resource/themes/admin/default/metadata.json", []byte(`{"name":"Default","id":"default"}`), 0644)
		defer func() {
			os.RemoveAll("resource")
		}()
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
