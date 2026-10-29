package router

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/siteurl"
)

func article(writer http.ResponseWriter, httpRequest *http.Request) error {
	// Route parameters are escaped path segments; decode once before lookup so
	// sitemap, browser and canonical URLs all identify the same stored slug.
	slug, err := siteurl.DecodeArticleSlug(routeParam(httpRequest, "slug"))
	if err != nil {
		return page404(writer, httpRequest)
	}
	var a model.Article
	if err := solitudes.System.DB.Preload("Author").Preload("UpdatedBy").Order("created_at DESC").Take(&a, "slug = ?", slug).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return page404(writer, httpRequest)
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
	// Every revision inherits the latest article's audience. Authorize before
	// querying history or redirecting a request for the current version.
	if !canReadArticle(currentAccount(writer, httpRequest), &a) {
		return page404(writer, httpRequest)
	}

	var title string
	// load history
	if routeParam(httpRequest, "version") != "" {
		versionParam := routeParam(httpRequest, "version")
		if len(versionParam) < 2 || versionParam[0] != 'v' {
			return page404(writer, httpRequest)
		}
		version, err := strconv.ParseUint(versionParam[1:], 10, strconv.IntSize)
		if err != nil || version == 0 {
			return page404(writer, httpRequest)
		}
		if uint(version) == a.Version {
			return redirectResponse(writer, httpRequest, siteurl.ArticlePath(a.Slug), http.StatusMovedPermanently)
		}
		a, err = loadArticleRevision(writer, httpRequest, a, uint(version))
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return page404(writer, httpRequest)
			}
			return fmt.Errorf("failed to fetch article history: %w", err)
		}
		title = fmt.Sprintf("%s v%d", a.Title, a.Version)
	} else {
		title = a.Title
	}

	// Apply the selected revision's own fragment rules, not the latest body's.
	a.Content = a.ContentFor(currentAccount(writer, httpRequest), accessNoticeFor(writer, httpRequest))
	if !a.Public() {
		writer.Header().Set("X-Robots-Tag", "noindex, nofollow")
		writer.Header().Set("Cache-Control", "private, no-store")
	}

	// Keep model data available to custom templates; each theme chooses its UI.
	if err := a.LoadChapters(readableArticles(solitudes.System.DB, currentAccount(writer, httpRequest))); err != nil {
		return err
	}
	relatedBook(&a, currentAccount(writer, httpRequest))
	relatedSiblingArticle(&a, currentAccount(writer, httpRequest))
	a.GenTOC()

	pg, navigation, thread, err := articleComments(writer, httpRequest, &a)
	if err != nil {
		return err
	}

	desc := mdExcerpt(a.Content, 150)
	isOldVersion := routeParam(httpRequest, "version") != ""
	ogType := "article"
	if a.TemplateID == solitudes.PageTemplateID {
		ogType = "website"
	}

	data := viewData{
		"title":              title,
		"desc":               desc,
		"og_type":            ogType,
		"keywords":           a.RawTags,
		"article":            &a,
		"can_read_history":   true, // The latest article's audience was checked above.
		"comment_navigation": navigation,
		"thread":             thread,
		"comment_page":       pg,
		"noindex":            isOldVersion || !a.Public() || pg.Page > 1 || thread != "",
	}
	if a.Public() {
		data["canonical_path"] = siteurl.ArticlePath(a.Slug)
		if !isOldVersion {
			writer.Header().Set("Link", "<"+articleMarkdownURL(&a)+">; rel=\"alternate\"; type=\"text/markdown\", <"+publicBaseURL()+"/llms.txt>; rel=\"describedby\"")
		}
	}
	return renderPage(writer, httpRequest, http.StatusOK, "site/"+solitudes.TemplateIndex[a.TemplateID], injectSiteData(writer, httpRequest, data))
}

func relatedSiblingArticle(p *model.Article, account *model.Account) (prev model.Article, next model.Article) {
	var sb model.SibilingArticle
	articles := readableArticles(solitudes.System.DB, account).Where("template_id <> ?", solitudes.PageTemplateID)
	if p.BookRefer == nil {
		articles.Session(&gorm.Session{}).Select("id,title,slug").Order("created_at ASC, id ASC").Take(&sb.Next, "book_refer is null and (created_at, id) > (?, ?)", p.CreatedAt, p.ID)
		articles.Session(&gorm.Session{}).Select("id,title,slug").Order("created_at DESC, id DESC").Take(&sb.Prev, "book_refer is null and (created_at, id) < (?, ?)", p.CreatedAt, p.ID)
	} else {
		articles.Session(&gorm.Session{}).Select("id,title,slug").Order("created_at ASC, id ASC").Take(&sb.Next, "book_refer = ? and (created_at, id) > (?, ?)", p.BookRefer, p.CreatedAt, p.ID)
		articles.Session(&gorm.Session{}).Select("id,title,slug").Order("created_at DESC, id DESC").Take(&sb.Prev, "book_refer = ? and (created_at, id) < (?, ?)", p.BookRefer, p.CreatedAt, p.ID)
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
