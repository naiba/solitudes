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

func TestThemeStaticHandlerRejectsInvalidThemeNameAndSymlink(t *testing.T) {
	t.Chdir(t.TempDir())
	writeThemeStaticTestFile(t, "resource/themes/site/cactus/static/css/main.css", "safe")
	writeThemeStaticTestFile(t, "data/conf.yml", "secret config")
	if err := os.Symlink(filepath.Join("..", "..", "..", "..", "..", "data", "conf.yml"),
		"resource/themes/site/cactus/static/config.css"); err != nil {
		t.Fatal(err)
	}
	app := newThemeStaticTestApp(t)
	for _, requestPath := range []string{
		"/static/site/%2e%2e/css/main.css",
		"/static/site/cactus/config.css",
		"/static/admin/%2e%2e/css/main.css",
	} {
		t.Run(requestPath, func(t *testing.T) {
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, requestPath, nil), -1)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusNotFound || strings.Contains(string(body), "secret config") {
				t.Fatalf("status = %d, body = %q", resp.StatusCode, body)
			}
		})
	}
}

func TestThemePreviewRejectsTraversalAndSymlink(t *testing.T) {
	t.Chdir(t.TempDir())
	writeThemeStaticTestFile(t, "resource/themes/site/cactus/screenshot.png", "preview")
	writeThemeStaticTestFile(t, "data/conf.yml", "secret config")
	app := fiber.New()
	app.Get("/admin/theme/preview/:kind/:name", themePreview)
	for _, requestPath := range []string{
		"/admin/theme/preview/site/cactus",
		"/admin/theme/preview/site/%2e%2e",
		"/admin/theme/preview/site/%2e%2e%2f%2e%2e",
	} {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, requestPath, nil), -1)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if requestPath == "/admin/theme/preview/site/cactus" && resp.StatusCode != http.StatusOK {
			t.Fatalf("valid preview status = %d", resp.StatusCode)
		}
		if requestPath != "/admin/theme/preview/site/cactus" && resp.StatusCode == http.StatusOK {
			t.Fatalf("traversal preview status = %d", resp.StatusCode)
		}
	}
	if err := os.Remove("resource/themes/site/cactus/screenshot.png"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "..", "..", "..", "data", "conf.yml"),
		"resource/themes/site/cactus/screenshot.png"); err != nil {
		t.Fatal(err)
	}
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/admin/theme/preview/site/cactus", nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("symlink preview status = %d, want 404", resp.StatusCode)
	}
}

func TestThemePreviewUsesSameRootResourceForBothKinds(t *testing.T) {
	t.Chdir(t.TempDir())
	app := fiber.New()
	app.Get("/preview/:kind/:name", themePreview)
	writeThemeStaticTestFile(t, "data/conf.yml", "private configuration")
	for _, kind := range []string{"site", "admin"} {
		t.Run(kind, func(t *testing.T) {
			root := filepath.Join("resource", "themes", kind, "sample")
			writeThemeStaticTestFile(t, filepath.Join(root, "screenshot.png"), kind+" preview")
			for _, name := range []string{"sample", "missing", "%2e%2e", "%2e%2e%2fsample"} {
				response, err := app.Test(httptest.NewRequest(http.MethodGet, "/preview/"+kind+"/"+name, nil), -1)
				if err != nil {
					t.Fatal(err)
				}
				body, _ := io.ReadAll(response.Body)
				response.Body.Close()
				if name == "sample" {
					if response.StatusCode != 200 || string(body) != kind+" preview" || !strings.Contains(response.Header.Get("Content-Type"), "image/png") {
						t.Fatalf("invalid %s preview: %d %s", kind, response.StatusCode, body)
					}
				} else if response.StatusCode != 404 {
					t.Fatalf("invalid preview allowed: %s", name)
				}
			}
			file := filepath.Join(root, "screenshot.png")
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join("..", "..", "..", "..", "data", "conf.yml"), file); err != nil {
				t.Fatal(err)
			}
			response, err := app.Test(httptest.NewRequest(http.MethodGet, "/preview/"+kind+"/sample", nil), -1)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != 404 || strings.Contains(string(body), "private configuration") {
				t.Fatal("preview symlink leaked external content")
			}
		})
	}
}
