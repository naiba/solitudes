package wengine

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"

	"github.com/jinzhu/gorm"

	"github.com/naiba/solitudes"

	"github.com/gin-gonic/gin"
	"github.com/naiba/solitudes/x/soligin"
)

type mediaInfo struct {
	Filename   string
	Article    solitudes.Article
	UploadedAt time.Time
}

func media(c *gin.Context) {
	rawPage := c.Query("page")
	page64, _ := strconv.ParseInt(rawPage, 10, 64)
	page := int(page64)
	if page < 0 {
		page = 0
	}
	medias, _ := solitudes.System.SafeCache.GetOrBuild(fmt.Sprintf("%s%d", solitudes.CacheKeyPrefixUploadFiles, page), func() (interface{}, error) {
		files, _ := ioutil.ReadDir("data/upload")
		end := (page + 1) * 10
		if len(files) < end {
			end = len(files)
		}
		var innerMedias []mediaInfo
		for i := page * 10; i < end; i++ {
			if files[i].IsDir() {
				continue
			}
			var item mediaInfo
			item.UploadedAt = files[i].ModTime()
			item.Filename = files[i].Name()
			if err := solitudes.System.DB.First(&item.Article, "content like ?", "%(/upload/"+item.Filename+")%").Error; err == gorm.ErrRecordNotFound {
				var ah solitudes.ArticleHistory
				if solitudes.System.DB.First(&ah, "content like ?", "%(/upload/"+item.Filename+")%").Error == nil {
					solitudes.System.DB.First(&item.Article, "id = ?", ah.ArticleID)
				}
			}
			innerMedias = append(innerMedias, item)
		}
		return innerMedias, nil
	})
	c.HTML(http.StatusOK, "admin/media", soligin.Soli(c, true, gin.H{
		"title":  "Media file manage",
		"medias": medias,
		"page":   page,
	}))
}
