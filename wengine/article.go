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
	if err := solitudes.System.DB.Take(&a, "slug = ?", slug[1]).Error; err == gorm.ErrRecordNotFound {
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
		if err := solitudes.System.DB.Take(&history, "article_id = ? and version = ?", a.ID, slug[2]).Error; err == gorm.ErrRecordNotFound {
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

	// load root comments
	pageSlice := c.Query("comment_page")
	var page int64
	if pageSlice != "" {
		page, _ = strconv.ParseInt(pageSlice, 10, 32)
	}
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.DB.Where("reply_to is null and article_id = ?", a.ID),
		Page:    int(page),
		Limit:   5,
		OrderBy: []string{"created_at desc"},
	}, &a.Comments)

	// load prevPost,nextPost
	relatedSiblingArticle(&a)

	// load childComments
	relatedChildComments(&a, a.Comments, true)

	a.GenTOC()

	c.HTML(http.StatusOK, "default/"+solitudes.TemplateIndex[a.TemplateID], soligin.Soli(c, true, gin.H{
		"title":        title,
		"keywords":     a.RawTags,
		"article":      a,
		"comment_page": pg,
	}))
}

func relatedSiblingArticle(p *solitudes.Article) (prev solitudes.Article, next solitudes.Article) {
	sibiling, _ := solitudes.System.SafeCache.GetOrBuild(solitudes.CacheKeyPrefixRelatedSiblingArticle+p.ID, func() (interface{}, error) {
		var sb solitudes.SibilingArticle
		if p.BookRefer == nil {
			solitudes.System.DB.Select("id,title,slug").Order("created_at ASC").Take(&sb.Next, "book_refer is null and created_at > ?", p.CreatedAt)
			solitudes.System.DB.Select("id,title,slug").Order("created_at DESC").Where("book_refer is null and created_at < ?", p.CreatedAt).Take(&sb.Prev)
		} else {
			// if this is a book chapter
			solitudes.System.DB.Select("id,title,slug").Order("created_at ASC").Take(&sb.Next, "book_refer = ? and  created_at > ?", p.BookRefer, p.CreatedAt)
			solitudes.System.DB.Select("id,title,slug").Order("created_at DESC").Where("book_refer = ? and  created_at < ?", p.BookRefer, p.CreatedAt).Take(&sb.Prev)
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
		chapters, _ := solitudes.System.SafeCache.GetOrBuild(solitudes.CacheKeyPrefixRelatedChapters+p.ID, func() (interface{}, error) {
			return innerRelatedChapters(p.ID), nil
		})
		if chapters != nil {
			x := chapters.([]*solitudes.Article)
			p.Chapters = x
		}
	}
}

func innerRelatedChapters(pid string) (ps []*solitudes.Article) {
	solitudes.System.DB.Order("created_at ASC").Find(&ps, "book_refer=?", pid)
	for i := 0; i < len(ps); i++ {
		if ps[i].IsBook {
			ps[i].Chapters = innerRelatedChapters(ps[i].ID)
		}
	}
	return
}

func relatedBook(p *solitudes.Article) {
	if p.BookRefer != nil {
		book, err := solitudes.System.SafeCache.GetOrBuild(solitudes.CacheKeyPrefixRelatedArticle+*p.BookRefer, func() (interface{}, error) {
			var book solitudes.Article
			var err error
			if err = solitudes.System.DB.Take(&book, "id = ?", p.BookRefer).Error; err != nil {
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

func relatedChildComments(a *solitudes.Article, cm []*solitudes.Comment, root bool) {
	if root {
		var idMaptoComment = make(map[string]*solitudes.Comment)
		var idArray []string
		// map to index
		for i := 0; i < len(cm); i++ {
			idMaptoComment[cm[i].ID] = cm[i]
			idArray = append(idArray, cm[i].ID)
		}
		var cms []*solitudes.Comment
		solitudes.System.DB.Raw(`WITH RECURSIVE cs AS (SELECT comments.* FROM comments WHERE comments.reply_to in (?) union ALL
		SELECT comments.* FROM comments, cs WHERE comments.reply_to = cs.id)
		SELECT * FROM cs ORDER BY created_at;`, idArray).Scan(&cms)
		// map to index
		for i := 0; i < len(cms); i++ {
			if cms[i].ReplyTo != nil {
				idMaptoComment[cms[i].ID] = cms[i]
			}
		}
		// set child comments
		for i := 0; i < len(cms); i++ {
			if _, has := idMaptoComment[*cms[i].ReplyTo]; has {
				idMaptoComment[*cms[i].ReplyTo].ChildComments =
					append(idMaptoComment[*cms[i].ReplyTo].ChildComments, cms[i])
			}
		}
	}
	for i := 0; i < len(cm); i++ {
		cm[i].Article = a
		if len(cm[i].ChildComments) > 0 {
			relatedChildComments(a, cm[i].ChildComments, false)
			continue
		}
	}
}
