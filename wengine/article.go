package wengine

import (
	"net/http"
	"strconv"

	"github.com/biezhi/gorm-paginator/pagination"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/x/soligin"
)

func publish(c *gin.Context) {
	id := c.Query("id")
	var article solitudes.Article
	if id != "" {
		solitudes.System.D.First(&article, "id = ?", id)
	}
	c.HTML(http.StatusOK, "admin/publish", soligin.Soli(c, true, gin.H{
		"title":     "Publish new article",
		"templates": solitudes.Templates,
		"article":   article,
	}))
}

func deleteArticle(c *gin.Context) {
	var err error
	var a solitudes.Article
	if err = solitudes.System.D.Select("id").Preload("ArticleHistories").First(&a, "id = ?", c.Query("id")).Error; err != nil {
		c.String(http.StatusForbidden, err.Error())
		return
	}
	var indexIDs []string
	indexIDs = append(indexIDs, a.GetIndexID())
	tx := solitudes.System.D.Unscoped().Begin()
	if err = tx.Delete(solitudes.Article{}, "id = ?", a.ID).Error; err != nil {
		tx.Rollback()
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	for i := 0; i < len(a.ArticleHistories); i++ {
		indexIDs = append(indexIDs, a.ArticleHistories[i].GetIndexID())
	}
	if err = tx.Delete(solitudes.ArticleHistory{}, "article_id = ?", a.ID).Error; err != nil {
		tx.Rollback()
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	if err = tx.Commit().Error; err != nil {
		tx.Rollback()
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	for i := 0; i < len(indexIDs); i++ {
		solitudes.System.S.Delete(indexIDs[i])
	}
	c.Redirect(http.StatusFound, "/admin/")
}

func publishHandler(c *gin.Context) {
	var err error
	if c.Query("action") == "delete" && c.Query("id") != "" {
		deleteArticle(c)
		return
	}

	// new or edit article
	var newArticle solitudes.Article

	if err = c.ShouldBind(&newArticle); err != nil {
		c.String(http.StatusForbidden, err.Error())
		return
	}

	// edit article
	if newArticle.ID != 0 {
		var originArticle solitudes.Article
		if err := solitudes.System.D.First(&originArticle, "id = ?", newArticle.ID).Error; err != nil {
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
	} else {
		newArticle.DeletedAt = nil
	}

	// update article version
	newArticle.Version = newArticle.Version + 1

	// save edit history && article
	tx := solitudes.System.D.Begin()
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
		err = solitudes.System.S.Index(newArticle.GetIndexID(), newArticle.ToIndexData())
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

func article(c *gin.Context) {
	slug := c.MustGet(solitudes.CtxRequestParams).([]string)

	// load article
	var a solitudes.Article
	if err := solitudes.System.D.First(&a, "slug = ?", slug[1]).Error; err == gorm.ErrRecordNotFound {
		c.HTML(http.StatusNotFound, "default/error", soligin.Soli(c, true, gin.H{
			"title": "404 Page Not Found",
			"msg":   "Wow ... This page may fly to Mars.",
		}))
		return
	} else if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	if len(a.Tags) == 0 {
		a.Tags = nil
	}

	relatedChapters(&a)
	relatedBook(&a)

	// load comments
	pageSlice := c.Query("comment_page")
	var page int64
	if pageSlice != "" {
		page, _ = strconv.ParseInt(pageSlice, 10, 32)
	}
	pg := pagination.Paging(&pagination.Param{
		DB: solitudes.System.D.Preload("ChildComments", func(db *gorm.DB) *gorm.DB {
			return db.Order("id DESC")
		}).Where("reply_to = 0 and article_id = ?", a.ID),
		Page:    int(page),
		Limit:   5,
		OrderBy: []string{"id desc"},
	}, &a.Comments)

	// load prevPost,nextPost
	var prevPost, nextPost solitudes.Article
	if a.BookRefer == 0 {
		solitudes.System.D.Select("id,title,slug").First(&nextPost, "id > ?", a.ID)
		solitudes.System.D.Select("id,title,slug").Where("id < ?", a.ID).Order("id DESC").First(&prevPost)
	} else {
		// if this is a book section
		solitudes.System.D.Select("id,title,slug").First(&nextPost, "book_refer = ? and  id > ?", a.BookRefer, a.ID)
		solitudes.System.D.Select("id,title,slug").Where("book_refer = ? and  id < ?", a.BookRefer, a.ID).Order("id DESC").First(&prevPost)
	}

	a.GenTOC()

	c.HTML(http.StatusOK, "default/"+solitudes.TemplateIndex[a.TemplateID], soligin.Soli(c, true, gin.H{
		"title":        a.Title,
		"keywords":     a.RawTags,
		"article":      a,
		"comment_page": pg,
		"next":         nextPost,
		"prev":         prevPost,
	}))
}

func relatedChapters(p *solitudes.Article) {
	if p.IsBook {
		solitudes.System.D.Find(&p.Chapters, "book_refer=?", p.ID)
		for i := 0; i < len(p.Chapters); i++ {
			if p.Chapters[i].IsBook {
				relatedChapters(p.Chapters[i])
			}
		}
	}
}

func relatedBook(p *solitudes.Article) {
	if p.BookRefer != 0 {
		var book solitudes.Article
		if err := solitudes.System.D.First(&book, "id = ?", p.BookRefer).Error; err != nil {
			return
		}
		p.Book = &book
	}
}
