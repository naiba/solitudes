package wengine

import (
	"net/http"
	"strconv"

	"github.com/biezhi/gorm-paginator/pagination"
	"github.com/gin-gonic/gin"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/x/soligin"
)

func archive(c *gin.Context) {
	pageSlice := c.MustGet(solitudes.CtxRequestParams).([]string)
	var page int64
	if len(pageSlice) == 2 {
		page, _ = strconv.ParseInt(pageSlice[1], 10, 32)
	}
	var articles []solitudes.Article
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.D,
		Page:    int(page),
		Limit:   15,
		OrderBy: []string{"id desc"},
	}, &articles)
	c.HTML(http.StatusOK, "default/archive", soligin.Soli(c, false, gin.H{
		"title":    "Archive",
		"what":     "archives",
		"articles": listArticleByYear(articles),
		"page":     pg,
	}))
}

func tags(c *gin.Context) {
	pageSlice := c.MustGet(solitudes.CtxRequestParams).([]string)
	var page int64
	if len(pageSlice) == 3 {
		page, _ = strconv.ParseInt(pageSlice[2], 10, 32)
	}
	var articles []solitudes.Article
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.D.Where("tags @> ARRAY[?]::varchar[]", pageSlice[1]),
		Page:    int(page),
		Limit:   15,
		OrderBy: []string{"id desc"},
	}, &articles)
	c.HTML(http.StatusOK, "default/archive", soligin.Soli(c, false, gin.H{
		"title":    "Articles in \"" + pageSlice[1] + "\"",
		"what":     "tags",
		"articles": listArticleByYear(articles),
		"page":     pg,
	}))
}

func listArticleByYear(as []solitudes.Article) [][]solitudes.Article {
	var listed [][]solitudes.Article
	var lastYear int
	var listItem []solitudes.Article
	for i := 0; i < len(as); i++ {
		currentYear := as[i].UpdatedAt.Year()
		if currentYear != lastYear {
			if len(listItem) > 0 {
				listed = append(listed, listItem)
			}
			lastYear = currentYear
		}
		listItem = append(listItem, as[i])
	}
	if len(listItem) > 0 {
		listed = append(listed, listItem)
	}
	return listed
}
