package wengine

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/x/soligin"
)

func publish(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/publish", soligin.Soli(gin.H{
		"templates": solitudes.Templates,
	}))
}

func publishHandler(c *gin.Context) {
	var article solitudes.Article
	if err := c.ShouldBind(&article); err != nil {
		c.String(http.StatusForbidden, err.Error())
		return
	}
	if err := solitudes.System.D.Save(&article).Error; err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
}
