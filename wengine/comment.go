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
	Website  string  `form:"website" binding:"omitempty,url"`
	Version  uint    `form:"version" binding:"required"`
	Email    string  `form:"email" binding:"omitempty,email"`
}

func commentHandler(c *gin.Context) {
	isAdmin := c.GetBool(solitudes.CtxAuthorized)
	var cf commentForm
	if err := c.ShouldBind(&cf); err != nil {
		if !isAdmin {
			c.String(http.StatusBadRequest, err.Error())
			return
		}
	}
	var article solitudes.Article
	if err := solitudes.System.DB.Select("id,version").First(&article, "slug = ?", cf.Slug).Error; err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	if cf.Version > article.Version || cf.Version == 0 {
		c.String(http.StatusBadRequest, "Error invalid version")
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
	if solitudes.System.Config.Web.Akismet != "" && !isAdmin {
		isSpam, err := akismet.Check(&akismet.Comment{
			Blog:               "https://" + solitudes.System.Config.Web.Domain, // required
			UserIP:             c.ClientIP(),                                    // required
			UserAgent:          c.GetHeader("User-Agent"),                       // required
			CommentType:        commentType,
			Referrer:           c.GetHeader("Referer"),
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
			c.String(http.StatusForbidden, "Rejected by Akismet Anti-Spam System")
			return
		}
	}

	var cm solitudes.Comment
	cm.ReplyTo = cf.ReplyTo
	cm.Content = cf.Content
	cm.ArticleID = &article.ID
	if isAdmin {
		cm.Nickname = solitudes.System.Config.Web.User.Nickname
		cm.Email = solitudes.System.Config.Web.User.Email
	} else {
		cm.Nickname = cf.Nickname
		cm.Email = cf.Email
		cm.Website = cf.Website
		cm.IP = c.ClientIP()
		cm.UserAgent = c.GetHeader("UserAgent")
	}
	cm.IsAdmin = isAdmin
	cm.Version = cf.Version
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
