package router

import (
	"net/http"
	"strconv"

	"github.com/biezhi/gorm-paginator/pagination"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/soligin"
)

func comments(c *gin.Context) {
	rawPage := c.Query("page")
	var page int64
	page, _ = strconv.ParseInt(rawPage, 10, 32)
	var cs []model.Comment
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.DB.Preload("Article"),
		Page:    int(page),
		Limit:   15,
		OrderBy: []string{"created_at DESC"},
	}, &cs)
	c.HTML(http.StatusOK, "admin/comments", soligin.Soli(c, gin.H{
		"title":    c.MustGet(solitudes.CtxTranslator).(*solitudes.Translator).T("manage_comments"),
		"comments": cs,
		"page":     pg,
	}))
}

func deleteComment(c *gin.Context) {
	id := c.Query("id")
	rpl := c.Query("rpl")
	articleID := c.Query("aid")

	if len(id) < 10 || len(articleID) < 10 {
		c.String(http.StatusForbidden, "Error id")
		return
	}

	tx := solitudes.System.DB.Begin()
	if err := tx.Delete(&model.Comment{}, "id =?", id).Error; err != nil {
		tx.Rollback()
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	if rpl == "" {
		if err := tx.Model(model.Article{}).Where("id = ?", articleID).
			UpdateColumn("comment_num", gorm.Expr("comment_num - ?", 1)).Error; err != nil {
			tx.Rollback()
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := tx.Commit().Error; err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
}
