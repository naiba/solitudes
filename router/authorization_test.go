package router

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func TestMediaDeleteRequiresLoginAndSameOrigin(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("data/upload", 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join("data/upload", "photo.jpg")
	if err := os.WriteFile(file, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	cfg := &model.Config{}
	cfg.User.Token = "valid-token"
	cfg.User.TokenExpires = time.Now().Add(time.Hour).Unix()
	solitudes.System = &solitudes.SysVariable{Config: cfg}
	app := fiber.New()
	app.Use(auth, csrfGuard)
	app.Delete("/admin/media", loginRequired, mediaHandler)

	for _, tc := range []struct {
		name, token, origin string
		want                int
		deleted             bool
	}{
		{"anonymous", "", "http://example.com", http.StatusFound, false},
		{"invalid token", "not-valid", "http://example.com", http.StatusFound, false},
		{"cross origin", "valid-token", "https://evil.example", http.StatusForbidden, false},
		{"missing origin", "valid-token", "", http.StatusForbidden, false},
		{"authorized", "valid-token", "http://example.com", http.StatusOK, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/admin/media?name=photo.jpg", nil)
			req.Host = "example.com"
			req.Header.Set(fiber.HeaderHost, "example.com")
			if tc.origin != "" {
				req.Header.Set(fiber.HeaderOrigin, tc.origin)
			}
			if tc.token != "" {
				req.AddCookie(&http.Cookie{Name: solitudes.AuthCookie, Value: tc.token})
			}
			resp, err := app.Test(req, -1)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.want)
			}
			_, err = os.Stat(file)
			if tc.deleted != os.IsNotExist(err) {
				t.Fatalf("file existence mismatch: %v", err)
			}
		})
	}
}

func TestExpiredAdminTokenCannotDeleteMedia(t *testing.T) {
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	cfg := &model.Config{}
	cfg.User.Token = "expired"
	cfg.User.TokenExpires = time.Now().Add(-time.Minute).Unix()
	solitudes.System = &solitudes.SysVariable{Config: cfg}
	app := fiber.New()
	app.Use(auth)
	app.Delete("/admin/media", loginRequired, func(c *fiber.Ctx) error {
		return c.SendStatus(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodDelete, "/admin/media", nil)
	req.AddCookie(&http.Cookie{Name: solitudes.AuthCookie, Value: "expired"})
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expired token status = %d, want 302", resp.StatusCode)
	}
}
