package router

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gorilla/feeds"
	"github.com/naiba/solitudes/pkg/pagination"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

func tagsCloud(c *fiber.Ctx) error {
	var tags []string
	var counts []int
	rows, err := solitudes.System.DB.Raw(`select count(*), unnest(articles.tags) t from articles group by t order by count desc`).Rows()
	if err != nil {
		return fmt.Errorf("failed to fetch tags cloud: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var line string
		var count int
		if err := rows.Scan(&count, &line); err != nil {
			return fmt.Errorf("failed to scan tag row: %w", err)
		}
		tags = append(tags, line)
		counts = append(counts, count)
	}
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	return c.Status(http.StatusOK).Render("site/tags", injectSiteData(c, fiber.Map{
		"title":  tr.T("tags_cloud"),
		"tags":   tags,
		"counts": counts,
	}))
}

func posts(c *fiber.Ctx) error {
	pageStr := c.Params("page")
	var page int64
	if pageStr != "" {
		var err error
		page, err = strconv.ParseInt(pageStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid page format: %w", err)
		}
	}
	var articles []model.Article
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.DB.Where("array_length(tags, 1) is null").Or("NOT tags @> ARRAY[?]::varchar[]", "Topic"),
		Page:    int(page),
		Limit:   20,
		OrderBy: []string{"created_at DESC"},
	}, &articles)
	for i := range articles {
		articles[i].RelatedCount(solitudes.System.DB)
		// 如果存在 Topic tag，加载前 5 条评论
		if articles[i].IsTopic() {
			pagination.Paging(&pagination.Param{
				DB:      solitudes.System.DB.Where("reply_to is null and article_id = ?", articles[i].ID),
				Limit:   5,
				OrderBy: []string{"created_at DESC"},
			}, &articles[i].Comments)
		}
	}
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	return c.Status(http.StatusOK).Render("site/posts", injectSiteData(c, fiber.Map{
		"title":    tr.T("posts"),
		"what":     "posts",
		"articles": listArticleByYear(articles),
		"page":     pg,
	}))
}

func book(c *fiber.Ctx) error {
	pageStr := c.Params("page")
	var page int64
	if pageStr != "" {
		var err error
		page, err = strconv.ParseInt(pageStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid page format: %w", err)
		}
	}
	var articles []model.Article
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.DB.Where("is_book is true"),
		Page:    int(page),
		Limit:   20,
		OrderBy: []string{"created_at DESC"},
	}, &articles)
	for i := range articles {
		articles[i].RelatedCount(solitudes.System.DB)
	}
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	return c.Status(http.StatusOK).Render("site/posts", injectSiteData(c, fiber.Map{
		"title":    tr.T("books"),
		"what":     "books",
		"articles": listArticleByYear(articles),
		"page":     pg,
	}))
}

func feedHandler(c *fiber.Ctx) error {
	ip := c.IP()
	if ip != "" {
		visit := model.FeedVisit{
			IP: ip,
		}
		if err := solitudes.System.DB.Create(&visit).Error; err != nil {
			log.Printf("Failed to record feed visit: %v", err)
		}
	}

	format := c.Params("format")
	if format == "" {
		return c.Status(http.StatusBadRequest).JSON(map[string]interface{}{
			"message":         "please spec a feed format",
			"supportedFormat": []string{"json", "rss", "atom"},
			"feedLink":        "https://" + solitudes.System.Config.Site.Domain + "/feed/:format",
		})
	}
	feed := &feeds.Feed{
		Title:       solitudes.System.Config.Site.SpaceName,
		Link:        &feeds.Link{Href: "https://" + solitudes.System.Config.Site.Domain},
		Description: solitudes.System.Config.Site.SpaceDesc,
		Author:      &feeds.Author{Name: solitudes.System.Config.User.Nickname, Email: solitudes.System.Config.User.Email},
		Updated:     time.Now(),
	}
	var articles []model.Article
	if err := solitudes.System.DB.Order("created_at DESC").Limit(20).Find(&articles).Error; err != nil {
		return fmt.Errorf("failed to fetch articles for feed: %w", err)
	}
	for i := range articles {
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
	switch format {
	case "atom":
		atom, err := feed.ToAtom()
		if err != nil {
			return fmt.Errorf("failed to generate atom feed: %w", err)
		}
		c.Set("Content-Type", "application/xml")
		_, err = c.Status(http.StatusOK).WriteString(atom)
		return err
	case "rss":
		rssFeed := (&feeds.Rss{Feed: feed}).RssFeed()
		rssFeed.Generator = "Solitudes v" + solitudes.BuildVersion + " github.com/naiba/solitudes"
		rss, err := feeds.ToXML(rssFeed)
		if err != nil {
			return fmt.Errorf("failed to generate rss feed: %w", err)
		}
		c.Set("Content-Type", "application/xml")
		_, err = c.Status(http.StatusOK).WriteString(rss)
		return err
	case "json":
		json, err := feed.ToJSON()
		if err != nil {
			return fmt.Errorf("failed to generate json feed: %w", err)
		}
		c.Set("Content-Type", "application/json")
		_, err = c.Status(http.StatusOK).WriteString(json)
		return err
	default:
		_, err := c.Status(http.StatusBadRequest).WriteString("Unknown feed type")
		return err
	}
}

func tags(c *fiber.Ctx) error {
	pageStr := c.Params("page")
	var page int64
	if pageStr != "" {
		var err error
		page, err = strconv.ParseInt(pageStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid page format: %w", err)
		}
	}
	var articles []model.Article
	tag, _ := url.QueryUnescape(c.Params("tag"))
	if tag == "" {
		return page404(c)
	}
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.DB.Where("tags @> ARRAY[?]::varchar[]", tag),
		Page:    int(page),
		Limit:   20,
		OrderBy: []string{"created_at DESC"},
	}, &articles)
	for i := range articles {
		articles[i].RelatedCount(solitudes.System.DB)
		// 如果存在 Topic tag，加载前 5 条评论
		if articles[i].IsTopic() {
			pagination.Paging(&pagination.Param{
				DB:      solitudes.System.DB.Where("reply_to is null and article_id = ?", articles[i].ID),
				Limit:   5,
				OrderBy: []string{"created_at DESC"},
			}, &articles[i].Comments)
		}
	}
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	return c.Status(http.StatusOK).Render("site/posts", injectSiteData(c, fiber.Map{
		"title":    tr.T("articles_in", tag),
		"what":     "tags",
		"tag":      tag,
		"articles": listArticleByYear(articles),
		"page":     pg,
	}))
}

func listArticleByYear(as []model.Article) [][]model.Article {
	var listed [][]model.Article
	var lastYear int
	var listItem []model.Article
	for _, article := range as {
		currentYear := article.CreatedAt.Year()
		if currentYear != lastYear {
			if len(listItem) > 0 {
				listed = append(listed, listItem)
				listItem = make([]model.Article, 0)
			}
			lastYear = currentYear
		}
		listItem = append(listItem, article)
	}
	if len(listItem) > 0 {
		listed = append(listed, listItem)
	}
	return listed
}
