package router

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/naiba/solitudes/pkg/pagination"
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

	// 移除过度并发，改用顺序加载（对于单次请求，DB 查询的顺序执行通常比 5 个 goroutine 的调度开销更低且更可控）
	relatedChapters(&a, currentAccount(c))
	relatedBook(&a, currentAccount(c))
	relatedSiblingArticle(&a, currentAccount(c))
	a.GenTOC()

	// 仅对评论加载保持 Paging 逻辑（这里由于 Paging 依赖 pg 变量，保持原逻辑但移除 goroutine）
	pageSlice := c.Query("comment_page")
	var page int64
	if pageSlice != "" {
		page, _ = strconv.ParseInt(pageSlice, 10, 32)
	}
	pg := pagination.Paging(&pagination.Param{
		DB:      visibleComments(solitudes.System.DB).Where("reply_to is null and article_id = ?", a.ID),
		Page:    int(page),
		Limit:   20,
		OrderBy: []string{"created_at DESC"},
	}, &a.Comments)
	pg.TotalRecord = int(a.CommentNum)
	// load childComments
	relatedChildComments(&a, a.Comments, true)

	a.RelatedCount(solitudes.System.DB)

	desc := mdExcerpt(a.Content, 150)
	isOldVersion := c.Params("version") != ""

	return c.Status(http.StatusOK).Render("site/"+solitudes.TemplateIndex[a.TemplateID], injectSiteData(c, fiber.Map{
		"title":        title,
		"desc":         desc,
		"og_type":      "article",
		"keywords":     a.RawTags,
		"article":      &a,
		"comment_page": pg,
		"noindex":      isOldVersion,
	}))
}

func relatedSiblingArticle(p *model.Article, account *model.Account) (prev model.Article, next model.Article) {
	var sb model.SibilingArticle
	if p.BookRefer == nil {
		readableArticles(solitudes.System.DB, account).Select("id,title,slug").Order("created_at ASC").Take(&sb.Next, "book_refer is null and created_at > ?", p.CreatedAt)
		readableArticles(solitudes.System.DB, account).Select("id,title,slug").Order("created_at DESC").Take(&sb.Prev, "book_refer is null and created_at < ?", p.CreatedAt)
	} else {
		readableArticles(solitudes.System.DB, account).Select("id,title,slug").Order("created_at ASC").Take(&sb.Next, "book_refer = ? and created_at > ?", p.BookRefer, p.CreatedAt)
		readableArticles(solitudes.System.DB, account).Select("id,title,slug").Order("created_at DESC").Take(&sb.Prev, "book_refer = ? and created_at < ?", p.BookRefer, p.CreatedAt)
	}
	p.SibilingArticle = &sb
	return
}

func relatedChapters(p *model.Article, account *model.Account) {
	if p.IsBook {
		p.Chapters = innerRelatedChapters(p.ID, account)
	}
}

func innerRelatedChapters(pid string, account *model.Account) (ps []*model.Article) {
	readableArticles(solitudes.System.DB, account).Order("created_at ASC").Find(&ps, "book_refer=?", pid)
	for i := range ps {
		if ps[i].IsBook {
			ps[i].Chapters = innerRelatedChapters(ps[i].ID, account)
		}
	}
	return
}

func relatedBook(p *model.Article, account *model.Account) {
	if p.BookRefer != nil {
		var book model.Article
		if err := readableArticles(solitudes.System.DB, account).Take(&book, "id = ?", p.BookRefer).Error; err == nil {
			p.Book = &book
		}
	}
}

func relatedChildComments(a *model.Article, cm []*model.Comment, root bool) {
	if root {
		var idMaptoComment = make(map[string]*model.Comment)
		var idArray []string
		// map to index
		for i := range cm {
			idMaptoComment[cm[i].ID] = cm[i]
			idArray = append(idArray, cm[i].ID)
		}
		var cms []*model.Comment
		solitudes.System.DB.Raw(`WITH RECURSIVE cs AS (SELECT comments.* FROM comments WHERE comments.is_spam = false AND comments.reply_to in (?) union ALL
		SELECT comments.* FROM comments, cs WHERE comments.is_spam = false AND comments.reply_to = cs.id)
		SELECT * FROM cs ORDER BY created_at;`, idArray).Scan(&cms)
		// map to index
		for i := range cms {
			if cms[i].ReplyTo != nil {
				idMaptoComment[cms[i].ID] = cms[i]
			}
		}
		// set child comments
		for i := range cms {
			if _, has := idMaptoComment[*cms[i].ReplyTo]; has {
				idMaptoComment[*cms[i].ReplyTo].ChildComments =
					append(idMaptoComment[*cms[i].ReplyTo].ChildComments, cms[i])
			}
		}
	}
	for i := range cm {
		cm[i].Article = a
		if len(cm[i].ChildComments) > 0 {
			relatedChildComments(a, cm[i].ChildComments, false)
			continue
		}
	}
}
