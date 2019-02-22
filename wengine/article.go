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
