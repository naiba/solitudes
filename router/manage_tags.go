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
	var tags []string
	var counts []int
	rows, err := solitudes.System.DB.Raw(`select count(*), unnest(articles.tags) t from articles group by t order by count desc`).Rows()
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var line string
			var count int
			rows.Scan(&count, &line)
			tags = append(tags, line)
			counts = append(counts, count)
		}
	}
	c.Status(http.StatusOK).Render("admin/tags", injectSiteData(c, fiber.Map{
		"title":  c.Locals(solitudes.CtxTranslator).(*translator.Translator).T("manage_tags"),
		"tags":   tags,
		"counts": counts,
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

// searchTags 搜索标签
func searchTags(c *fiber.Ctx) error {
	query := strings.TrimSpace(c.Query("q"))
	var tags []string
	rows, err := solitudes.System.DB.Raw(`SELECT DISTINCT unnest(tags) as tag FROM articles WHERE array_to_string(tags, ',') ILIKE ? ORDER BY tag LIMIT 20`, "%"+query+"%").Rows()
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var tag string
			rows.Scan(&tag)
			tags = append(tags, tag)
		}
	}
	return c.JSON(tags)
}

// searchBooks 搜索专栏
func searchBooks(c *fiber.Ctx) error {
	query := strings.TrimSpace(c.Query("q"))
	var books []model.Article
	solitudes.System.DB.Where("is_book = ? AND (title ILIKE ? OR slug ILIKE ?)", true, "%"+query+"%", "%"+query+"%").
		Select("id", "title", "slug").
		Order("created_at DESC").
		Limit(20).
		Find(&books)

	type BookResult struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Slug  string `json:"slug"`
	}
	results := make([]BookResult, len(books))
	for i, book := range books {
		results[i] = BookResult{
			ID:    book.ID,
			Title: book.Title,
			Slug:  book.Slug,
		}
	}
	return c.JSON(results)
}
