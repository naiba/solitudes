package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestAccountSignInPreservesDestinationOnlyForSafeReads(t *testing.T) {
	app := fiber.New()
	app.Get("/account", requireAccount)
	app.Post("/account", requireAccount)
	for _, tc := range []struct{ method, path, want string }{
		{http.MethodGet, "/account?section=security", "/login?return_to=%2Faccount%3Fsection%3Dsecurity"},
		{http.MethodPost, "/account", "/login"},
	} {
		resp, err := app.Test(httptest.NewRequest(tc.method, tc.path, nil))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != tc.want {
			t.Errorf("%s %s: status %d, location %q; want 302 -> %q", tc.method, tc.path, resp.StatusCode, resp.Header.Get("Location"), tc.want)
		}
	}
}
