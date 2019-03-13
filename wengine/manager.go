package wengine

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/x/soligin"
)

func manager(c *gin.Context) {
	var articleNum, commentNum int
	var lastArticle solitudes.Article
	var lastComment solitudes.Comment
	type tagNum struct {
		Count int
	}
	var tn tagNum

	var wg sync.WaitGroup
	wg.Add(5)
	solitudes.System.Pool.Submit(func() {
		solitudes.System.DB.Model(solitudes.Article{}).Count(&articleNum)
		wg.Done()
	})
	solitudes.System.Pool.Submit(func() {
		solitudes.System.DB.Model(solitudes.Comment{}).Count(&commentNum)
		wg.Done()
	})
	solitudes.System.Pool.Submit(func() {
		solitudes.System.DB.Raw(`select count(*) from (select tags,count(tags) from (select unnest(tags) as tags from articles) t group by tags) ts;`).Scan(&tn)
		wg.Done()
	})
	solitudes.System.Pool.Submit(func() {
		solitudes.System.DB.Select("updated_at").Order("updated_at DESC").First(&lastArticle)
		wg.Done()
	})
	solitudes.System.Pool.Submit(func() {
		solitudes.System.DB.Select("created_at").Order("created_at DESC").First(&lastComment)
		wg.Done()
	})
	wg.Wait()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	c.HTML(http.StatusOK, "admin/index", soligin.Soli(c, true, gin.H{
		"title":              "Dashboard",
		"articleNum":         articleNum,
		"commentNum":         commentNum,
		"lastArticlePublish": fmt.Sprintf("%.2f", time.Now().Sub(lastArticle.CreatedAt).Hours()/24),
		"lastComment":        fmt.Sprintf("%.2f", time.Now().Sub(lastComment.CreatedAt).Hours()/24),
		"tagNum":             tn.Count,

		"memoryUsage": bToMb(m.Sys),
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
	if err := c.SaveUploadedFile(f, "data"+extName); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.String(http.StatusOK, extName)
}

func rebuildBleveData(c *gin.Context) {
	solitudes.BuildArticleIndex()
}
