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
	"github.com/stretchr/testify/assert"
)

func TestLogoServing(t *testing.T) {
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
	app.Get("/logo.png", logoHandler)
	app.Get("/favicon.ico", faviconHandler)

	// Test serving logo
	req := httptest.NewRequest("GET", "/logo.png", nil)
	resp, err := app.Test(req)
	assert.Nil(t, err)
	// We expect 404 because "data/upload/logo.png" does not exist in test env
	// and theme fallback also fails because theme path doesn't exist.
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestUploadStaticHandlerCannotReadOutsideUpload(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("data/upload", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("data/upload/photo.png", []byte("public photo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("data/conf.yml", []byte("secret config"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "conf.yml"), "data/upload/leak.png"); err != nil {
		t.Fatal(err)
	}
	app := newThemeStaticTestApp(t)
	app.Get("/upload/*", uploadStaticHandler)
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/upload/photo.png", http.StatusOK},
		{"/upload/leak.png", http.StatusNotFound},
		{"/upload/../conf.yml", http.StatusNotFound},
		{"/upload/%2e%2e/conf.yml", http.StatusNotFound},
	} {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, tc.path, nil), -1)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tc.want || strings.Contains(string(body), "secret config") {
			t.Errorf("%s: status = %d, body = %q", tc.path, resp.StatusCode, body)
		}
	}
}

func TestLogoHandlerCannotFollowUploadSymlink(t *testing.T) {
	t.Chdir(t.TempDir())
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	cfg := &model.Config{}
	cfg.Site.Theme = "cactus"
	solitudes.System = &solitudes.SysVariable{Config: cfg}
	if err := os.MkdirAll("data/upload", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("data/conf.yml", []byte("secret config"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "conf.yml"), "data/upload/logo.png"); err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Get("/logo.png", logoHandler)
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/logo.png", nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNotFound || strings.Contains(string(body), "secret config") {
		t.Fatalf("status = %d, body = %q", resp.StatusCode, body)
	}
}
