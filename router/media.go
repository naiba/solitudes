package router

import (
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/gofiber/fiber"
	"github.com/jinzhu/gorm"

	"github.com/naiba/solitudes"

	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func mediaHandler(c *fiber.Ctx) {
	name := c.Query("name")
	if err := os.Remove("data/upload/" + path.Clean(name)); err != nil {
		c.Status(http.StatusInternalServerError).Write(err.Error())
	}
}

type mediaInfo struct {
	Filename   string
	Article    model.Article
	UploadedAt time.Time
}

func media(c *fiber.Ctx) {
	rawPage := c.Query("page")
	page64, _ := strconv.ParseInt(rawPage, 10, 64)
	page := int(page64)
	if page < 0 {
		page = 0
	}
	//medias, _ := solitudes.System.SafeCache.GetOrBuild(fmt.Sprintf("%s%d", solitudes.CacheKeyPrefixUploadFiles, page), func() (interface{}, error) {
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
		if err := solitudes.System.DB.Take(&item.Article, "content like ?", "%(/upload/"+item.Filename+")%").Error; err == gorm.ErrRecordNotFound {
			var ah model.ArticleHistory
			if solitudes.System.DB.Take(&ah, "content like ?", "%(/upload/"+item.Filename+")%").Error == nil {
				solitudes.System.DB.Take(&item.Article, "id = ?", ah.ArticleID)
				item.Article.Version = ah.Version
			}
		}
		innerMedias = append(innerMedias, item)
	}
	//	return innerMedias, nil
	//})
	c.Status(http.StatusOK).Render("admin/media", injectSiteData(c, fiber.Map{
		"title":  c.Locals(solitudes.CtxTranslator).(*translator.Translator).T("manage_media"),
		"medias": innerMedias,
		"page":   page,
	}))
}
