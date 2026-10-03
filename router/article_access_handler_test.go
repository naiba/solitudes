package router

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func TestPublishRejectsInvalidAudienceAndMalformedAccessBeforeWriting(t *testing.T) {
	for _, role := range []model.Role{model.RoleAdmin, model.RoleEditor} {
		app := fiber.New()
		app.Use(func(c *fiber.Ctx) error {
			c.Locals(solitudes.CtxAccount, &model.Account{ID: "author", Role: role})
			return c.Next()
		})
		app.Post("/publish", publishHandler)
		for _, tc := range []struct{ visibility, content string }{
			{"unknown", "body"}, {"members; DROP TABLE articles", "body"},
			{"public", "```access:members\nsecret"},
			{"public", "```access:unknown\nsecret\n```"},
		} {
			form := url.Values{"visibility": {tc.visibility}, "content": {tc.content}, "slug": {"test"}, "title": {"Test"}, "template": {"1"}}
			req := httptest.NewRequest(http.MethodPost, "/publish", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("%s accepted malformed publish: %d", role, resp.StatusCode)
			}
		}
	}
}
