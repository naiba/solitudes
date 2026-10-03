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
	"github.com/gofiber/fiber/v2"
	"golang.org/x/net/html"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/content"
	"github.com/naiba/solitudes/pkg/pagination"
	"github.com/naiba/solitudes/pkg/translator"
)

func manageArticle(c *fiber.Ctx) error {
	if err := requirePublishRole(c); err != nil {
		return err
	}
	page, err := listPage(c.Query("page"))
	if err != nil {
		return err
	}
	var as []model.Article
	db := solitudes.System.DB
	if account := currentAccount(c); account != nil && !account.Role.IsAdmin() {
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
	if err := model.AggregateBookCounts(readableArticles(solitudes.System.DB, currentAccount(c)), as); err != nil {
		return err
	}
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	return c.Status(http.StatusOK).Render("admin/articles", injectSiteData(c, fiber.Map{
		"title":    tr.T("manage_articles"),
		"articles": as,
		"page":     pg,
	}))
}

func publish(c *fiber.Ctx) error {
	if err := requirePublishRole(c); err != nil {
		return err
	}
	id := c.Query("id")
	var article model.Article
	if id != "" {
		if err := solitudes.System.DB.Take(&article, "id = ?", id).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to fetch article for editing: %w", err)
		}
		if !mayEditArticle(currentAccount(c), &article) {
			return fiber.ErrNotFound
		}
	}
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	return c.Status(http.StatusOK).Render("admin/publish", injectSiteData(c, fiber.Map{
		"title":     tr.T("publish_article"),
		"templates": solitudes.Templates,
		"article":   article,
	}))
}

func deleteArticle(c *fiber.Ctx) error {
	if err := requirePublishRole(c); err != nil {
		return err
	}
	id := c.Query("id")
	if len(id) < 10 {
		return errors.New("invalid article id")
	}
	var a model.Article
	if err := solitudes.System.DB.Select("id", "author_id").Preload("ArticleHistories").Take(&a, "id = ?", id).Error; err != nil {
		return fmt.Errorf("failed to find article for deletion: %w", err)
	}
	if !mayEditArticle(currentAccount(c), &a) {
		return fiber.ErrNotFound
	}
	c.Locals("audit_target", a.ID)
	var indexIDs []string
	indexIDs = append(indexIDs, a.GetIndexID())
	err := solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
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
func requirePublishRole(c *fiber.Ctx) error {
	account := currentAccount(c)
	if account == nil || !account.Role.CanPublish() {
		return fiber.ErrForbidden
	}
	return nil
}

func publishHandler(c *fiber.Ctx) error {
	account := currentAccount(c)
	if account == nil || !account.Role.CanPublish() {
		return fiber.ErrForbidden
	}
	var pa publishArticle
	if err := c.BodyParser(&pa); err != nil {
		return fmt.Errorf("failed to parse publish form: %w", err)
	}
	if pa.Visibility == "" {
		pa.Visibility = model.VisibilityPublic
	}
	if !pa.Visibility.Valid() {
		return fiber.NewError(http.StatusBadRequest, "invalid article visibility")
	}
	if err := content.Validate(pa.Content); err != nil {
		return fiber.NewError(http.StatusBadRequest, err.Error())
	}
	if err := validator.StructCtx(c.Context(), &pa); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	var bookRefer *string
	if pa.BookRefer != "" {
		bookRefer = &pa.BookRefer
	}
	// edit article
	newArticle := &model.Article{
		AuthorID:       &account.ID,
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

	if newArticle.ID == "" {
		var existed model.Article
		if err := solitudes.System.DB.Select("id, author_id").Order("created_at DESC").Take(&existed, "slug = ?", newArticle.Slug).Error; err == nil {
			if !mayEditArticle(account, &existed) {
				return fiber.ErrForbidden
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
		c.Locals("audit_target", originalArticle.ID)
		if !mayEditArticle(account, &originalArticle) {
			c.Locals("audit_reason", "article_owner_required")
			return fiber.ErrForbidden
		}
		newArticle.AuthorID = originalArticle.AuthorID
	}
	if !account.Role.IsAdmin() && !editorContentAllowed(originalArticle.Content, newArticle.Content) {
		c.Locals("audit_reason", "privileged_markdown_rejected")
		return fiber.NewError(http.StatusForbidden, "only administrators may add or change executable Markdown content or raw HTML")
	}

	err = solitudes.System.DB.Transaction(func(tx *gorm.DB) error {
		if pa.NewVersion == 1 && originalArticle.ID != "" {
			history := model.ArticleHistory{
				EditorID:  &account.ID,
				Content:   originalArticle.Content,
				Version:   originalArticle.Version,
				ArticleID: originalArticle.ID,
				CreatedAt: originalArticle.CreatedAt,
			}
			if err := tx.Create(&history).Error; err != nil {
				return fmt.Errorf("failed to create article history: %w", err)
			}
		}

		if err := tx.Save(&newArticle).Error; err != nil {
			return fmt.Errorf("failed to save article: %w", err)
		}

		return nil
	})

	if err != nil {
		return err
	}
	// indexing serch engine
	c.Locals("audit_target", newArticle.ID)
	numBefore, _ := solitudes.System.Search.DocCount()
	errIndex := solitudes.IndexArticle(newArticle)
	numAfter, _ := solitudes.System.Search.DocCount()
	log.Printf("Doc %s indexed %d --> %d %+v\n", newArticle.GetIndexID(), numBefore, numAfter, errIndex)

	return c.Status(http.StatusOK).JSON(newArticle)
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
		af.UpdatedAt = originArticle.UpdatedAt
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
