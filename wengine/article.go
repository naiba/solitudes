package wengine

import (
	"net/http"
	"strconv"

	"github.com/biezhi/gorm-paginator/pagination"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
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
	article.DeletedAt = nil
	if err := solitudes.System.D.Save(&article).Error; err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
}

func article(c *gin.Context) {
	slug := c.MustGet(solitudes.CtxRequestParams).([]string)
	var a solitudes.Article

	if err := solitudes.System.D.Where("slug = ?", slug[1]).First(&a).Error; err == gorm.ErrRecordNotFound {
		c.HTML(http.StatusNotFound, "default/404", soligin.Soli(gin.H{}))
		return
	} else if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	c.HTML(http.StatusOK, "default/"+solitudes.TemplateIndex[a.TemplateID], soligin.Soli(gin.H{
		"article": a,
	}))
}

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
	c.HTML(http.StatusOK, "default/archive", soligin.Soli(gin.H{
		"articles": listArticleByYear(articles),
		"page":     pg,
	}))
}

func listArticleByYear(as []solitudes.Article) [][]solitudes.Article {
	var listed = make([][]solitudes.Article, 0)
	var lastYear int
	var listItem []solitudes.Article
	for i := 0; i < len(as); i++ {
		currentYear := as[i].UpdatedAt.Year()
		if currentYear != lastYear {
			if len(listItem) > 0 {
				listed = append(listed, listItem)
			}
			listItem = make([]solitudes.Article, 0)
			lastYear = currentYear
		}
		listItem = append(listItem, as[i])
	}
	if len(listItem) > 0 {
		listed = append(listed, listItem)
	}
	return listed
}
