package router

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func tagsManagePage(c *fiber.Ctx) error {
	data, err := pagedTags(c, solitudes.System.DB)
	if err != nil {
		return err
	}
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	data["title"] = tr.T("manage_tags")
	return c.Status(http.StatusOK).Render("admin/tags", injectSiteData(c, data))
}

func deleteTag(c *fiber.Ctx) error {
	tagName := c.Query("tagName")
	if err := solitudes.System.DB.Exec("UPDATE articles SET tags = array_remove(tags, ?);", tagName).Error; err != nil {
		return fmt.Errorf("failed to delete tag: %w", err)
	}
	return nil
}

func renameTag(c *fiber.Ctx) error {
	oldTagName := c.Query("oldTagName")
	newTagName := strings.TrimSpace(c.Query("newTagName"))
	if newTagName == "" {
		return errors.New("empty tag name")
	}
	if err := solitudes.System.DB.Exec("UPDATE articles SET tags = array_replace(tags, ?, ?);", oldTagName, newTagName).Error; err != nil {
		return fmt.Errorf("failed to rename tag: %w", err)
	}
	return nil
}

// searchTags 搜索标签
func searchTags(c *fiber.Ctx) error {
	account := currentAccount(c)
	query := strings.TrimSpace(c.Query("q"))
	var tags []string
	rows, err := readableArticles(solitudes.System.DB, account).Model(&model.Article{}).
		Select("DISTINCT unnest(tags) AS tag").Where("array_to_string(tags, ',') ILIKE ?", "%"+query+"%").Order("tag").Limit(20).Rows()
	if err != nil {
		return fmt.Errorf("failed to search tags: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return fmt.Errorf("failed to scan searched tag: %w", err)
		}
		tags = append(tags, tag)
	}
	return c.JSON(tags)
}

// searchBooks 搜索专栏
func searchBooks(c *fiber.Ctx) error {
	query := strings.TrimSpace(c.Query("q"))
	var books []model.Article
	if err := readableArticles(solitudes.System.DB, currentAccount(c)).Where("is_book = ? AND (title ILIKE ? OR slug ILIKE ?)", true, "%"+query+"%", "%"+query+"%").
		Select("id", "title", "slug").
		Order("created_at DESC").
		Limit(20).
		Find(&books).Error; err != nil {
		return fmt.Errorf("failed to search books: %w", err)
	}

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
