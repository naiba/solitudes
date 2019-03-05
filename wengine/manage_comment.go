package wengine

import (
	"net/http"
	"strconv"

	"github.com/biezhi/gorm-paginator/pagination"
	"github.com/gin-gonic/gin"
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
