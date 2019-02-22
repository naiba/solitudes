package wengine

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/naiba/solitudes"

	"github.com/naiba/solitudes/x/soligin"

	"github.com/gin-gonic/gin"
)

func manager(c *gin.Context) {
	var articleNum, commentNum, tagNum int
	var lastArticle solitudes.Article
	var lastComment solitudes.Comment
	solitudes.System.D.Model(solitudes.Article{}).Count(&articleNum)
	solitudes.System.D.Model(solitudes.Comment{}).Count(&commentNum)
	solitudes.System.D.Exec(`select count(*) from (select tags
		from (
		  select unnest(tags) as tags
		  from articles 
		) t
		group by tags) ts;`).Scan(&tagNum)
	solitudes.System.D.Select("created_at").Order("id DESC").First(&lastArticle)
	solitudes.System.D.Select("created_at").Order("id DESC").First(&lastComment)

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	c.HTML(http.StatusOK, "admin/index", soligin.Soli(gin.H{
		"articleNum":         articleNum,
		"commentNum":         commentNum,
		"lastArticlePublish": time.Now().Sub(lastArticle.CreatedAt).Hours() / 24,
		"lastComment":        time.Now().Sub(lastComment.CreatedAt).Hours() / 24,
		"tagNum":             tagNum,

		"memoryUsage": bToMb(m.Alloc),
		"gcNum":       m.NumGC,
		"routineNum":  runtime.NumGoroutine(),
	}))
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}

var validExtNames = map[string]interface{}{
	"jpg":  nil,
	"jpeg": nil,
	"png":  nil,
	"gif":  nil,
}

func upload(c *gin.Context) {
	f, err := c.FormFile("file")
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	fs := strings.Split(f.Filename, ".")
	if len(fs) < 2 {
		c.String(http.StatusInternalServerError, "Invalid file")
		return
	}
	extName := fs[len(fs)-1]
	if _, ok := validExtNames[extName]; !ok {
		c.String(http.StatusInternalServerError, "Invalid file type")
		return
	}
	extName = fmt.Sprintf("/upload/%d.%s", time.Now().UnixNano(), extName)
	if err := c.SaveUploadedFile(f, "resource/data"+extName); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.String(http.StatusOK, extName)
}
