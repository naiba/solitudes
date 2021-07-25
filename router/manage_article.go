package router

import (
	"net/http"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"

	"github.com/biezhi/gorm-paginator/pagination"
)

func manageArticle(c *fiber.Ctx) error {
	rawPage := c.Query("page")
	var page int64
	page, _ = strconv.ParseInt(rawPage, 10, 32)
	var as []model.Article
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.DB,
		Page:    int(page),
		Limit:   15,
		OrderBy: []string{"created_at DESC"},
	}, &as)
	for i := 0; i < len(as); i++ {
		as[i].RelatedCount(solitudes.System.DB, solitudes.System.Pool, checkPoolSubmit)
	}
	c.Status(http.StatusOK).Render("admin/articles", injectSiteData(c, fiber.Map{
		"title":    c.Locals(solitudes.CtxTranslator).(*translator.Translator).T("manage_articles"),
		"articles": as,
		"page":     pg,
	}))
	return nil
}

func publish(c *fiber.Ctx) error {
	id := c.Query("id")
	var article model.Article
	if id != "" {
		solitudes.System.DB.Take(&article, "id = ?", id)
	}
	c.Status(http.StatusOK).Render("admin/publish", injectSiteData(c, fiber.Map{
		"title":     c.Locals(solitudes.CtxTranslator).(*translator.Translator).T("publish_article"),
		"templates": solitudes.Templates,
		"article":   article,
	}))
	return nil
}

func deleteArticle(c *fiber.Ctx) error {
	id := c.Query("id")
	if len(id) < 10 {
		c.Status(http.StatusBadRequest).WriteString("Error article id")
		return nil
	}
	var a model.Article
	if err := solitudes.System.DB.Select("id").Preload("ArticleHistories").Take(&a, "id = ?", id).Error; err != nil {
		c.Status(http.StatusBadRequest).WriteString(err.Error())
		return err
	}
	var indexIDs []string
	indexIDs = append(indexIDs, a.GetIndexID())
	tx := solitudes.System.DB.Begin()
	if err := tx.Delete(model.Article{}, "id = ?", a.ID).Error; err != nil {
		tx.Rollback()
		c.Status(http.StatusInternalServerError).WriteString(err.Error())
		return err
	}
	// delete article history
	for i := 0; i < len(a.ArticleHistories); i++ {
		indexIDs = append(indexIDs, a.ArticleHistories[i].GetIndexID())
	}
	if err := tx.Delete(model.ArticleHistory{}, "article_id = ?", a.ID).Error; err != nil {
		tx.Rollback()
		c.Status(http.StatusInternalServerError).WriteString(err.Error())
		return err
	}
	// delete comments
	if err := tx.Delete(model.Comment{}, "article_id = ?", a.ID).Error; err != nil {
		tx.Rollback()
		c.Status(http.StatusInternalServerError).WriteString(err.Error())
		return err
	}
	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		c.Status(http.StatusInternalServerError).WriteString(err.Error())
		return err
	}
	// delete full-text search data
	for i := 0; i < len(indexIDs); i++ {
		solitudes.System.Search.Delete(indexIDs[i])
	}
	return nil
}

type publishArticle struct {
	ID         string `form:"id"`
	Title      string `form:"title"`
	Slug       string `form:"slug"`
	Content    string `form:"content"`
	Template   byte   `form:"template"`
	Tags       string `form:"tags"`
	IsBook     bool   `form:"is_book"`
	BookRefer  string `form:"book_refer"`
	NewVersion bool   `form:"new_version"`
}

func publishHandler(c *fiber.Ctx) error {
	var pa publishArticle
	if err := c.BodyParser(&pa); err != nil {
		c.Status(http.StatusBadRequest).WriteString(err.Error())
		return err
	}
	if err := validator.StructCtx(c.Context(), &pa); err != nil {
		c.Status(http.StatusBadRequest).WriteString(err.Error())
		return err
	}

	var err error
	var bookRefer *string
	if pa.BookRefer != "" {
		bookRefer = &pa.BookRefer
	}
	// edit article
	article := &model.Article{
		ID:         pa.ID,
		Title:      pa.Title,
		Slug:       pa.Slug,
		Content:    pa.Content,
		NewVersion: pa.NewVersion,
		TemplateID: pa.Template,
		IsBook:     pa.IsBook,
		RawTags:    pa.Tags,
		BookRefer:  bookRefer,
	}
	if article, err = fetchOriginArticle(article); err != nil {
		c.Status(http.StatusBadRequest).WriteString(err.Error())
		return err
	}

	// save edit history && article
	tx := solitudes.System.DB.Begin()
	err = tx.Save(&article).Error
	if article.NewVersion && err == nil {
		var history model.ArticleHistory
		history.Content = article.Content
		history.Version = article.Version
		history.ArticleID = article.ID
		err = tx.Save(&history).Error
	}
	if err == nil {
		// indexing serch engine
		solitudes.System.Search.Index(article.GetIndexID(), article)
	}
	if err != nil {
		tx.Rollback()
		c.Status(http.StatusInternalServerError).WriteString(err.Error())
		return err
	}
	if err = tx.Commit().Error; err != nil {
		c.Status(http.StatusInternalServerError).WriteString(err.Error())
		return err
	}
	return nil
}

func fetchOriginArticle(af *model.Article) (*model.Article, error) {
	if af.ID == "" {
		return af, nil
	}
	var originArticle model.Article
	if err := solitudes.System.DB.Take(&originArticle, "id = ?", af.ID).Error; err != nil {
		return nil, err
	}
	if af.NewVersion {
		originArticle.Version++
	}
	originArticle.Title = af.Title
	originArticle.Slug = af.Slug
	originArticle.Content = af.Content
	originArticle.TemplateID = af.TemplateID
	originArticle.RawTags = af.RawTags
	originArticle.BookRefer = af.BookRefer
	originArticle.IsBook = af.IsBook
	return &originArticle, nil
}
