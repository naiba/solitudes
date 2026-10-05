package router

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gofiber/fiber/v2"
	"gopkg.in/yaml.v3"

	"github.com/naiba/solitudes/internal/model"
)

func postThemeSettings(t *testing.T, app *fiber.App, encoding string, fields map[string]string) int {
	t.Helper()
	var body bytes.Buffer
	contentType := fiber.MIMEApplicationJSON
	switch encoding {
	case "multipart":
		writer := multipart.NewWriter(&body)
		for key, value := range fields {
			if err := writer.WriteField(key, value); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		contentType = writer.FormDataContentType()
	case "form":
		values := url.Values{}
		for key, value := range fields {
			values.Set(key, value)
		}
		body.WriteString(values.Encode())
		contentType = fiber.MIMEApplicationForm
	default:
		if err := json.NewEncoder(&body).Encode(fields); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/settings", &body)
	req.Header.Set(fiber.HeaderContentType, contentType)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestSettingsThemeConfigReplacesSubmittedMap(t *testing.T) {
	for _, encoding := range []string{"json", "form", "multipart"} {
		for _, tc := range []struct {
			name      string
			yaml      *string
			want      map[string]interface{}
			invalid   bool
			saveFails bool
		}{
			{name: "delete obsolete keys and preserve submitted custom settings", yaml: new("cactus.colorscheme: light\nfolio.customcode: keep\ncustom.enabled: true\ncustom.links: [one, two]\n"), want: map[string]interface{}{
				"cactus.colorscheme": "light", "folio.customcode": "keep", "custom.enabled": true, "custom.links": []interface{}{"one", "two"},
			}},
			{name: "omitted field preserves settings"},
			{name: "empty editor resets", yaml: new("")},
			{name: "empty map resets", yaml: new("{}")},
			{name: "whitespace resets", yaml: new("  \n")},
			{name: "YAML null resets", yaml: new("null\n")},
			{name: "malformed YAML changes nothing", yaml: new("key: ["), invalid: true},
			{name: "sequence rejected", yaml: new("[one, two]"), invalid: true},
			{name: "scalar rejected", yaml: new("hello"), invalid: true},
			{name: "duplicate key rejected", yaml: new("key: one\nkey: two"), invalid: true},
			{name: "save failure rolls back deletion", yaml: new("{}"), saveFails: true},
		} {
			t.Run(encoding+"/"+tc.name, func(t *testing.T) {
				app, cfg, _ := settingsAccountFixture(t)
				if err := os.WriteFile("resource/themes/site/cactus/metadata.json", []byte(`{"id":"cactus","config":{"cactus.colorscheme":"auto"}}`), 0o644); err != nil {
					t.Fatal(err)
				}
				original := map[string]interface{}{"cactus.colorscheme": "dark", "astro-paper.customcode": "obsolete", "folio.customcode": "keep"}
				cfg.Site.ThemeConfig = original
				if err := cfg.Save(); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile("settings.yml")
				if err != nil {
					t.Fatal(err)
				}
				if tc.saveFails {
					cfg.ConfigFilePath = filepath.Join(t.TempDir(), "missing", "settings.yml")
				}
				fields := map[string]string{"site_title": "Updated title"}
				if tc.yaml != nil {
					fields["theme_config"] = *tc.yaml
				}
				status := postThemeSettings(t, app, encoding, fields)
				if tc.invalid || tc.saveFails {
					wantStatus := http.StatusBadRequest
					if tc.saveFails {
						wantStatus = http.StatusInternalServerError
					}
					if status != wantStatus || !reflect.DeepEqual(cfg.Site.ThemeConfig, original) || cfg.Site.SpaceName != "" {
						t.Fatalf("rejected update changed config: status=%d config=%+v", status, cfg.Site)
					}
					after, err := os.ReadFile("settings.yml")
					if err != nil || !bytes.Equal(before, after) {
						t.Fatalf("rejected update changed saved config: %v", err)
					}
					return
				}
				if status != http.StatusOK {
					t.Fatalf("save settings: %d", status)
				}
				want := tc.want
				if tc.yaml == nil {
					want = original
				} else if want == nil {
					want = map[string]interface{}{"cactus.colorscheme": "auto"}
				}
				if !reflect.DeepEqual(cfg.Site.ThemeConfig, want) {
					t.Fatalf("running settings: got %#v, want %#v", cfg.Site.ThemeConfig, want)
				}
				data, err := os.ReadFile("settings.yml")
				if err != nil {
					t.Fatal(err)
				}
				var restored model.Config
				if err := yaml.Unmarshal(data, &restored); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(restored.Site.ThemeConfig, want) {
					t.Fatalf("saved settings: got %#v, want %#v", restored.Site.ThemeConfig, want)
				}
				if original["astro-paper.customcode"] != "obsolete" || original["cactus.colorscheme"] != "dark" {
					t.Fatal("mutated previous config map")
				}
			})
		}
	}
}

func TestThemeConfigDeletionRequiresAdministrator(t *testing.T) {
	for _, role := range []model.Role{model.RoleEditor, model.RoleUser, ""} {
		t.Run(string(role), func(t *testing.T) {
			app, cfg, account := settingsAccountFixture(t)
			account.Role = role
			if role == "" {
				app = fiber.New()
				app.Post("/admin/settings", requireAdmin, settingsHandler)
			}
			cfg.Site.ThemeConfig = map[string]interface{}{"keep": "unchanged"}
			if status := postThemeSettings(t, app, "multipart", map[string]string{"theme_config": "{}"}); status != http.StatusForbidden {
				t.Fatalf("non-admin could save settings: %d", status)
			}
			if cfg.Site.ThemeConfig["keep"] != "unchanged" {
				t.Fatal("non-admin deleted theme configuration")
			}
			if _, err := os.Stat(cfg.ConfigFilePath); !os.IsNotExist(err) {
				t.Fatalf("non-admin wrote config: %v", err)
			}
		})
	}
}
