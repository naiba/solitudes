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

func comments(c *gin.Context) {
	rawPage := c.Query("page")
	var page int64
	page, _ = strconv.ParseInt(rawPage, 10, 32)
	var cs []solitudes.Comment
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.DB.Preload("Article"),
		Page:    int(page),
		Limit:   15,
		OrderBy: []string{"created_at desc"},
	}, &cs)
	c.HTML(http.StatusOK, "admin/comments", soligin.Soli(c, true, gin.H{
		"title":    "Manage Comments",
		"comments": cs,
		"page":     pg,
	}))
}

func deleteComment(c *gin.Context) {
	id := c.Query("id")
	articleStrID := c.Query("aid")
	intID, err := strconv.ParseInt(id, 10, 32)
	articleID, err2 := strconv.ParseInt(articleStrID, 10, 32)

	if err != nil || err2 != nil || articleID == 0 || intID == 0 {
		c.String(http.StatusForbidden, "Error id")
		return
	}

	tx := solitudes.System.DB.Begin()
	if err = tx.Delete(&solitudes.Comment{}, "id =?", intID).Error; err != nil {
		tx.Rollback()
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	if err = tx.Model(solitudes.Article{}).Where("id = ?", articleID).
		UpdateColumn("comment_num", gorm.Expr("comment_num - ?", 1)).Error; err != nil {
		tx.Rollback()
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	if err = tx.Commit().Error; err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
}
