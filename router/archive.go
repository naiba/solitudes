package router

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/biezhi/gorm-paginator/pagination"
	"github.com/gofiber/fiber/v2"
	"github.com/gorilla/feeds"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func tagsCloud(c *fiber.Ctx) error {
	var tags []string
	var counts []int
	rows, err := solitudes.System.DB.Raw(`select count(*), unnest(articles.tags) t from articles group by t order by count desc`).Rows()
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var line string
			var count int
			rows.Scan(&count, &line)
			tags = append(tags, line)
			counts = append(counts, count)
		}
	}
	c.Status(http.StatusOK).Render("default/tags", injectSiteData(c, fiber.Map{
		"title":  c.Locals(solitudes.CtxTranslator).(*translator.Translator).T("tags_cloud"),
		"tags":   tags,
		"counts": counts,
	}))
	return nil
}

func archive(c *fiber.Ctx) error {
	var page int64
	page, _ = strconv.ParseInt(c.Params("page"), 10, 64)
	var articles []model.Article
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.DB.Where("NOT tags @> ARRAY[?]::varchar[]", "Topic"),
		Page:    int(page),
		Limit:   20,
		OrderBy: []string{"created_at DESC"},
	}, &articles)
	for i := 0; i < len(articles); i++ {
		articles[i].RelatedCount(solitudes.System.DB, solitudes.System.Pool, checkPoolSubmit)
		// 如果存在 Topic tag，加载前 3 条评论
		if articles[i].IsTopic() {
			pagination.Paging(&pagination.Param{
				DB:      solitudes.System.DB.Where("reply_to is null and article_id = ?", articles[i].ID),
				Limit:   5,
				OrderBy: []string{"created_at DESC"},
			}, &articles[i].Comments)
		}
	}
	c.Status(http.StatusOK).Render("default/archive", injectSiteData(c, fiber.Map{
		"title":    c.Locals(solitudes.CtxTranslator).(*translator.Translator).T("archive"),
		"what":     "archives",
		"articles": listArticleByYear(articles),
		"page":     pg,
	}))
	return nil
}

func feedHandler(c *fiber.Ctx) error {
	if c.Params("format") == "" {
		c.Status(http.StatusBadRequest).JSON(map[string]interface{}{
			"message":         "please spec a feed format",
			"supportedFormat": []string{"json", "rss", "atom"},
			"feedLink":        "https://" + solitudes.System.Config.Site.Domain + "/feed/:format",
		})
		return nil
	}
	feed := &feeds.Feed{
		Title:       solitudes.System.Config.Site.SpaceName,
		Link:        &feeds.Link{Href: "https://" + solitudes.System.Config.Site.Domain},
		Description: solitudes.System.Config.Site.SpaceDesc,
		Author:      &feeds.Author{Name: solitudes.System.Config.User.Nickname, Email: solitudes.System.Config.User.Email},
		Updated:     time.Now(),
	}
	var articles []model.Article
	solitudes.System.DB.Order("created_at DESC", true).Limit(20).Find(&articles)
	for i := 0; i < len(articles); i++ {
		// 检查私有博文
		if articles[i].IsPrivate && !c.Locals(solitudes.CtxAuthorized).(bool) {
			articles[i].Content = "Private Article"
		}

		feed.Items = append(feed.Items, &feeds.Item{
			Title:   articles[i].Title,
			Link:    &feeds.Link{Href: "https://" + solitudes.System.Config.Site.Domain + "/" + articles[i].Slug + "/v" + strconv.Itoa(int(articles[i].Version))},
			Author:  &feeds.Author{Name: solitudes.System.Config.User.Nickname, Email: solitudes.System.Config.User.Email},
			Content: luteEngine.MarkdownStr(articles[i].GetIndexID(), articles[i].Content),
			Created: articles[i].CreatedAt,
			Updated: articles[i].UpdatedAt,
		})
	}
	switch c.Params("format") {
	case "atom":
		atom, err := feed.ToAtom()
		if err != nil {
			return err
		}
		c.Set("Content-Type", "application/xml")
		c.Status(http.StatusOK).WriteString(atom)
	case "rss":
		rss, err := feed.ToRss()
		if err != nil {
			return err
		}
		c.Set("Content-Type", "application/xml")
		c.Status(http.StatusOK).WriteString(rss)
	case "json":
		json, err := feed.ToJSON()
		if err != nil {
			return err
		}
		c.Set("Content-Type", "application/json")
		c.Status(http.StatusOK).WriteString(json)
	default:
		c.Status(http.StatusOK).WriteString("Unknown type")
	}
	return nil
}

func tags(c *fiber.Ctx) error {
	var page int64
	page, _ = strconv.ParseInt(c.Params("page"), 10, 64)
	var articles []model.Article
	tag, _ := url.QueryUnescape(c.Params("tag"))
	if tag == "" {
		page404(c)
		return nil
	}
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.DB.Where("tags @> ARRAY[?]::varchar[]", tag),
		Page:    int(page),
		Limit:   20,
		OrderBy: []string{"created_at DESC"},
	}, &articles)
	for i := 0; i < len(articles); i++ {
		articles[i].RelatedCount(solitudes.System.DB, solitudes.System.Pool, checkPoolSubmit)
		// 如果存在 Topic tag，加载前 3 条评论
		if articles[i].IsTopic() {
			pagination.Paging(&pagination.Param{
				DB:      solitudes.System.DB.Where("reply_to is null and article_id = ?", articles[i].ID),
				Limit:   5,
				OrderBy: []string{"created_at DESC"},
			}, &articles[i].Comments)
		}
	}
	c.Status(http.StatusOK).Render("default/archive", injectSiteData(c, fiber.Map{
		"title":    c.Locals(solitudes.CtxTranslator).(*translator.Translator).T("articles_in", tag),
		"what":     "tags",
		"articles": listArticleByYear(articles),
		"page":     pg,
	}))
	return nil
}

func listArticleByYear(as []model.Article) [][]model.Article {
	var listed [][]model.Article
	var lastYear int
	var listItem []model.Article
	for i := 0; i < len(as); i++ {
		currentYear := as[i].CreatedAt.Year()
		if currentYear != lastYear {
			if len(listItem) > 0 {
				listed = append(listed, listItem)
				listItem = make([]model.Article, 0)
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
