package router

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/88250/lute/ast"
	"github.com/88250/lute/parse"
	"golang.org/x/net/html"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/content"
	"github.com/naiba/solitudes/pkg/pagination"
	"github.com/naiba/solitudes/pkg/siteurl"
	"github.com/naiba/solitudes/pkg/translator"
)

func manageArticle(writer http.ResponseWriter, httpRequest *http.Request) error {
	if err := requirePublishRole(writer, httpRequest); err != nil {
		return err
	}
	page, err := listPage(queryValue(httpRequest, "page"))
	if err != nil {
		return err
	}
	var as []model.Article
	db := solitudes.System.DB
	if account := currentAccount(writer, httpRequest); account != nil && !account.Role.IsAdmin() {
		db = db.Where("author_id = ?", account.ID)
	}
	pg, err := pagination.Paging(&pagination.Param{
		DB:      db.Preload("Author"),
		Page:    int(page),
		Limit:   20,
		OrderBy: []string{"created_at DESC, id DESC"},
	}, &as)
	if err != nil {
		return err
	}
	if err := model.AggregateBookCounts(readableArticles(solitudes.System.DB, currentAccount(writer, httpRequest)), as); err != nil {
		return err
	}
	tr := requestValue(httpRequest, solitudes.CtxTranslator).(*translator.Translator)
	return renderPage(writer, httpRequest, http.StatusOK, "admin/articles", injectSiteData(writer, httpRequest, viewData{
		"title":    tr.T("manage_articles"),
		"articles": as,
		"page":     pg,
	}))
}

func publish(writer http.ResponseWriter, httpRequest *http.Request) error {
	if err := requirePublishRole(writer, httpRequest); err != nil {
		return err
	}
	id := queryValue(httpRequest, "id")
	var article model.Article
	if id != "" {
		if err := solitudes.System.DB.Take(&article, "id = ?", id).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to fetch article for editing: %w", err)
		}
		if !mayEditArticle(currentAccount(writer, httpRequest), &article) {
			return newHTTPError(http.StatusNotFound)
		}
	}
	tr := requestValue(httpRequest, solitudes.CtxTranslator).(*translator.Translator)
	return renderPage(writer, httpRequest, http.StatusOK, "admin/publish", injectSiteData(writer, httpRequest, viewData{
		"title":     tr.T("publish_article"),
		"templates": solitudes.Templates,
		"article":   article,
	}))
}

func deleteArticle(writer http.ResponseWriter, httpRequest *http.Request) error {
	if err := requirePublishRole(writer, httpRequest); err != nil {
		return err
	}
	id := queryValue(httpRequest, "id")
	if len(id) < 10 {
		return errors.New("invalid article id")
	}
	var a model.Article
	if err := solitudes.System.DB.Select("id", "author_id").Preload("ArticleHistories").Take(&a, "id = ?", id).Error; err != nil {
		return fmt.Errorf("failed to find article for deletion: %w", err)
	}
	if !mayEditArticle(currentAccount(writer, httpRequest), &a) {
		return newHTTPError(http.StatusNotFound)
	}
	requestValue(httpRequest, "audit_target", a.ID)
	var indexIDs []string
	indexIDs = append(indexIDs, a.GetIndexID())
	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockRedirectRules(tx); err != nil {
			return err
		}
		if err := tx.Where("article_id = ?", a.ID).Delete(&model.RedirectRule{}).Error; err != nil {
			return fmt.Errorf("delete article redirects: %w", err)
		}
		// 删除文章
		if err := tx.Delete(&model.Article{}, "id = ?", a.ID).Error; err != nil {
			return fmt.Errorf("failed to delete article: %w", err)
		}
		// 删除文章历史
		for _, history := range a.ArticleHistories {
			indexIDs = append(indexIDs, history.GetIndexID())
		}
		if err := tx.Delete(&model.ArticleHistory{}, "article_id = ?", a.ID).Error; err != nil {
			return fmt.Errorf("failed to delete article histories: %w", err)
		}
		// 删除评论
		if err := tx.Delete(&model.Comment{}, "article_id = ?", a.ID).Error; err != nil {
			return fmt.Errorf("failed to delete article comments: %w", err)
		}
		return nil
	})

	if err != nil {
		return err
	}
	// delete full-text search data
	for _, indexID := range indexIDs {
		solitudes.System.Search.Delete(indexID)
	}
	return nil
}

type publishArticle struct {
	ID             string                  `form:"id"`
	Title          string                  `form:"title"`
	Slug           string                  `form:"slug"`
	Content        string                  `form:"content"`
	Template       byte                    `form:"template"`
	Tags           string                  `form:"tags"`
	IsBook         bool                    `form:"is_book"`
	Visibility     model.ArticleVisibility `form:"visibility"`
	DisableComment bool                    `form:"disable_comment"`
	BookRefer      string                  `form:"book_refer"`
	NewVersion     uint                    `form:"new_version"`
}

// Keep write and management handlers protected even if a route is moved out
// of the authenticated /admin group in the future.
func requirePublishRole(writer http.ResponseWriter, httpRequest *http.Request) error {
	account := currentAccount(writer, httpRequest)
	if account == nil || !account.Role.CanPublish() {
		return newHTTPError(http.StatusForbidden)
	}
	return nil
}

func publishHandler(writer http.ResponseWriter, httpRequest *http.Request) error {
	account := currentAccount(writer, httpRequest)
	if account == nil || !account.Role.CanPublish() {
		return newHTTPError(http.StatusForbidden)
	}
	var pa publishArticle
	if err := decodeRequestBody(httpRequest, &pa); err != nil {
		return fmt.Errorf("failed to parse publish form: %w", err)
	}
	if pa.Visibility == "" {
		pa.Visibility = model.VisibilityPublic
	}
	if !pa.Visibility.Valid() {
		return newHTTPError(http.StatusBadRequest, "invalid article visibility")
	}
	if err := content.Validate(pa.Content); err != nil {
		return newHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := validator.StructCtx(httpRequest.Context(), &pa); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	var bookRefer *string
	if pa.BookRefer != "" {
		bookRefer = &pa.BookRefer
	}
	// edit article
	newArticle := &model.Article{
		AuthorID:       &account.ID,
		UpdatedByID:    &account.ID,
		ID:             pa.ID,
		Title:          strings.TrimSpace(pa.Title),
		Slug:           strings.TrimSpace(pa.Slug),
		Content:        clearNonUTF8Chars(pa.Content),
		NewVersion:     pa.NewVersion,
		TemplateID:     pa.Template,
		IsBook:         pa.IsBook,
		Visibility:     pa.Visibility,
		DisableComment: pa.DisableComment,
		RawTags:        pa.Tags,
		BookRefer:      bookRefer,
		Version:        1,
	}

	if newArticle.IsTopic() {
		if len(newArticle.Slug) == 0 {
			newArticle.Slug = time.Now().Format("20060102150405")
		}
		if len(newArticle.Title) == 0 {
			newArticle.Title = newArticle.Slug
		}
	}

	// A valid URL segment is required before any lookup or write: otherwise a
	// reserved slug can publish an article hidden behind a system endpoint.
	if err := siteurl.ValidateArticleSlug(newArticle.Slug); err != nil {
		return newHTTPError(http.StatusBadRequest, err.Error())
	}

	if newArticle.ID == "" {
		var existed model.Article
		if err := solitudes.System.DB.Select("id, author_id").Order("created_at DESC").Take(&existed, "slug = ?", newArticle.Slug).Error; err == nil {
			if !mayEditArticle(account, &existed) {
				return newHTTPError(http.StatusForbidden)
			}
			newArticle.ID = existed.ID
			newArticle.CreatedAt = time.Now()
			newArticle.UpdatedAt = time.Now()
		}
	}

	originalArticle, err := fetchOriginArticle(newArticle)
	if err != nil {
		return fmt.Errorf("failed to fetch original article: %w", err)
	}
	if originalArticle.ID != "" {
		requestValue(httpRequest, "audit_target", originalArticle.ID)
		if !mayEditArticle(account, &originalArticle) {
			requestValue(httpRequest, "audit_reason", "article_owner_required")
			return newHTTPError(http.StatusForbidden)
		}
		newArticle.AuthorID = originalArticle.AuthorID
	}
	if !account.Role.IsAdmin() && !editorContentAllowed(originalArticle.Content, newArticle.Content) {
		requestValue(httpRequest, "audit_reason", "privileged_markdown_rejected")
		return newHTTPError(http.StatusForbidden, "only administrators may add or change executable Markdown content or raw HTML")
	}

	err = solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockRedirectRules(tx); err != nil {
			return err
		}
		if originalArticle.ID != "" {
			// Re-read under the same lock used by redirect management: concurrent
			// renames must register the actual previous slug, not a stale snapshot.
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&originalArticle, "id = ?", newArticle.ID).Error; err != nil {
				return err
			}
			if !mayEditArticle(account, &originalArticle) {
				return newHTTPError(http.StatusForbidden)
			}
			newArticle.Version = originalArticle.Version
			if pa.NewVersion == 1 {
				newArticle.Version++
			}
			newArticle.ReadNum, newArticle.CommentNum = originalArticle.ReadNum, originalArticle.CommentNum
		}
		if pa.NewVersion == 1 && originalArticle.ID != "" {
			history := model.ArticleHistory{
				EditorID:    &account.ID,
				UpdatedByID: originalArticle.UpdatedByID,
				UpdatedAt:   originalArticle.UpdatedAt,
				Title:       originalArticle.Title,
				Content:     originalArticle.Content,
				Version:     originalArticle.Version,
				ArticleID:   originalArticle.ID,
				CreatedAt:   originalArticle.CreatedAt,
			}
			if err := tx.Create(&history).Error; err != nil {
				return fmt.Errorf("failed to create article history: %w", err)
			}
		}

		if err := tx.Save(&newArticle).Error; err != nil {
			return fmt.Errorf("failed to save article: %w", err)
		}

		return syncArticleRedirects(tx, &originalArticle, newArticle)
	})

	if err != nil {
		return err
	}
	// indexing serch engine
	requestValue(httpRequest, "audit_target", newArticle.ID)
	numBefore, _ := solitudes.System.Search.DocCount()
	errIndex := solitudes.IndexArticle(newArticle)
	numAfter, _ := solitudes.System.Search.DocCount()
	log.Printf("Doc %s indexed %d --> %d %+v\n", newArticle.GetIndexID(), numBefore, numAfter, errIndex)

	return writeJSON(writer, http.StatusOK, newArticle)
}

// Editors may update ordinary Markdown, but cannot introduce raw HTML (including
// scripts, event handlers, SVG and embeds) or executable URL schemes. Keep the
// raw HTML of existing articles byte-for-byte so an editor can still edit prose
// around administrator-authored embeds without acquiring script privileges.
func editorContentAllowed(original, proposed string) bool {
	// Restricted content is executable Markdown for readers who can access it,
	// not inert code. Inspect its expanded AST with the same editor policy.
	allowAll := func(string) bool { return true }
	var err error
	original, err = content.Filter(original, allowAll, nil)
	if err != nil {
		return false
	}
	proposed, err = content.Filter(proposed, allowAll, nil)
	if err != nil {
		return false
	}
	oldRaw, oldUnsafe := markdownPrivilegedContent(original)
	newRaw, newUnsafe := markdownPrivilegedContent(proposed)
	return equalMarkdownTokens(oldRaw, newRaw) && equalMarkdownTokens(oldUnsafe, newUnsafe) &&
		equalMarkdownTokens(renderedExecutableElements(original), renderedExecutableElements(proposed))
}

func equalMarkdownTokens(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func markdownPrivilegedContent(content string) (rawHTML, unsafeURLs []string) {
	tree := parse.Parse("editor-permissions", []byte(content), luteEngine.ParseOptions)
	ast.Walk(tree.Root, func(n *ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.WalkContinue
		}
		switch n.Type {
		case ast.NodeHTMLBlock, ast.NodeInlineHTML:
			rawHTML = append(rawHTML, string(n.Tokens))
		case ast.NodeLinkDest:
			if !safeEditorMarkdownURL(string(n.Tokens)) {
				unsafeURLs = append(unsafeURLs, string(n.Tokens))
			}
		}
		return ast.WalkContinue
	})
	return rawHTML, unsafeURLs
}

// Lute's AST stores an inline <script> and </script> as two HTML nodes, while
// their JavaScript body is a separate text node. Comparing only HTML node
// tokens therefore misses edits to the executable body. Inspect the HTML Lute
// actually emits and preserve each active element including its full body.
// AST checks above still prevent editors from introducing new HTML tags or
// dangerous Markdown URL schemes.
func renderedExecutableElements(content string) (elements []string) {
	tokenizer := html.NewTokenizer(strings.NewReader(mdRender("editor-permissions", content)))
	var current strings.Builder
	depth := 0
	for {
		typeOfToken := tokenizer.Next()
		if typeOfToken == html.ErrorToken {
			break
		}
		token := tokenizer.Token()
		if typeOfToken == html.StartTagToken && executableHTMLElement(token.Data) {
			if depth == 0 {
				current.Reset()
			}
			depth++
		}
		if depth > 0 {
			current.Write(tokenizer.Raw())
		}
		if typeOfToken == html.EndTagToken && executableHTMLElement(token.Data) && depth > 0 {
			depth--
			if depth == 0 {
				elements = append(elements, current.String())
			}
		}
	}
	if depth > 0 {
		elements = append(elements, current.String())
	}
	return elements
}

func executableHTMLElement(tag string) bool {
	switch tag {
	case "script", "style", "template":
		return true
	default:
		return false
	}
}

func safeEditorMarkdownURL(raw string) bool {
	if strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\\\r\n\t") {
		return false
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "", "http", "https", "mailto", "tel":
		return true
	default:
		return false
	}
}

func fetchOriginArticle(af *model.Article) (model.Article, error) {
	if af.ID == "" {
		return model.Article{}, nil
	}
	var originArticle model.Article
	if err := solitudes.System.DB.Take(&originArticle, "id = ?", af.ID).Error; err != nil {
		return model.Article{}, err
	}

	af.CreatedAt = originArticle.CreatedAt
	af.CommentNum = originArticle.CommentNum
	af.ReadNum = originArticle.ReadNum

	if af.NewVersion == 1 {
		af.UpdatedAt = time.Now()
		af.Version = originArticle.Version + 1
	} else {
		af.UpdatedAt = time.Now()
		af.Version = originArticle.Version
	}

	return originArticle, nil
}

func mayEditArticle(account *model.Account, article *model.Article) bool {
	if account == nil || !account.Role.CanPublish() || article == nil || article.ID == "" {
		return false
	}
	return account.Role.IsAdmin() || article.AuthorID != nil && *article.AuthorID == account.ID
}

func clearNonUTF8Chars(s string) string {
	v := make([]rune, 0, len(s))
	for i, r := range s {
		// 清理非 UTF-8 字符
		if r == utf8.RuneError {
			_, size := utf8.DecodeRuneInString(s[i:])
			if size == 1 {
				continue
			}
		}
		// 清理 backspace
		if r == '\b' {
			continue
		}
		v = append(v, r)
	}
	return string(v)
}
