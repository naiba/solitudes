package wengine

import (
	"net/http"
	"runtime"
	"time"

	"github.com/naiba/solitudes"

	"github.com/naiba/solitudes/x/soligin"

	"github.com/gin-gonic/gin"
)

func manager(c *gin.Context) {
	var articleNum, commentNum, labelNum int
	var lastArticle solitudes.Article
	var lastComment solitudes.Comment
	solitudes.System.D.Model(solitudes.Article{}).Count(&articleNum)
	solitudes.System.D.Model(solitudes.Comment{}).Count(&commentNum)
	solitudes.System.D.Model(solitudes.Label{}).Count(&labelNum)
	solitudes.System.D.Select("created_at").Order("id DESC").First(&lastArticle)
	solitudes.System.D.Select("created_at").Order("id DESC").First(&lastComment)

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	c.HTML(http.StatusOK, "admin/index", soligin.Soli(gin.H{
		"articleNum":         articleNum,
		"commentNum":         commentNum,
		"lastArticlePublish": time.Now().Sub(lastArticle.CreatedAt).Hours() / 24,
		"lastComment":        time.Now().Sub(lastComment.CreatedAt).Hours() / 24,
		"labelNum":           labelNum,

		"memoryUsage": bToMb(m.Alloc),
		"gcNum":       m.NumGC,
		"routineNum":  runtime.NumGoroutine(),
	}))
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}
