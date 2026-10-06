package router

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func canonicalURLRedirect(c *fiber.Ctx) error {
	// SEO redirects are for reads only: a 301 on a form/API write can discard
	// the method and body. Let the normal routing and CSRF checks handle writes.
	if c.Method() != fiber.MethodGet && c.Method() != fiber.MethodHead {
		return c.Next()
	}
	path := c.Path()
	if len(path) <= 1 || strings.HasPrefix(path, "//") {
		return c.Next()
	}
	target := strings.TrimRight(path, "/")
	parts := strings.Split(strings.TrimPrefix(target, "/"), "/")
	list := target == "/search" || target == "/readers" || target == "/tags" ||
		parts[0] == "posts" || parts[0] == "books" || strings.HasPrefix(target, "/tags/")
	// Archive page one and zero-padded page numbers represent the same content.
	// Match route shapes, not numeric tags, and keep escaped tags intact.
	archivePage := (len(parts) == 2 && (parts[0] == "posts" || parts[0] == "books")) ||
		(len(parts) == 3 && parts[0] == "tags")
	if archivePage {
		if page, err := listPage(parts[len(parts)-1]); err == nil {
			if page == 1 {
				parts = parts[:len(parts)-1]
			} else {
				parts[len(parts)-1] = strconv.Itoa(page)
			}
			target = "/" + strings.Join(parts, "/")
		}
	}
	if list {
		target += "/"
	}
	// Compare the entire path so repeated trailing slashes are normalized too.
	if target != path {
		if query := string(c.Request().URI().QueryString()); query != "" {
			target += "?" + query
		}
		return c.Redirect(target, http.StatusMovedPermanently)
	}
	return c.Next()
}
