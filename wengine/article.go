package wengine

import (
	"net/http"
	"strconv"

	"github.com/biezhi/gorm-paginator/pagination"
	"github.com/blevesearch/bleve"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/x/soligin"
)

func publish(c *gin.Context) {
	id := c.Query("id")
	var article solitudes.Article
	if id != "" {
		solitudes.System.D.Where("id = ?", id).First(&article)
	}
	c.HTML(http.StatusOK, "admin/publish", soligin.Soli(c, true, gin.H{
		"title":     "Publish new article",
		"templates": solitudes.Templates,
		"article":   article,
	}))
}

func publishHandler(c *gin.Context) {
	var err error
	if c.Query("action") == "delete" && c.Query("id") != "" {
		// delete article
		solitudes.System.S.Delete(c.Query("id"))
		if err = solitudes.System.D.Unscoped().Delete(solitudes.Article{}, "id = ?", c.Query("id")).Error; err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		c.Redirect(http.StatusFound, "/admin/")
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
		err = solitudes.System.S.Index(newArticle.SID(), newArticle.ToIndexData())
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
	if err := solitudes.System.D.Where("slug = ?", slug[1]).First(&a).Error; err == gorm.ErrRecordNotFound {
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
	if a.IsBook {
		solitudes.System.D.Find(&a.Chapters, "book_refer=?", a.ID)
	}
	var book solitudes.Article
	if a.BookRefer != 0 {
		solitudes.System.D.First(&book, "id = ?", a.BookRefer)
	}

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
		solitudes.System.D.Select("id,title,slug").Where("id > ?", a.ID).First(&nextPost)
		solitudes.System.D.Select("id,title,slug").Where("id < ?", a.ID).Order("id DESC").First(&prevPost)
	} else {
		// if this is a book section
		solitudes.System.D.Select("id,title,slug").Where("book_refer = ? and  id > ?", a.BookRefer, a.ID).First(&nextPost)
		solitudes.System.D.Select("id,title,slug").Where("book_refer = ? and  id < ?", a.BookRefer, a.ID).Order("id DESC").First(&prevPost)
	}

	a.GenTOC()

	c.HTML(http.StatusOK, "default/"+solitudes.TemplateIndex[a.TemplateID], soligin.Soli(c, true, gin.H{
		"title":        a.Title,
		"keywords":     a.RawTags,
		"article":      a,
		"book":         book,
		"comment_page": pg,
		"next":         nextPost,
		"prev":         prevPost,
	}))
}

func search(c *gin.Context) {
	keywords := c.Query("w")
	req := bleve.NewSearchRequest(bleve.NewQueryStringQuery(keywords))
	req.Highlight = bleve.NewHighlight()
	res, err := solitudes.System.S.Search(req)
	if err != nil {
		panic(err)
	}
	c.JSON(http.StatusOK, res)
}

func archive(c *gin.Context) {
	pageSlice := c.MustGet(solitudes.CtxRequestParams).([]string)
	var page int64
	if len(pageSlice) == 2 {
		page, _ = strconv.ParseInt(pageSlice[1], 10, 32)
	}
	var articles []solitudes.Article
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.D,
		Page:    int(page),
		Limit:   15,
		OrderBy: []string{"id desc"},
	}, &articles)
	c.HTML(http.StatusOK, "default/archive", soligin.Soli(c, false, gin.H{
		"title":    "Archive",
		"what":     "archives",
		"articles": listArticleByYear(articles),
		"page":     pg,
	}))
}

func tags(c *gin.Context) {
	pageSlice := c.MustGet(solitudes.CtxRequestParams).([]string)
	var page int64
	if len(pageSlice) == 3 {
		page, _ = strconv.ParseInt(pageSlice[2], 10, 32)
	}
	var articles []solitudes.Article
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.D.Where("tags @> ARRAY[?]::varchar[]", pageSlice[1]),
		Page:    int(page),
		Limit:   15,
		OrderBy: []string{"id desc"},
	}, &articles)
	c.HTML(http.StatusOK, "default/archive", soligin.Soli(c, false, gin.H{
		"title":    "Articles in \"" + pageSlice[1] + "\"",
		"what":     "tags",
		"articles": listArticleByYear(articles),
		"page":     pg,
	}))
}

func listArticleByYear(as []solitudes.Article) [][]solitudes.Article {
	var listed = make([][]solitudes.Article, 0)
	var lastYear int
	var listItem []solitudes.Article
	for i := 0; i < len(as); i++ {
		currentYear := as[i].UpdatedAt.Year()
		if currentYear != lastYear {
			if len(listItem) > 0 {
				listed = append(listed, listItem)
			}
			listItem = make([]solitudes.Article, 0)
			lastYear = currentYear
		}
		listItem = append(listItem, as[i])
	}
	if len(listItem) > 0 {
		listed = append(listed, listItem)
	}
	return listed
}
