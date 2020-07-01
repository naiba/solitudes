package router

import (
	"net/http"
	"strconv"
	"time"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/soligin"

	"github.com/biezhi/gorm-paginator/pagination"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/feeds"
)

func archive(c *gin.Context) {
	pageSlice := c.MustGet(solitudes.CtxRequestParams).([]string)
	var page int64
	if len(pageSlice) == 2 {
		page, _ = strconv.ParseInt(pageSlice[1], 10, 32)
	}
	var articles []model.Article
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.DB.Where("book_refer is NULL"),
		Page:    int(page),
		Limit:   15,
		OrderBy: []string{"created_at DESC"},
	}, &articles)
	for i := 0; i < len(articles); i++ {
		articles[i].RelatedCount(solitudes.System.DB, solitudes.System.Pool, checkPoolSubmit)
	}
	c.HTML(http.StatusOK, "default/archive", soligin.Soli(c, gin.H{
		"title":    c.MustGet(solitudes.CtxTranslator).(*solitudes.Translator).T("archive"),
		"what":     "archives",
		"articles": listArticleByYear(articles),
		"page":     pg,
	}))
}

func feedHandler(c *gin.Context) {

	feed := &feeds.Feed{
		Title:       solitudes.System.Config.SpaceName,
		Link:        &feeds.Link{Href: "https://" + solitudes.System.Config.Web.Domain},
		Description: solitudes.System.Config.SpaceDesc,
		Author:      &feeds.Author{Name: solitudes.System.Config.Web.User.Nickname, Email: solitudes.System.Config.Web.User.Email},
		Updated:     time.Now(),
	}

	var articles []model.Article
	solitudes.System.DB.Order("created_at DESC", true).Limit(20).Find(&articles)
	for i := 0; i < len(articles); i++ {
		feed.Items = append(feed.Items, &feeds.Item{
			Title:   articles[i].Title,
			Link:    &feeds.Link{Href: "https://" + solitudes.System.Config.Web.Domain + "/" + articles[i].Slug + "/v" + strconv.Itoa(int(articles[i].Version))},
			Author:  &feeds.Author{Name: solitudes.System.Config.Web.User.Nickname, Email: solitudes.System.Config.Web.User.Email},
			Content: articles[i].Content,
			Created: articles[i].CreatedAt,
			Updated: articles[i].UpdatedAt,
		})
	}
	pageSlice := c.MustGet(solitudes.CtxRequestParams).([]string)
	switch pageSlice[1] {
	case "atom":
		atom, err := feed.ToAtom()
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
		c.Header("Content-Type", "application/xml")
		c.String(http.StatusOK, atom)
	case "rss":
		rss, err := feed.ToRss()
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
		c.Header("Content-Type", "application/xml")
		c.String(http.StatusOK, rss)
	case "json":
		json, err := feed.ToJSON()
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
		c.Header("Content-Type", "application/json")
		c.String(http.StatusOK, json)
	default:
		c.String(http.StatusOK, "Unknown type")
	}
}

func tags(c *gin.Context) {
	pageSlice := c.MustGet(solitudes.CtxRequestParams).([]string)
	var page int64
	if len(pageSlice) == 3 {
		page, _ = strconv.ParseInt(pageSlice[2], 10, 32)
	}
	var articles []model.Article
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.DB.Where("tags @> ARRAY[?]::varchar[]", pageSlice[1]),
		Page:    int(page),
		Limit:   15,
		OrderBy: []string{"updated_at DESC"},
	}, &articles)
	for i := 0; i < len(articles); i++ {
		articles[i].RelatedCount(solitudes.System.DB, solitudes.System.Pool, checkPoolSubmit)
	}
	c.HTML(http.StatusOK, "default/archive", soligin.Soli(c, gin.H{
		"title":    c.MustGet(solitudes.CtxTranslator).(*solitudes.Translator).T("articles_in", pageSlice[1]),
		"what":     "tags",
		"articles": listArticleByYear(articles),
		"page":     pg,
	}))
}

func listArticleByYear(as []model.Article) [][]model.Article {
	var listed [][]model.Article
	var lastYear int
	var listItem []model.Article
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
