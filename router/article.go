package router

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
)

func article(c *fiber.Ctx) error {
	var a model.Article
	if err := solitudes.System.DB.Preload("Author").Order("created_at DESC").Take(&a, "slug = ?", c.Params("slug")).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return page404(c)
		}
		return fmt.Errorf("failed to fetch article: %w", err)
	}
	// BugFix: TemplateID 为 0 时 TemplateIndex 查不到映射，导致模板名变成 "site/" 而报错
	if _, ok := solitudes.TemplateIndex[a.TemplateID]; !ok {
		a.TemplateID = solitudes.ArticleTemplateID
	}
	if len(a.Tags) == 0 {
		a.Tags = nil
	}

	var title string
	// load history
	if c.Params("version") != "" {
		// Historical bodies may predate a newly added restriction. Only the
		// author/administrator can retrieve revisions, regardless of audience.
		if !a.Allows("private", currentAccount(c)) {
			return page404(c)
		}
		version, err := strconv.ParseUint(c.Params("version")[1:], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid version format: %w", err)
		}
		if uint(version) == a.Version {
			return c.Redirect("/"+a.Slug, http.StatusMovedPermanently)
		}
		var history model.ArticleHistory
		if err := solitudes.System.DB.Take(&history, "article_id = ? and version = ?", a.ID, version).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return page404(c)
			}
			return fmt.Errorf("failed to fetch article history: %w", err)
		}
		a.NewVersion = a.Version
		a.Version = history.Version
		a.Content = history.Content
		a.CreatedAt = history.CreatedAt
		title = fmt.Sprintf("%s v%d", a.Title, a.Version)
	} else {
		title = a.Title
	}

	// Private drafts are accessible only to their author or an administrator.
	if !canReadArticle(currentAccount(c), &a) {
		return page404(c)
	}
	a.Content = a.ContentFor(currentAccount(c), accessNoticeFor(c))
	if !a.Public() {
		c.Set(fiber.HeaderXRobotsTag, "noindex, nofollow")
		c.Set(fiber.HeaderCacheControl, "private, no-store")
	}

	// 移除过度并发，改用顺序加载（对于单次请求，DB 查询的顺序执行通常比 5 个 goroutine 的调度开销更低且更可控）
	if err := a.LoadChapters(readableArticles(solitudes.System.DB, currentAccount(c))); err != nil {
		return err
	}
	relatedBook(&a, currentAccount(c))
	relatedSiblingArticle(&a, currentAccount(c))
	a.GenTOC()

	pg, navigation, thread, err := articleComments(c, &a)
	if err != nil {
		return err
	}

	desc := mdExcerpt(a.Content, 150)
	isOldVersion := c.Params("version") != ""

	return c.Status(http.StatusOK).Render("site/"+solitudes.TemplateIndex[a.TemplateID], injectSiteData(c, fiber.Map{
		"title":              title,
		"desc":               desc,
		"og_type":            "article",
		"keywords":           a.RawTags,
		"article":            &a,
		"can_read_history":   a.Allows("private", currentAccount(c)),
		"comment_navigation": navigation,
		"thread":             thread,
		"comment_page":       pg,
		"noindex":            isOldVersion || !a.Public() || pg.Page > 1 || thread != "",
	}))
}

func relatedSiblingArticle(p *model.Article, account *model.Account) (prev model.Article, next model.Article) {
	var sb model.SibilingArticle
	if p.BookRefer == nil {
		readableArticles(solitudes.System.DB, account).Select("id,title,slug").Order("created_at ASC, id ASC").Take(&sb.Next, "book_refer is null and (created_at, id) > (?, ?)", p.CreatedAt, p.ID)
		readableArticles(solitudes.System.DB, account).Select("id,title,slug").Order("created_at DESC, id DESC").Take(&sb.Prev, "book_refer is null and (created_at, id) < (?, ?)", p.CreatedAt, p.ID)
	} else {
		readableArticles(solitudes.System.DB, account).Select("id,title,slug").Order("created_at ASC, id ASC").Take(&sb.Next, "book_refer = ? and (created_at, id) > (?, ?)", p.BookRefer, p.CreatedAt, p.ID)
		readableArticles(solitudes.System.DB, account).Select("id,title,slug").Order("created_at DESC, id DESC").Take(&sb.Prev, "book_refer = ? and (created_at, id) < (?, ?)", p.BookRefer, p.CreatedAt, p.ID)
	}
	p.SibilingArticle = &sb
	return
}

func relatedBook(p *model.Article, account *model.Account) {
	if p.BookRefer != nil {
		var book model.Article
		if err := readableArticles(solitudes.System.DB, account).Select("id, title, slug").Take(&book, "id = ?", p.BookRefer).Error; err == nil {
			p.Book = &book
		}
	}
}
