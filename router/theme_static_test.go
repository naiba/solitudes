package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

type themeStaticTestViews struct{}

func (themeStaticTestViews) Load() error { return nil }

func (themeStaticTestViews) Render(w io.Writer, name string, _ interface{}, _ ...string) error {
	_, err := io.WriteString(w, name)
	return err
}

func newThemeStaticTestApp(t *testing.T) *fiber.App {
	t.Helper()

	cfg := &model.Config{}
	cfg.Site.Theme = "cactus"
	cfg.Site.ThemeConfig = map[string]interface{}{}
	cfg.Admin.Theme = "default"
	solitudes.System = &solitudes.SysVariable{Config: cfg}
	translator.Init()

	app := fiber.New(fiber.Config{Views: themeStaticTestViews{}})
	app.Use(trans, auth)
	app.Get("/static/:kind/:theme/*", themeStaticHandler)
	return app
}

func writeThemeStaticTestFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
}

func TestThemeStaticHandlerServesThemeAsset(t *testing.T) {
	t.Chdir(t.TempDir())
	writeThemeStaticTestFile(t, "resource/themes/site/cactus/static/css/main.css", "body { color: #123; }")

	app := newThemeStaticTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/static/site/cactus/css/main.css", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request theme asset: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %q", resp.StatusCode, http.StatusOK, body)
	}
	if string(body) != "body { color: #123; }" {
		t.Fatalf("body = %q", body)
	}
	if got := resp.Header.Get("Cache-Control"); got != "public, max-age=2592000" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/css") {
		t.Fatalf("Content-Type = %q, want text/css", got)
	}
}

func TestThemeStaticHandlerBlocksTraversalOutsideThemeRoot(t *testing.T) {
	t.Chdir(t.TempDir())
	const secretConfig = "database: postgres://secret.example/solitudes"
	writeThemeStaticTestFile(t, "resource/themes/site/cactus/static/css/main.css", "body {}")
	writeThemeStaticTestFile(t, "data/conf.yml", secretConfig)

	app := newThemeStaticTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/static/site/cactus/../../../../../data/conf.yml", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request traversal path: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %q", resp.StatusCode, http.StatusNotFound, body)
	}
	if strings.Contains(string(body), secretConfig) {
		t.Fatalf("traversal response leaked config: %q", body)
	}
}
