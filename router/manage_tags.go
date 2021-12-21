package router

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/pkg/translator"
)

func tagsManagePage(c *fiber.Ctx) error {
	var tags []string
	rows, err := solitudes.System.DB.Raw(`select count(*), unnest(articles.tags) t from articles group by t order by count desc`).Rows()
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var line string
			var count int
			rows.Scan(&count, &line)
			tags = append(tags, line)
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
