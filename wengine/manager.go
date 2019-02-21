package wengine

import (
	"net/http"

	"github.com/naiba/solitudes/x/soligin"

	"github.com/gin-gonic/gin"
)

func manager(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/index", soligin.Soli(gin.H{}))
}
