package wengine

import (
	"net/http"
	"strconv"

	"github.com/adtac/go-akismet/akismet"
	"github.com/biezhi/gorm-paginator/pagination"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/x/soligin"
)

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
	if err := solitudes.System.DB.Select("id").First(&article, "slug = ?", cf.Slug).Error; err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	var commentType string
	if cf.ReplyTo != 0 {
		commentType = "reply"
		var count int
		solitudes.System.DB.Model(solitudes.Comment{}).Where("id = ?", cf.ReplyTo).Count(&count)
		if count != 1 {
			c.String(http.StatusBadRequest, "reply to invaild comment")
			return
		}
	} else {
		commentType = "comment"
	}

	// akismet anti spam
	if solitudes.System.Config.Web.Akismet != "" {
		isSpam, err := akismet.Check(&akismet.Comment{
			Blog:               "https://" + solitudes.System.Config.Web.Domain, // required
			UserIP:             c.ClientIP(),                                    // required
			UserAgent:          c.Request.Header.Get("User-Agent"),              // required
			CommentType:        commentType,
			Referrer:           c.Request.Header.Get("Referer"),
			Permalink:          "https://" + solitudes.System.Config.Web.Domain + "/" + cf.Slug,
			CommentAuthor:      cf.Nickname,
			CommentAuthorEmail: cf.Email,
			CommentAuthorURL:   cf.Website,
			CommentContent:     cf.Content,
		}, solitudes.System.Config.Web.Akismet)
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
	tx := solitudes.System.DB.Begin()
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

func comments(c *gin.Context) {
	rawPage := c.Query("page")
	var page int64
	page, _ = strconv.ParseInt(rawPage, 10, 32)
	var cs []solitudes.Comment
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.DB.Preload("Article"),
		Page:    int(page),
		Limit:   15,
		OrderBy: []string{"id desc"},
	}, &cs)
	c.HTML(http.StatusOK, "admin/comments", soligin.Soli(c, true, gin.H{
		"title":    "Manage Comments",
		"comments": cs,
		"page":     pg,
	}))
}

func deleteComment(c *gin.Context) {
	id := c.Query("id")
	intID, err := strconv.ParseInt(id, 10, 32)
	if err != nil || intID == 0 {
		c.String(http.StatusForbidden, "Error comment id")
		return
	}
	if err := solitudes.System.DB.Delete(&solitudes.Comment{}, "id =?", intID).Error; err != nil {
		c.String(http.StatusInternalServerError, err.Error())
	}
}
