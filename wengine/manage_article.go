package wengine

import (
	"net/http"
	"strconv"

	"github.com/biezhi/gorm-paginator/pagination"
	"github.com/gin-gonic/gin"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/x/soligin"
)

func manageArticle(c *gin.Context) {
	rawPage := c.Query("page")
	var page int64
	page, _ = strconv.ParseInt(rawPage, 10, 32)
	var as []solitudes.Article
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.DB,
		Page:    int(page),
		Limit:   15,
		OrderBy: []string{"id desc"},
	}, &as)
	c.HTML(http.StatusOK, "admin/articles", soligin.Soli(c, true, gin.H{
		"title":    "Manage Articles",
		"articles": as,
		"page":     pg,
	}))
}

func publish(c *gin.Context) {
	id := c.Query("id")
	var article solitudes.Article
	if id != "" {
		solitudes.System.DB.First(&article, "id = ?", id)
	}
	c.HTML(http.StatusOK, "admin/publish", soligin.Soli(c, true, gin.H{
		"title":     "Publish new article",
		"templates": solitudes.Templates,
		"article":   article,
	}))
}

func deleteArticle(c *gin.Context) {
	id := c.Query("id")
	intID, err := strconv.ParseInt(id, 10, 32)
	if err != nil || intID == 0 {
		c.String(http.StatusForbidden, "Error article id")
		return
	}
	var a solitudes.Article
	if err = solitudes.System.DB.Select("id").Preload("ArticleHistories").First(&a, "id = ?", id).Error; err != nil {
		c.String(http.StatusForbidden, err.Error())
		return
	}
	var indexIDs []string
	indexIDs = append(indexIDs, a.GetIndexID())
	tx := solitudes.System.DB.Begin()
	if err = tx.Delete(solitudes.Article{}, "id = ?", a.ID).Error; err != nil {
		tx.Rollback()
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	// delete article history
	for i := 0; i < len(a.ArticleHistories); i++ {
		indexIDs = append(indexIDs, a.ArticleHistories[i].GetIndexID())
	}
	if err = tx.Delete(solitudes.ArticleHistory{}, "article_id = ?", a.ID).Error; err != nil {
		tx.Rollback()
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	// delete comments
	if err = tx.Delete(solitudes.Comment{}, "article_id = ?", a.ID).Error; err != nil {
		tx.Rollback()
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	if err = tx.Commit().Error; err != nil {
		tx.Rollback()
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	// delete bleve data
	for i := 0; i < len(indexIDs); i++ {
		solitudes.System.Search.Delete(indexIDs[i])
	}
}

func publishHandler(c *gin.Context) {
	var err error
	// new or edit article
	var newArticle solitudes.Article

	if err = c.ShouldBind(&newArticle); err != nil {
		c.String(http.StatusForbidden, err.Error())
		return
	}

	// edit article
	if newArticle.ID != 0 {
		var originArticle solitudes.Article
		if err := solitudes.System.DB.First(&originArticle, "id = ?", newArticle.ID).Error; err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		originArticle.Title = newArticle.Title
		originArticle.Slug = newArticle.Slug
		originArticle.Content = newArticle.Content
		originArticle.TemplateID = newArticle.TemplateID
		originArticle.RawTags = newArticle.RawTags
		originArticle.BookRefer = newArticle.BookRefer
		originArticle.IsBook = newArticle.IsBook
		newArticle = originArticle
	}
	// update article version
	newArticle.Version = newArticle.Version + 1

	// save edit history && article
	tx := solitudes.System.DB.Begin()
	err = tx.Save(&newArticle).Error
	if err == nil {
		var history solitudes.ArticleHistory
		history.Content = newArticle.Content
		history.Version = newArticle.Version
		history.ArticleID = newArticle.ID
		err = tx.Save(&history).Error
	}
	if err == nil {
		// indexing serch engine
		err = solitudes.System.Search.Index(newArticle.GetIndexID(), newArticle.ToIndexData())
	}
	if err != nil {
		tx.Rollback()
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	if err = tx.Commit().Error; err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
}
