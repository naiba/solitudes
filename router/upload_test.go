package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestLogoServing(t *testing.T) {
	// Initialize config
	solitudes.System = &solitudes.SysVeriable{
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
