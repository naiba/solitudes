package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
)

func newCSRFGuardTestApp() *fiber.App {
	app := fiber.New()
	app.Use(csrfGuard)
	handler := func(c *fiber.Ctx) error {
		return c.SendString("ok")
	}
	app.Get("/ok", handler)
	app.Post("/ok", handler)
	app.Delete("/ok", handler)
	return app
}

func performCSRFRequest(t *testing.T, method string, headers map[string]string) *http.Response {
	t.Helper()
	app := newCSRFGuardTestApp()
	req := httptest.NewRequest(method, "/ok", strings.NewReader(""))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Host = "example.com"
	req.Header.Set(fiber.HeaderHost, "example.com")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test error: %v", err)
	}
	return resp
}

func TestCSRFGuardAllowsSafeMethods(t *testing.T) {
	resp := performCSRFRequest(t, http.MethodGet, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestCSRFGuardRejectsMissingOriginAndReferer(t *testing.T) {
	resp := performCSRFRequest(t, http.MethodPost, nil)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCSRFGuardRejectsCrossOriginPost(t *testing.T) {
	resp := performCSRFRequest(t, http.MethodPost, map[string]string{
		fiber.HeaderOrigin: "https://attacker.example",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCSRFGuardRejectsCrossOriginRefererWhenOriginMissing(t *testing.T) {
	resp := performCSRFRequest(t, http.MethodPost, map[string]string{
		fiber.HeaderReferer: "https://attacker.example/admin",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCSRFGuardAllowsSameOriginPost(t *testing.T) {
	resp := performCSRFRequest(t, http.MethodPost, map[string]string{
		fiber.HeaderOrigin:  "http://example.com",
		fiber.HeaderReferer: "http://example.com/admin",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestCSRFGuardAllowsSameOriginRefererOnly(t *testing.T) {
	resp := performCSRFRequest(t, http.MethodPost, map[string]string{
		fiber.HeaderReferer: "http://example.com/admin/settings",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestCSRFGuardRejectsCrossOriginDelete(t *testing.T) {
	resp := performCSRFRequest(t, http.MethodDelete, map[string]string{
		fiber.HeaderOrigin: "https://attacker.example",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCSRFGuardRejectsSameHostDifferentScheme(t *testing.T) {
	resp := performCSRFRequest(t, http.MethodPost, map[string]string{
		fiber.HeaderOrigin: "https://example.com",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCSRFGuardRejectsSameHostDifferentPort(t *testing.T) {
	resp := performCSRFRequest(t, http.MethodPost, map[string]string{
		fiber.HeaderOrigin: "http://example.com:444",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCSRFGuardAllowsSameOriginWithPort(t *testing.T) {
	app := newCSRFGuardTestApp()
	req := httptest.NewRequest(http.MethodPost, "/ok", nil)
	req.Host = "example.com:8080"
	req.Header.Set(fiber.HeaderHost, "example.com:8080")
	req.Header.Set(fiber.HeaderOrigin, "http://example.com:8080")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
