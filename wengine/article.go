package wengine

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/adtac/go-akismet/akismet"
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
		solitudes.System.D.Where("id = ?", id).First(&article)
	}
	c.HTML(http.StatusOK, "admin/publish", soligin.Soli(c, true, gin.H{
		"title":     "Publish new article",
		"templates": solitudes.Templates,
		"article":   article,
	}))
}

var titleRegex = regexp.MustCompile(`^\s{0,2}(#{1,6})\s(.*)$`)
var whitespaces = regexp.MustCompile(`[\s|\.]{1,}`)

func genTOC(post *solitudes.Article) {
	lines := strings.Split(post.Content, "\n")
	var matches []string
	var currentToc *solitudes.ArticleTOC
	post.Toc = make([]*solitudes.ArticleTOC, 0)
	for j := 0; j < len(lines); j++ {
		matches = titleRegex.FindStringSubmatch(lines[j])
		if len(matches) == 3 {
			var toc solitudes.ArticleTOC
			toc.Level = len(matches[1])
			toc.Title = string(matches[2])
			toc.Slug = string(whitespaces.ReplaceAllString(matches[2], "-"))
			toc.SubTitles = make([]*solitudes.ArticleTOC, 0)
			if currentToc == nil {
				post.Toc = append(post.Toc, &toc)
				currentToc = &toc
			} else {
				parent := currentToc
				if currentToc.Level > toc.Level {
					// 父节点
					for i := -1; i < currentToc.Level-toc.Level; i++ {
						parent = parent.Parent
						if parent == nil || parent.Level < toc.Level {
							break
						}
					}
					if parent == nil {
						post.Toc = append(post.Toc, &toc)
					} else {
						toc.Parent = parent
						parent.SubTitles = append(parent.SubTitles, &toc)
					}
				} else if currentToc.Level == toc.Level {
					// 兄弟节点
					if parent.Parent == nil {
						post.Toc = append(post.Toc, &toc)
					} else {
						toc.Parent = parent.Parent
						parent.Parent.SubTitles = append(parent.Parent.SubTitles, &toc)
					}
				} else {
					// 子节点
					toc.Parent = parent
					parent.SubTitles = append(parent.SubTitles, &toc)
				}
				currentToc = &toc
			}
		}
	}
}

func publishHandler(c *gin.Context) {
	var err error
	if c.Query("action") == "delete" && c.Query("id") != "" {
		// delete article
		if err = solitudes.System.D.Unscoped().Delete(solitudes.Article{}, "id = ?", c.Query("id")).Error; err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		c.Redirect(http.StatusFound, "/admin/")
	} else {
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

		genTOC(&newArticle)
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

type commentForm struct {
	ReplyTo  uint   `form:"reply_to" json:"reply_to,omitempty"`
	Nickname string `form:"nickname" binding:"required" json:"name,omitempty"`
	Content  string `form:"content" binding:"required" gorm:"text" json:"content,omitempty"`
	Slug     string `form:"slug" binding:"required" gorm:"index" json:"article_id,omitempty"`
	Website  string `form:"website,omitempty" binding:"omitempty,url" json:"website,omitempty"`
	Email    string `form:"email,omitempty" binding:"omitempty,email" json:"email,omitempty"`
}

func commentHandler(c *gin.Context) {
	var cf commentForm
	if err := c.ShouldBind(&cf); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	var article solitudes.Article
	if err := solitudes.System.D.Select("id").First(&article, "slug = ?", cf.Slug).Error; err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	var commentType string
	if cf.ReplyTo != 0 {
		commentType = "reply"
		var count int
		solitudes.System.D.Model(solitudes.Comment{}).Where("id = ?", cf.ReplyTo).Count(&count)
		if count != 1 {
			c.String(http.StatusBadRequest, "reply to invaild comment")
			return
		}
	} else {
		commentType = "comment"
	}

	// akismet anti spam
	if solitudes.System.C.Web.Akismet != "" {
		isSpam, err := akismet.Check(&akismet.Comment{
			Blog:               "https://" + solitudes.System.C.Web.Domain, // required
			UserIP:             c.ClientIP(),                               // required
			UserAgent:          c.Request.Header.Get("User-Agent"),         // required
			CommentType:        commentType,
			Referrer:           c.Request.Header.Get("Referer"),
			Permalink:          "https://" + solitudes.System.C.Web.Domain + "/" + cf.Slug,
			CommentAuthor:      cf.Nickname,
			CommentAuthorEmail: cf.Email,
			CommentAuthorURL:   cf.Website,
			CommentContent:     cf.Content,
		}, solitudes.System.C.Web.Akismet)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		if isSpam {
			c.String(http.StatusForbidden, "Spam")
			return
		}
	}

	var cm solitudes.Comment
	cm.ReplyTo = cf.ReplyTo
	cm.Nickname = cf.Nickname
	cm.Content = cf.Content
	cm.ArticleID = article.ID
	cm.Website = cf.Website
	cm.Email = cf.Email
	cm.IP = c.ClientIP()
	cm.UserAgent = c.Request.Header.Get("User-Agent")
	cm.IsAdmin = c.GetBool(solitudes.CtxAuthorized)
	tx := solitudes.System.D.Begin()
	if err := tx.Save(&cm).Error; err != nil {
		tx.Rollback()
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Model(solitudes.Article{}).
		Where("id = ?", cm.ArticleID).
		UpdateColumn("comment_num", gorm.Expr("comment_num + ?", 1)).Error; err != nil {
		tx.Rollback()
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
}
