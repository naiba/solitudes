package router

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/gorilla/feeds"
	"github.com/naiba/solitudes/pkg/pagination"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

const maxTagLength = 255

func validTagParam(tag string) bool {
	if tag == "" || utf8.RuneCountInString(tag) > maxTagLength {
		return false
	}
	for _, r := range tag {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func tagsCloud(c *fiber.Ctx) error {
	data, err := pagedTags(c, readableArticles(solitudes.System.DB, currentAccount(c)))
	if err != nil {
		return err
	}
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	data["title"], data["desc"] = tr.T("tags_cloud"), tr.T("tags_all_the_tags")
	return c.Status(http.StatusOK).Render("site/tags", injectSiteData(c, data))
}

func posts(c *fiber.Ctx) error {
	page, err := listPage(c.Params("page"))
	if err != nil {
		return err
	}
	var articles []model.Article
	pg, err := pagination.Paging(&pagination.Param{
		DB:      readableArticles(solitudes.System.DB, currentAccount(c), accessNoticeFor(c)).Preload("Author").Where("(array_length(tags, 1) is null OR NOT tags @> ARRAY[?]::varchar[])", "Topic"),
		Page:    int(page),
		Limit:   20,
		OrderBy: []string{"created_at DESC, id DESC"},
	}, &articles)
	if err != nil {
		return err
	}
	if err := model.AggregateBookCounts(readableArticles(solitudes.System.DB, currentAccount(c)), articles); err != nil {
		return err
	}
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	return c.Status(http.StatusOK).Render("site/posts", injectSiteData(c, fiber.Map{
		"title":        tr.T("posts"),
		"desc":         tr.T("posts_all_the_posts"),
		"what":         "posts",
		"navigation":   archiveNavigation(c, "/posts/", pg),
		"archive_base": "/posts/",
		"articles":     listArticleByYear(articles),
		"page":         pg,
		"noindex":      page > 1,
	}))
}

func book(c *fiber.Ctx) error {
	page, err := listPage(c.Params("page"))
	if err != nil {
		return err
	}
	var articles []model.Article
	pg, err := pagination.Paging(&pagination.Param{
		DB:      readableArticles(solitudes.System.DB, currentAccount(c), accessNoticeFor(c)).Preload("Author").Where("is_book is true"),
		Page:    int(page),
		Limit:   20,
		OrderBy: []string{"created_at DESC, id DESC"},
	}, &articles)
	if err != nil {
		return err
	}
	if err := model.AggregateBookCounts(readableArticles(solitudes.System.DB, currentAccount(c)), articles); err != nil {
		return err
	}
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	return c.Status(http.StatusOK).Render("site/posts", injectSiteData(c, fiber.Map{
		"title":        tr.T("books"),
		"desc":         tr.T("books_all_the_books"),
		"what":         "books",
		"navigation":   archiveNavigation(c, "/books/", pg),
		"archive_base": "/books/",
		"articles":     listArticleByYear(articles),
		"page":         pg,
		"noindex":      page > 1,
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
		return generateFeed(format)
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

func generateFeed(format string) (interface{}, error) {
	feed := &feeds.Feed{
		Title:       solitudes.System.Config.Site.SpaceName,
		Link:        &feeds.Link{Href: "https://" + solitudes.System.Config.Site.Domain},
		Description: solitudes.System.Config.Site.SpaceDesc,
		Updated:     time.Now(),
	}
	var articles []model.Article
	if err := solitudes.System.DB.Where("visibility = 'public'").Preload("Author").Order("created_at DESC").Limit(20).Find(&articles).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch articles for feed: %w", err)
	}
	feed.Items = publicFeedItems(articles)

	switch format {
	case "atom":
		// Atom requires a feed author when there are no entries. The site is
		// the publisher; individual entries still name their actual authors.
		feed.Author = &feeds.Author{Name: solitudes.System.Config.Site.SpaceName}
		atomFeed := (&feeds.Atom{Feed: feed}).AtomFeed()
		for i := range articles {
			if articles[i].Author.ID != "" && atomFeed.Entries[i].Author != nil {
				atomFeed.Entries[i].Author.Uri = "https://" + solitudes.System.Config.Site.Domain + "/users/" + articles[i].Author.ID
			}
		}
		body, err := feeds.ToXML(atomFeed)
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
		jsonFeed := (&feeds.JSON{Feed: feed}).JSONFeed()
		for i := range articles {
			if articles[i].Author.ID != "" && len(jsonFeed.Items[i].Authors) != 0 {
				profileURL := "https://" + solitudes.System.Config.Site.Domain + "/users/" + articles[i].Author.ID
				jsonFeed.Items[i].Authors[0].Url = profileURL
				jsonFeed.Items[i].Author.Url = profileURL
			}
		}
		body, err := jsonFeed.ToJSON()
		if err != nil {
			return nil, fmt.Errorf("failed to generate json feed: %w", err)
		}
		return &feedOutput{contentType: "application/json", body: body}, nil
	default:
		return nil, fmt.Errorf("unknown feed type: %s", format)
	}
}

func publicFeedItems(articles []model.Article) []*feeds.Item {
	items := make([]*feeds.Item, 0, len(articles))
	for i := range articles {
		// The feed has a shared singleflight key: never let an admin request
		// publish private content to a concurrent anonymous subscriber.
		articles[i].Content = articles[i].ContentFor(nil, nil)
		maskPrivateArticleContent(&articles[i], false)
		authorName := articles[i].Author.Nickname
		if authorName == "" {
			authorName = solitudes.System.Config.Site.SpaceName
		}
		items = append(items, &feeds.Item{
			Title:       articles[i].Title,
			Link:        &feeds.Link{Href: "https://" + solitudes.System.Config.Site.Domain + "/" + articles[i].Slug},
			Author:      &feeds.Author{Name: authorName},
			Description: mdExcerpt(articles[i].Content, 200),
			Content:     luteEngine.MarkdownStr(articles[i].GetIndexID(), articles[i].Content),
			Created:     articles[i].CreatedAt,
			Updated:     articles[i].UpdatedAt,
		})
	}

	return items
}

func tags(c *fiber.Ctx) error {
	tag, err := url.PathUnescape(c.Params("tag"))
	if err != nil || !validTagParam(tag) {
		return page404(c)
	}
	page, err := listPage(c.Params("page"))
	if err != nil {
		return err
	}
	var articles []model.Article
	pg, err := pagination.Paging(&pagination.Param{
		DB:      readableArticles(solitudes.System.DB, currentAccount(c), accessNoticeFor(c)).Preload("Author").Where("tags @> ARRAY[?]::varchar[]", tag),
		Page:    int(page),
		Limit:   20,
		OrderBy: []string{"created_at DESC, id DESC"},
	}, &articles)
	if err != nil {
		return err
	}
	if pg.TotalRecord == 0 || pg.Page > pg.TotalPage {
		return page404(c)
	}
	if err := model.AggregateBookCounts(readableArticles(solitudes.System.DB, currentAccount(c)), articles); err != nil {
		return err
	}
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	return c.Status(http.StatusOK).Render("site/posts", injectSiteData(c, fiber.Map{
		"title":        tr.T("articles_in", tag),
		"desc":         tr.T("posts_with_tag", tag),
		"what":         "tags",
		"navigation":   archiveNavigation(c, "/tags/"+url.PathEscape(tag)+"/", pg),
		"archive_base": "/tags/" + url.PathEscape(tag) + "/",
		"tag":          tag,
		"articles":     listArticleByYear(articles),
		"page":         pg,
		"noindex":      page > 1,
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
