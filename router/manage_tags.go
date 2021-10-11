package router

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func tagsManagePage(c *fiber.Ctx) error {
	tagsUnique := make(map[string]struct{})
	var tags []string
	var articles []model.Article
	solitudes.System.DB.Select("tags").Find(&articles)
	for i := 0; i < len(articles); i++ {
		for j := 0; j < len(articles[i].Tags); j++ {
			if _, has := tagsUnique[articles[i].Tags[j]]; has {
				continue
			}
			tagsUnique[articles[i].Tags[j]] = struct{}{}
			tags = append(tags, articles[i].Tags[j])
		}
	}
	c.Status(http.StatusOK).Render("admin/tags", injectSiteData(c, fiber.Map{
		"title": c.Locals(solitudes.CtxTranslator).(*translator.Translator).T("manage_tags"),
		"tags":  tags,
	}))
	return nil
}

func deleteTag(c *fiber.Ctx) error {
	tagName := c.Query("tagName")
	return solitudes.System.DB.Exec("UPDATE articles SET tags = array_remove(tags, ?);", tagName).Error
}

func renameTag(c *fiber.Ctx) error {
	oldTagName := c.Query("oldTagName")
	newTagName := strings.TrimSpace(c.Query("newTagName"))
	if newTagName == "" {
		return errors.New("empty tag name")
	}
	return solitudes.System.DB.Exec("UPDATE articles SET tags = array_replace(tags, ?, ?);", oldTagName, newTagName).Error
}
