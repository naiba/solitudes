package wengine

import (
	"net/http"

	"github.com/adtac/go-akismet/akismet"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/naiba/solitudes"
)

type commentForm struct {
	ReplyTo  *string `form:"reply_to" binding:"omitempty,uuid4"`
	Nickname string  `form:"nickname" binding:"required"`
	Content  string  `form:"content" binding:"required" gorm:"text"`
	Slug     string  `form:"slug" binding:"required" gorm:"index"`
	Website  string  `form:"website,omitempty" binding:"omitempty,url"`
	Email    string  `form:"email,omitempty" binding:"omitempty,email"`
}

func commentHandler(c *gin.Context) {
	var cf commentForm
	if err := c.ShouldBind(&cf); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	var article solitudes.Article
	if err := solitudes.System.DB.Select("id,version").First(&article, "slug = ?", cf.Slug).Error; err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	var commentType string
	if cf.ReplyTo != nil {
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
	cm.ArticleID = &article.ID
	cm.Website = cf.Website
	cm.Email = cf.Email
	cm.IP = c.ClientIP()
	cm.Version = article.Version
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
