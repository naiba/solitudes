package router

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/pagination"
	"github.com/naiba/solitudes/pkg/translator"
)

func manageArticle(c *fiber.Ctx) error {
	rawPage := c.Query("page")
	var page int64
	if rawPage != "" {
		var err error
		page, err = strconv.ParseInt(rawPage, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid page format: %w", err)
		}
	}
	var as []model.Article
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.DB,
		Page:    int(page),
		Limit:   20,
		OrderBy: []string{"created_at DESC"},
	}, &as)
	for i := range as {
		as[i].RelatedCount(solitudes.System.DB)
	}
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	return c.Status(http.StatusOK).Render("admin/articles", injectSiteData(c, fiber.Map{
		"title":    tr.T("manage_articles"),
		"articles": as,
		"page":     pg,
	}))
}

func publish(c *fiber.Ctx) error {
	id := c.Query("id")
	var article model.Article
	if id != "" {
		if err := solitudes.System.DB.Take(&article, "id = ?", id).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to fetch article for editing: %w", err)
		}
	}
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	return c.Status(http.StatusOK).Render("admin/publish", injectSiteData(c, fiber.Map{
		"title":     tr.T("publish_article"),
		"templates": solitudes.Templates,
		"article":   article,
	}))
}

func deleteArticle(c *fiber.Ctx) error {
	id := c.Query("id")
	if len(id) < 10 {
		return errors.New("invalid article id")
	}
	var a model.Article
	if err := solitudes.System.DB.Select("id").Preload("ArticleHistories").Take(&a, "id = ?", id).Error; err != nil {
		return fmt.Errorf("failed to find article for deletion: %w", err)
	}
	var indexIDs []string
	indexIDs = append(indexIDs, a.GetIndexID())
	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		// 删除文章
		if err := tx.Delete(&model.Article{}, "id = ?", a.ID).Error; err != nil {
			return fmt.Errorf("failed to delete article: %w", err)
		}
		// 删除文章历史
		for _, history := range a.ArticleHistories {
			indexIDs = append(indexIDs, history.GetIndexID())
		}
		if err := tx.Delete(&model.ArticleHistory{}, "article_id = ?", a.ID).Error; err != nil {
			return fmt.Errorf("failed to delete article histories: %w", err)
		}
		// 删除评论
		if err := tx.Delete(&model.Comment{}, "article_id = ?", a.ID).Error; err != nil {
			return fmt.Errorf("failed to delete article comments: %w", err)
		}
		return nil
	})

	if err != nil {
		return err
	}
	// delete full-text search data
	for _, indexID := range indexIDs {
		solitudes.System.Search.Delete(indexID)
	}
	return nil
}

type publishArticle struct {
	ID             string `form:"id"`
	Title          string `form:"title"`
	Slug           string `form:"slug"`
	Content        string `form:"content"`
	Template       byte   `form:"template"`
	Tags           string `form:"tags"`
	IsBook         bool   `form:"is_book"`
	IsPrivate      bool   `form:"is_private"`
	DisableComment bool   `form:"disable_comment"`
	BookRefer      string `form:"book_refer"`
	NewVersion     uint   `form:"new_version"`
}

func publishHandler(c *fiber.Ctx) error {
	var pa publishArticle
	if err := c.BodyParser(&pa); err != nil {
		return fmt.Errorf("failed to parse publish form: %w", err)
	}
	if err := validator.StructCtx(c.Context(), &pa); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	var bookRefer *string
	if pa.BookRefer != "" {
		bookRefer = &pa.BookRefer
	}
	// edit article
	newArticle := &model.Article{
		ID:             pa.ID,
		Title:          strings.TrimSpace(pa.Title),
		Slug:           strings.TrimSpace(pa.Slug),
		Content:        clearNonUTF8Chars(pa.Content),
		NewVersion:     pa.NewVersion,
		TemplateID:     pa.Template,
		IsBook:         pa.IsBook,
		IsPrivate:      pa.IsPrivate,
		DisableComment: pa.DisableComment,
		RawTags:        pa.Tags,
		BookRefer:      bookRefer,
		Version:        1,
	}

	if newArticle.IsTopic() {
		if len(newArticle.Slug) == 0 {
			newArticle.Slug = time.Now().Format("20060102150405")
		}
		if len(newArticle.Title) == 0 {
			newArticle.Title = newArticle.Slug
		}
	}

	originalArticle, err := fetchOriginArticle(newArticle)
	if err != nil {
		return fmt.Errorf("failed to fetch original article: %w", err)
	}

	err = solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		if pa.NewVersion == 1 && originalArticle.ID != "" {
			history := model.ArticleHistory{
				Content:   originalArticle.Content,
				Version:   originalArticle.Version,
				ArticleID: originalArticle.ID,
				CreatedAt: originalArticle.CreatedAt,
			}
			if err := tx.Create(&history).Error; err != nil {
				return fmt.Errorf("failed to create article history: %w", err)
			}
		}

		if err := tx.Save(&newArticle).Error; err != nil {
			return fmt.Errorf("failed to save article: %w", err)
		}

		return nil
	})

	if err != nil {
		return err
	}
	// indexing serch engine
	numBefore, _ := solitudes.System.Search.DocCount()
	errIndex := solitudes.System.Search.Index(newArticle.GetIndexID(), newArticle)
	numAfter, _ := solitudes.System.Search.DocCount()
	log.Printf("Doc %s indexed %d --> %d %+v\n", newArticle.GetIndexID(), numBefore, numAfter, errIndex)

	return c.Status(http.StatusOK).JSON(newArticle)
}

func fetchOriginArticle(af *model.Article) (model.Article, error) {
	if af.ID == "" {
		return model.Article{}, nil
	}
	var originArticle model.Article
	if err := solitudes.System.DB.Take(&originArticle, "id = ?", af.ID).Error; err != nil {
		return model.Article{}, err
	}

	af.CreatedAt = originArticle.CreatedAt
	af.CommentNum = originArticle.CommentNum
	af.ReadNum = originArticle.ReadNum

	if af.NewVersion == 1 {
		af.UpdatedAt = time.Now()
		af.Version = originArticle.Version + 1
	} else {
		af.UpdatedAt = originArticle.UpdatedAt
		af.Version = originArticle.Version
	}

	return originArticle, nil
}

func clearNonUTF8Chars(s string) string {
	v := make([]rune, 0, len(s))
	for i, r := range s {
		// 清理非 UTF-8 字符
		if r == utf8.RuneError {
			_, size := utf8.DecodeRuneInString(s[i:])
			if size == 1 {
				continue
			}
		}
		// 清理 backspace
		if r == '\b' {
			continue
		}
		v = append(v, r)
	}
	return string(v)
}
