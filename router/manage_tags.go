package router

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func tagsManagePage(writer http.ResponseWriter, httpRequest *http.Request) error {
	data, err := pagedTags(writer, httpRequest, solitudes.System.DB)
	if err != nil {
		return err
	}
	tr := requestValue(httpRequest, solitudes.CtxTranslator).(*translator.Translator)
	data["title"] = tr.T("manage_tags")
	return renderPage(writer, httpRequest, http.StatusOK, "admin/tags", injectSiteData(writer, httpRequest, data))
}

func deleteTag(writer http.ResponseWriter, httpRequest *http.Request) error {
	tagName := queryValue(httpRequest, "tagName")
	if err := solitudes.System.DB.Exec("UPDATE articles SET tags = array_remove(tags, ?);", tagName).Error; err != nil {
		return fmt.Errorf("failed to delete tag: %w", err)
	}
	return nil
}

func renameTag(writer http.ResponseWriter, httpRequest *http.Request) error {
	oldTagName := queryValue(httpRequest, "oldTagName")
	newTagName := strings.TrimSpace(queryValue(httpRequest, "newTagName"))
	if newTagName == "" {
		return errors.New("empty tag name")
	}
	if err := solitudes.System.DB.Exec("UPDATE articles SET tags = array_replace(tags, ?, ?);", oldTagName, newTagName).Error; err != nil {
		return fmt.Errorf("failed to rename tag: %w", err)
	}
	return nil
}

// searchTags 搜索标签
func searchTags(writer http.ResponseWriter, httpRequest *http.Request) error {
	account := currentAccount(writer, httpRequest)
	query := strings.TrimSpace(queryValue(httpRequest, "q"))
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
	return writeJSON(writer, http.StatusOK, tags)
}

// searchBooks 搜索专栏
func searchBooks(writer http.ResponseWriter, httpRequest *http.Request) error {
	query := strings.TrimSpace(queryValue(httpRequest, "q"))
	var books []model.Article
	if err := readableArticles(solitudes.System.DB, currentAccount(writer, httpRequest)).Where("is_book = ? AND (title ILIKE ? OR slug ILIKE ?)", true, "%"+query+"%", "%"+query+"%").
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
	return writeJSON(writer, http.StatusOK, results)
}
