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
		"desc":   tr.T("tags_all_the_tags"),
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
		if page <= 1 {
			return c.Redirect("/posts/", http.StatusMovedPermanently)
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
		if articles[i].IsPrivate && !c.Locals(solitudes.CtxAuthorized).(bool) {
			articles[i].Content = "Private Article"
		}
	}
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	return c.Status(http.StatusOK).Render("site/posts", injectSiteData(c, fiber.Map{
		"title":    tr.T("posts"),
		"desc":     tr.T("posts_all_the_posts"),
		"what":     "posts",
		"articles": listArticleByYear(articles),
		"page":     pg,
		"noindex":  page > 1,
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
		if page <= 1 {
			return c.Redirect("/books/", http.StatusMovedPermanently)
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
		"desc":     tr.T("books_all_the_books"),
		"what":     "books",
		"articles": listArticleByYear(articles),
		"page":     pg,
		"noindex":  page > 1,
	}))
}

func countFeedSubscribers() (int64, error) {
	var count int64
	oneDayAgo := time.Now().Add(-24 * time.Hour)
	err := solitudes.System.DB.Raw(`
		SELECT COUNT(*) FROM (
			SELECT ip FROM feed_visits
			WHERE created_at > ?
			GROUP BY ip
			HAVING COUNT(*) >= 3
		) t
	`, oneDayAgo).Scan(&count).Error
	return count, err
}

var validFeedFormats = map[string]bool{
	"json": true,
	"rss":  true,
	"atom": true,
}

func feedHandler(c *fiber.Ctx) error {
	format := c.Params("format")

	// 仅合法 format 才计数
	if validFeedFormats[format] {
		ip := c.IP()
		if ip != "" {
			threshold := time.Now().Add(-20 * time.Minute)
			var recent model.FeedVisit
			err := solitudes.System.DB.Where("ip = ? AND created_at > ?", ip, threshold).
				Order("created_at DESC").First(&recent).Error
			if err != nil {
				// 没有近期记录（含 ErrRecordNotFound），插入新记录
				visit := model.FeedVisit{IP: ip}
				if err := solitudes.System.DB.Create(&visit).Error; err != nil {
					log.Printf("Failed to record feed visit: %v", err)
				}
			}
		}
	}

	if format == "" {
		result, err, _ := solitudes.System.SafeCache.Do("feed:subscribers", func() (interface{}, error) {
			return countFeedSubscribers()
		})
		var subscriberCount int64
		if err == nil {
			subscriberCount = result.(int64)
		}

		return c.Status(http.StatusOK).JSON(map[string]interface{}{
			"message":         "please spec a feed format",
			"supportedFormat": []string{"json", "rss", "atom"},
			"feedLink":        "https://" + solitudes.System.Config.Site.Domain + "/feed/:format",
			"subscribers":     subscriberCount,
		})
	}

	if !validFeedFormats[format] {
		_, err := c.Status(http.StatusBadRequest).WriteString("Unknown feed type")
		return err
	}

	// 使用 singleflight 避免并发刷接口
	result, err, _ := solitudes.System.SafeCache.Do("feed:"+format, func() (interface{}, error) {
		return generateFeed(c, format)
	})
	if err != nil {
		return err
	}

	feedResult := result.(*feedOutput)
	c.Set("Content-Type", feedResult.contentType)
	_, err = c.Status(http.StatusOK).WriteString(feedResult.body)
	return err
}

type feedOutput struct {
	contentType string
	body        string
}

func generateFeed(c *fiber.Ctx, format string) (interface{}, error) {
	feed := &feeds.Feed{
		Title:       solitudes.System.Config.Site.SpaceName,
		Link:        &feeds.Link{Href: "https://" + solitudes.System.Config.Site.Domain},
		Description: solitudes.System.Config.Site.SpaceDesc,
		Author:      &feeds.Author{Name: solitudes.System.Config.User.Nickname, Email: solitudes.System.Config.User.Email},
		Updated:     time.Now(),
	}
	var articles []model.Article
	if err := solitudes.System.DB.Order("created_at DESC").Limit(20).Find(&articles).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch articles for feed: %w", err)
	}

	isAuthorized := c.Locals(solitudes.CtxAuthorized).(bool)
	for i := range articles {
		if articles[i].IsPrivate && !isAuthorized {
			articles[i].Content = "Private Article"
		}
		feed.Items = append(feed.Items, &feeds.Item{
			Title:       articles[i].Title,
			Link:        &feeds.Link{Href: "https://" + solitudes.System.Config.Site.Domain + "/" + articles[i].Slug},
			Author:      &feeds.Author{Name: solitudes.System.Config.User.Nickname, Email: solitudes.System.Config.User.Email},
			Description: mdExcerpt(articles[i].Content, 200),
			Content:     luteEngine.MarkdownStr(articles[i].GetIndexID(), articles[i].Content),
			Created:     articles[i].CreatedAt,
			Updated:     articles[i].UpdatedAt,
		})
	}

	switch format {
	case "atom":
		body, err := feed.ToAtom()
		if err != nil {
			return nil, fmt.Errorf("failed to generate atom feed: %w", err)
		}
		return &feedOutput{contentType: "application/xml", body: body}, nil
	case "rss":
		rssFeed := (&feeds.Rss{Feed: feed}).RssFeed()
		rssFeed.Generator = "Solitudes v" + solitudes.BuildVersion + " github.com/naiba/solitudes"
		body, err := feeds.ToXML(rssFeed)
		if err != nil {
			return nil, fmt.Errorf("failed to generate rss feed: %w", err)
		}
		return &feedOutput{contentType: "application/xml", body: body}, nil
	case "json":
		body, err := feed.ToJSON()
		if err != nil {
			return nil, fmt.Errorf("failed to generate json feed: %w", err)
		}
		return &feedOutput{contentType: "application/json", body: body}, nil
	default:
		return nil, fmt.Errorf("unknown feed type: %s", format)
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
		if page <= 1 {
			tag, _ := url.QueryUnescape(c.Params("tag"))
			return c.Redirect("/tags/"+url.PathEscape(tag)+"/", http.StatusMovedPermanently)
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
		if articles[i].IsPrivate && !c.Locals(solitudes.CtxAuthorized).(bool) {
			articles[i].Content = "Private Article"
		}
	}
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	return c.Status(http.StatusOK).Render("site/posts", injectSiteData(c, fiber.Map{
		"title":    tr.T("articles_in", tag),
		"desc":     tr.T("posts_with_tag", tag),
		"what":     "tags",
		"tag":      tag,
		"articles": listArticleByYear(articles),
		"page":     pg,
		"noindex":  page > 1,
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
