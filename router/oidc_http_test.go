package router

import (
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func TestOIDCHTTPResponseHeaders(t *testing.T) {
	previous := solitudes.System
	t.Cleanup(func() { solitudes.System = previous })
	config := &model.Config{}
	config.Site.Domain = "localhost:8080"
	solitudes.System = &solitudes.SysVariable{Config: config}
	for _, tc := range []struct {
		name, contentType, body string
		status                  int
	}{
		{"token", "application/json", `{"access_token":"test-only","token_type":"Bearer"}`, http.StatusOK},
		{"protocol-error", "application/json", `{"error":"invalid_request"}`, http.StatusBadRequest},
		{"userinfo-error", "text/plain; charset=utf-8", "access token missing\n", http.StatusUnauthorized},
		{"redirect", "text/html; charset=utf-8", "redirect", http.StatusFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Use(func(c *fiber.Ctx) error {
				c.Set("X-Content-Type-Options", "nosniff")
				c.Set("Cache-Control", "public, max-age=600")
				return c.Next()
			})
			app.Use(oidcHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.Header().Add("Vary", "Origin")
				w.Header().Add("Vary", "Accept")
				w.Header().Set("Cache-Control", "private")
				http.SetCookie(w, &http.Cookie{Name: "first", Value: "one", Path: "/", HttpOnly: true})
				http.SetCookie(w, &http.Cookie{Name: "second", Value: "two", Path: "/", HttpOnly: true})
				if tc.status == http.StatusFound {
					w.Header().Set("Location", "http://localhost:9090/callback?state=example")
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})))
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/oauth/token", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil || resp.StatusCode != tc.status || string(body) != tc.body {
				t.Fatalf("status/body changed: status=%d error=%v", resp.StatusCode, err)
			}
			if values := resp.Header.Values("Content-Type"); len(values) != 1 || values[0] != tc.contentType {
				t.Errorf("Content-Type must replace Fiber's default: %q", values)
			}
			if _, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil {
				t.Errorf("invalid media type: %v", err)
			}
			values := resp.Header.Values("Vary")
			slices.Sort(values)
			if !slices.Equal(values, []string{"Accept", "Origin"}) {
				t.Errorf("header values were merged or lost: %q", values)
			}
			cookies := resp.Cookies()
			if len(cookies) != 2 || cookies[0].Name != "first" || cookies[1].Name != "second" || !cookies[0].HttpOnly || !cookies[1].HttpOnly {
				t.Error("Set-Cookie fields must remain separate")
			}
			if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
				t.Error("protocol response lost cache or security protection")
			}
			if tc.status == http.StatusFound && resp.Header.Get("Location") != "http://localhost:9090/callback?state=example" {
				t.Error("redirect target changed")
			}
		})
	}
}
