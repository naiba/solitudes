package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func newCSRFGuardTestApp() *testHTTPRouter {
	app := newTestRouter()
	app.Use(csrfGuard)
	handler := func(writer http.ResponseWriter, httpRequest *http.Request) error {
		return writeText(writer, http.StatusOK, "ok")
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
	req.Header.Set("Host", "example.com")
	resp, err := testRequest(app, req)
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
		"Origin": "https://attacker.example",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCSRFGuardRejectsCrossOriginRefererWhenOriginMissing(t *testing.T) {
	resp := performCSRFRequest(t, http.MethodPost, map[string]string{
		"Referer": "https://attacker.example/admin",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCSRFGuardAllowsSameOriginPost(t *testing.T) {
	resp := performCSRFRequest(t, http.MethodPost, map[string]string{
		"Origin":  "http://example.com",
		"Referer": "http://example.com/admin",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestCSRFGuardAllowsSameOriginRefererOnly(t *testing.T) {
	resp := performCSRFRequest(t, http.MethodPost, map[string]string{
		"Referer": "http://example.com/admin/settings",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestCSRFGuardRejectsCrossOriginDelete(t *testing.T) {
	resp := performCSRFRequest(t, http.MethodDelete, map[string]string{
		"Origin": "https://attacker.example",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCSRFGuardRejectsSameHostDifferentScheme(t *testing.T) {
	resp := performCSRFRequest(t, http.MethodPost, map[string]string{
		"Origin": "https://example.com",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCSRFGuardRejectsSameHostDifferentPort(t *testing.T) {
	resp := performCSRFRequest(t, http.MethodPost, map[string]string{
		"Origin": "http://example.com:444",
	})
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCSRFGuardAllowsSameOriginWithPort(t *testing.T) {
	app := newCSRFGuardTestApp()
	req := httptest.NewRequest(http.MethodPost, "/ok", nil)
	req.Host = "example.com:8080"
	req.Header.Set("Host", "example.com:8080")
	req.Header.Set("Origin", "http://example.com:8080")
	resp, err := testRequest(app, req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
