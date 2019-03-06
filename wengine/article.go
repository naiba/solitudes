package wengine

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/biezhi/gorm-paginator/pagination"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/x/soligin"
)

func article(c *gin.Context) {
	slug := c.MustGet(solitudes.CtxRequestParams).([]string)

	// load article
	var a solitudes.Article
	if err := solitudes.System.DB.First(&a, "slug = ?", slug[1]).Error; err == gorm.ErrRecordNotFound {
		c.HTML(http.StatusNotFound, "default/error", soligin.Soli(c, false, gin.H{
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

	var title string
	// load history
	if len(slug) == 3 && slug[2] != "" {
		version, err := strconv.ParseUint(slug[2], 10, 64)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		if uint(version) == a.Version {
			c.Redirect(http.StatusFound, "/"+a.Slug)
			return
		}
		var history solitudes.ArticleHistory
		if err := solitudes.System.DB.First(&history, "article_id = ? and version = ?", a.ID, slug[2]).Error; err == gorm.ErrRecordNotFound {
			c.HTML(http.StatusNotFound, "default/error", soligin.Soli(c, false, gin.H{
				"title": "404 Page Not Found",
				"msg":   "Wow ... This page may fly to Mars.",
			}))
			return
		} else if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		a.Content = history.Content
		a.Version = history.Version
		title = fmt.Sprintf("%s v%d.%s", a.Title, a.Version, a.CreatedAt.Format("20060102"))
	} else {
		title = a.Title
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
		DB: solitudes.System.DB.Preload("ChildComments", func(db *gorm.DB) *gorm.DB {
			return db.Order("id DESC")
		}).Where("reply_to = 0 and article_id = ?", a.ID),
		Page:    int(page),
		Limit:   5,
		OrderBy: []string{"id desc"},
	}, &a.Comments)

	// load prevPost,nextPost
	relatedSiblingArticle(&a)

	// set slug
	setSlugToComment(&a, a.Comments)

	a.GenTOC()

	c.HTML(http.StatusOK, "default/"+solitudes.TemplateIndex[a.TemplateID], soligin.Soli(c, true, gin.H{
		"title":        title,
		"keywords":     a.RawTags,
		"article":      a,
		"comment_page": pg,
	}))
}

func relatedSiblingArticle(p *solitudes.Article) (prev solitudes.Article, next solitudes.Article) {
	sibiling, _ := solitudes.System.SafeCache.GetOrBuild(fmt.Sprintf("%s%d", solitudes.CacheKeyPrefixRelatedSiblingArticle, p.ID), func() (interface{}, error) {
		var sb solitudes.SibilingArticle
		if p.BookRefer == 0 {
			solitudes.System.DB.Select("id,title,slug").First(&sb.Next, "id > ?", p.ID)
			solitudes.System.DB.Select("id,title,slug").Where("id < ?", p.ID).Order("id DESC", true).First(&sb.Prev)
		} else {
			// if this is a book section
			solitudes.System.DB.Select("id,title,slug").First(&sb.Next, "book_refer = ? and  id > ?", p.BookRefer, p.ID)
			solitudes.System.DB.Select("id,title,slug").Where("book_refer = ? and  id < ?", p.BookRefer, p.ID).Order("id DESC", true).First(&sb.Prev)
		}
		return sb, nil
	})
	if sibiling != nil {
		x := sibiling.(solitudes.SibilingArticle)
		p.SibilingArticle = &x
	}
	return
}

func relatedChapters(p *solitudes.Article) {
	if p.IsBook {
		chapters, _ := solitudes.System.SafeCache.GetOrBuild(fmt.Sprintf("%s%d", solitudes.CacheKeyPrefixRelatedChapters, p.ID), func() (interface{}, error) {
			return innerRelatedChapters(p.ID), nil
		})
		if chapters != nil {
			x := chapters.([]*solitudes.Article)
			p.Chapters = x
		}
	}
}

func innerRelatedChapters(pid uint) (ps []*solitudes.Article) {
	solitudes.System.DB.Order("id ASC", true).Find(&ps, "book_refer=?", pid)
	for i := 0; i < len(ps); i++ {
		if ps[i].IsBook {
			ps[i].Chapters = innerRelatedChapters(ps[i].ID)
		}
	}
	return
}

func relatedBook(p *solitudes.Article) {
	if p.BookRefer != 0 {
		book, err := solitudes.System.SafeCache.GetOrBuild(fmt.Sprintf("%s%d", solitudes.CacheKeyPrefixRelatedArticle, p.BookRefer), func() (interface{}, error) {
			var book solitudes.Article
			var err error
			if err = solitudes.System.DB.First(&book, "id = ?", p.BookRefer).Error; err != nil {
				return nil, err
			}
			return book, err
		})
		if err == nil {
			x := book.(solitudes.Article)
			p.Book = &x
		}
	}
}

func setSlugToComment(a *solitudes.Article, cm []*solitudes.Comment) {
	for i := 0; i < len(cm); i++ {
		cm[i].Article = a
		if len(cm[i].ChildComments) > 0 {
			setSlugToComment(a, cm[i].ChildComments)
		}
	}
}
