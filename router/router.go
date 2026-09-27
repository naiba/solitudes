package router

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/88250/lute"
	"github.com/88250/lute/ast"
	luteHtml "github.com/88250/lute/html"
	luteRender "github.com/88250/lute/render"
	luteUtil "github.com/88250/lute/util"
	"github.com/go-playground/locales"
	gv "github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	html "github.com/gofiber/template/html/v2"
	"github.com/samber/lo"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/translator"
)

var luteEngine = lute.New()
var validator = gv.New()

func init() {
	luteEngine.SetCodeSyntaxHighlight(false)
	luteEngine.SetHeadingAnchor(true)
	luteEngine.SetHeadingID(true)
	luteEngine.SetSub(true)
	luteEngine.SetSup(true)
	luteEngine.SetAutoSpace(true)

	// 图片渲染：注入 loading="lazy" 属性
	luteEngine.Md2HTMLRendererFuncs[ast.NodeImage] = func(n *ast.Node, entering bool) (string, ast.WalkStatus) {
		if entering {
			dest := n.ChildByType(ast.NodeLinkDest)
			if dest == nil {
				return "", ast.WalkSkipChildren
			}
			attrs := [][]string{
				{"src", luteUtil.BytesToStr(luteHtml.EscapeHTML(dest.Tokens))},
				{"alt", n.Text()},
				{"loading", "lazy"},
			}
			if title := n.ChildByType(ast.NodeLinkTitle); nil != title && nil != title.Tokens {
				attrs = append(attrs, []string{"title", luteUtil.BytesToStr(luteHtml.EscapeHTML(title.Tokens))})
			}
			return renderTag("img", attrs, true), ast.WalkSkipChildren
		}
		return "", ast.WalkSkipChildren
	}

	// 标题渲染：为 heading anchor 注入 aria-label 属性
	luteEngine.Md2HTMLRendererFuncs[ast.NodeHeading] = func(n *ast.Node, entering bool) (string, ast.WalkStatus) {
		headingLevel := " 123456"
		level := headingLevel[n.HeadingLevel : n.HeadingLevel+1]
		if entering {
			id := luteRender.HeadingID(n)
			return "<h" + level + " id=\"" + id + "\">", ast.WalkContinue
		}
		id := luteRender.HeadingID(n)
		anchor := renderTag("a", [][]string{
			{"id", "vditorAnchor-" + id},
			{"class", "vditor-anchor"},
			{"href", "#" + id},
			{"aria-label", id},
		}, false)
		svg := `<svg viewBox="0 0 16 16" version="1.1" width="16" height="16" aria-hidden="true"><path fill-rule="evenodd" d="M4 9h1v1H4c-1.5 0-3-1.69-3-3.5S2.55 3 4 3h4c1.45 0 3 1.69 3 3.5 0 1.41-.91 2.72-2 3.25V8.59c.58-.45 1-1.27 1-2.09C10 5.22 8.98 4 8 4H4c-.98 0-2 1.22-2 2.5S3 9 4 9zm9-3h-1v1h1c1 0 2 1.22 2 2.5S13.98 12 13 12H9c-.98 0-2-1.22-2-2.5 0-.83.42-1.64 1-2.09V6.25c-1.09.53-2 1.84-2 3.25C6 11.31 7.55 13 9 13h4c1.45 0 3-1.69 3-3.5S14.5 6 13 6z"></path></svg>`
		return anchor + svg + "</a></h" + level + ">\n", ast.WalkContinue
	}

	luteEngine.Md2HTMLRendererFuncs[ast.NodeLink] = func(n *ast.Node, entering bool) (string, ast.WalkStatus) {
		if entering {
			dest := n.ChildByType(ast.NodeLinkDest)
			if dest == nil {
				return "", ast.WalkContinue
			}
			destStr := string(dest.Tokens)
			if isExternalLink(destStr) && !isVideoLink(destStr) {
				encodedURL := base64.URLEncoding.EncodeToString([]byte(destStr))
				attrs := [][]string{{"href", "/r/go?url=" + encodedURL}, {"target", "_blank"}, {"rel", "noopener noreferrer"}}
				if title := n.ChildByType(ast.NodeLinkTitle); nil != title && nil != title.Tokens {
					attrs = append(attrs, []string{"title", luteUtil.BytesToStr(luteHtml.EscapeHTML(title.Tokens))})
				}
				return renderTag("a", attrs, false), ast.WalkContinue
			}
			attrs := [][]string{{"href", luteUtil.BytesToStr(luteHtml.EscapeHTML(dest.Tokens))}}
			if title := n.ChildByType(ast.NodeLinkTitle); nil != title && nil != title.Tokens {
				attrs = append(attrs, []string{"title", luteUtil.BytesToStr(luteHtml.EscapeHTML(title.Tokens))})
			}
			return renderTag("a", attrs, false), ast.WalkContinue
		}
		return "</a>", ast.WalkContinue
	}
}

// themeResourcePath 返回主题资源的物理路径。
func themeResourcePath(kind, theme, subDir string) string {
	if theme == "" {
		if kind == "admin" {
			theme = "default"
		} else {
			theme = "cactus"
		}
	}
	return filepath.Join("resource", "themes", kind, theme, subDir)
}

// ThemeTemplateRoot returns the path to the templates for a given theme.
func ThemeTemplateRoot(kind, name string) string {
	return themeResourcePath(kind, name, "templates")
}

// ThemeStaticRoot constructs the path to the static assets for a given theme.
func ThemeStaticRoot(kind, name string) string {
	return themeResourcePath(kind, name, "static")
}

var themeNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

type tocTemplateData struct {
	Items  []*model.ArticleTOC
	Prefix string
}

func newTOCTemplateData(items []*model.ArticleTOC, prefix string) tocTemplateData {
	return tocTemplateData{Items: items, Prefix: prefix}
}

func tocNumberLabel(prefix string, index int) string {
	// Folio prints TOC numbers explicitly, so nested templates must carry the parent prefix instead of restarting at 1.
	return fmt.Sprintf("%s%d.", prefix, index+1)
}

// themeStaticHandler handles static file requests dynamically based on kind and theme
func themeStaticHandler(c *fiber.Ctx) error {
	kind := c.Params("kind")
	themeName := c.Params("theme")
	relativePath := c.Params("*")

	if (kind != "site" && kind != "admin") || !themeNamePattern.MatchString(themeName) {
		return page404(c)
	}

	// 拒绝目录列表请求（如 /static/site/cactus/ 或 /static/site/cactus/css/）
	if relativePath == "" || strings.HasSuffix(relativePath, "/") {
		return page404(c)
	}

	// URL wildcard 必须在主题静态目录内解析，不能再拼成宿主路径后 SendFile。
	staticRoot, err := os.OpenRoot(ThemeStaticRoot(kind, themeName))
	if err != nil {
		return page404(c)
	}
	defer staticRoot.Close()

	assetFile, err := staticRoot.Open(relativePath)
	if err != nil {
		return page404(c)
	}

	assetInfo, err := assetFile.Stat()
	if err != nil || assetInfo.IsDir() || assetInfo.Size() > int64(^uint(0)>>1) {
		assetFile.Close()
		return page404(c)
	}

	c.Set("Cache-Control", "public, max-age=2592000")
	c.Type(filepath.Ext(relativePath))
	return c.SendStream(assetFile, int(assetInfo.Size()))
}

// uploadStaticHandler serves only files inside data/upload, including when
// the directory contains an unexpected symlink.
func uploadStaticHandler(c *fiber.Ctx) error {
	relativePath := c.Params("*")
	if relativePath == "" || strings.HasSuffix(relativePath, "/") {
		return page404(c)
	}
	root, err := os.OpenRoot("data/upload")
	if err != nil {
		return page404(c)
	}
	defer root.Close()
	file, err := root.Open(relativePath)
	if err != nil {
		return page404(c)
	}
	info, err := file.Stat()
	if err != nil || info.IsDir() || info.Size() > int64(^uint(0)>>1) {
		file.Close()
		return page404(c)
	}
	c.Set("Cache-Control", "public, max-age=2592000")
	c.Type(filepath.Ext(relativePath))
	return c.SendStream(file, int(info.Size()))
}

// isExternalLink 判断是否为外部链接
// isAdminPath 判断请求是否为后台路径
func isAdminPath(path string) bool {
	return strings.HasPrefix(path, "/admin/")
}

var videoHostPatterns = []string{
	"youtube.com",
	"youtu.be",
	"bilibili.com",
	"v.youku.com",
	"v.qq.com",
	"coub.com",
	"facebook.com/*/videos/",
	"dailymotion.com",
	"ted.com/talks/",
}

func isVideoLink(urlStr string) bool {
	lowerURL := strings.ToLower(urlStr)
	for _, pattern := range videoHostPatterns {
		if strings.Contains(lowerURL, pattern) {
			return true
		}
	}
	return false
}

func isExternalLink(urlStr string) bool {
	if strings.HasPrefix(urlStr, "http://") || strings.HasPrefix(urlStr, "https://") {
		parsed, err := url.Parse(urlStr)
		if err != nil {
			return false
		}
		if solitudes.System != nil && solitudes.System.Config.Site.Domain != "" {
			siteHost := solitudes.System.Config.Site.Domain
			linkHost := parsed.Host
			if idx := strings.Index(linkHost, ":"); idx != -1 {
				linkHost = linkHost[:idx]
			}
			if idx := strings.Index(siteHost, ":"); idx != -1 {
				siteHost = siteHost[:idx]
			}
			return linkHost != siteHost
		}
		return true
	}
	return false
}

// renderTag 生成 HTML 标签
func renderTag(name string, attrs [][]string, selfClosing bool) string {
	var sb strings.Builder
	sb.WriteString("<")
	sb.WriteString(name)
	for _, attr := range attrs {
		sb.WriteString(" ")
		sb.WriteString(attr[0])
		sb.WriteString("=\"")
		sb.WriteString(attr[1])
		sb.WriteString("\"")
	}
	if selfClosing {
		sb.WriteString(" /")
	}
	sb.WriteString(">")
	return sb.String()
}

func mdRender(id string, raw string) string {
	return luteEngine.MarkdownStr(id, raw)
}

var mdCleanRegex = regexp.MustCompile(`(?m)^#{1,6}\s+.*$`)
var mdLinkRegex = regexp.MustCompile(`\[([^\]]*)\]\([^)]+\)`)  // [text](url) → 保留 text
var mdBareURLRegex = regexp.MustCompile(`https?://[^\s)\]>]+`) // 裸 URL → 移除，避免预览中出现不可点击的长链接
var mdSymbolRegex = regexp.MustCompile(`[#*_~\[\]()` + "`" + `>!|{}\-]`)
var mdImageRegex = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
var htmlImageRegex = regexp.MustCompile(`<img\s[^>]*src=["']([^"']+)["']`)

func mdExcerpt(content string, maxLen int) string {
	text := mdCleanRegex.ReplaceAllString(content, "")
	text = mdImageRegex.ReplaceAllString(text, "")   // ![alt](url) → 移除整个图片引用
	text = mdLinkRegex.ReplaceAllString(text, "$1")  // [text](url) → 保留 text
	text = mdBareURLRegex.ReplaceAllString(text, "") // 裸 URL → 移除
	text = mdSymbolRegex.ReplaceAllString(text, "")
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "…"
	}
	return text
}

func mdFirstImage(content string) string {
	match := mdImageRegex.FindStringSubmatch(content)
	if len(match) >= 3 {
		return strings.TrimSpace(match[2])
	}
	// fallback: 提取 raw HTML <img src="...">
	htmlMatch := htmlImageRegex.FindStringSubmatch(content)
	if len(htmlMatch) >= 2 {
		return strings.TrimSpace(htmlMatch[1])
	}
	return ""
}

// DynamicEngine wraps the actual html engine to allow hot-reloading
type DynamicEngine struct {
	engine *html.Engine
}

func (d *DynamicEngine) Load() error {
	if d.engine == nil {
		return nil
	}
	return d.engine.Load()
}

func (d *DynamicEngine) Render(out io.Writer, template string, binding interface{}, layout ...string) error {
	if d.engine == nil {
		return fmt.Errorf("template engine not initialized")
	}
	return d.engine.Render(out, template, binding, layout...)
}

var globalDynamicEngine = &DynamicEngine{}

// LoadTemplates initializes or reloads the template engine with current theme configurations
func LoadTemplates() error {
	siteTheme := solitudes.System.Config.Site.Theme
	adminTheme := solitudes.System.Config.Admin.Theme
	siteTemplateRoot := ThemeTemplateRoot("site", siteTheme)
	adminTemplateRoot := ThemeTemplateRoot("admin", adminTheme)

	// 重载翻译
	translator.Reload(siteTheme, adminTheme)

	// 使用 afero 创建带前缀的合并文件系统
	// site/* -> siteTemplateRoot/*, admin/* -> adminTemplateRoot/*
	baseFs := afero.NewMemMapFs()
	osFs := afero.NewOsFs()

	// 将 site 模板挂载到 site/ 前缀下
	siteFs := afero.NewBasePathFs(osFs, siteTemplateRoot)
	afero.Walk(siteFs, "", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		content, _ := afero.ReadFile(siteFs, path)
		afero.WriteFile(baseFs, "site/"+path, content, info.Mode())
		return nil
	})

	// 将 admin 模板挂载到 admin/ 前缀下
	adminFs := afero.NewBasePathFs(osFs, adminTemplateRoot)
	afero.Walk(adminFs, "", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		content, _ := afero.ReadFile(adminFs, path)
		afero.WriteFile(baseFs, "admin/"+path, content, info.Mode())
		return nil
	})

	newEngine := html.NewFileSystem(http.FS(afero.NewIOFS(baseFs)), ".html")
	setFuncMap(newEngine)

	// 加载模板
	if err := newEngine.Load(); err != nil {
		return fmt.Errorf("failed to load templates: %w", err)
	}

	if solitudes.System.Config.Debug {
		newEngine.Reload(true)
		newEngine.Debug(true)
	}

	globalDynamicEngine.engine = newEngine
	log.Printf("Templates loaded from site=%s, admin=%s", siteTemplateRoot, adminTemplateRoot)
	return nil
}

// ReloadTemplates reloads the template engine (for theme switching)
func ReloadTemplates() error {
	return LoadTemplates()
}

// newApp builds the HTTP application so browser tests can serve the real
// routes against an isolated database without modifying the production config.
func newApp() *fiber.App {
	return newAppWithRoutes(nil)
}

func newAppWithRoutes(extraRoutes func(*fiber.App)) *fiber.App {
	// 加载模板
	if err := LoadTemplates(); err != nil {
		log.Printf("Warning: Failed to load templates: %v", err)
	}
	activeOIDCProvider = nil
	if provider, err := newOIDCProvider(); err != nil {
		log.Printf("OIDC provider unavailable: %v", err)
	} else {
		activeOIDCProvider = provider
	}

	dbErrors := []error{
		gorm.ErrInvalidTransaction,
	}
	app := fiber.New(fiber.Config{
		EnableTrustedProxyCheck: solitudes.System.Config.EnableTrustedProxyCheck,
		TrustedProxies:          solitudes.System.Config.TrustedProxies,
		ProxyHeader:             solitudes.System.Config.ProxyHeader,
		Views:                   globalDynamicEngine,
		ErrorHandler: func(c *fiber.Ctx, e error) error {
			// 404 页面
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return page404(c)
			}
			title := "Unknown error"
			errMsg := e.Error()
			status := http.StatusInternalServerError
			var fiberError *fiber.Error
			if errors.As(e, &fiberError) {
				status = fiberError.Code
			}
			if lo.ContainsBy(dbErrors, func(item error) bool {
				return errors.Is(e, item)
			}) {
				title = "DB error"
				errMsg = "Please contact the webmaster"
			}
			if strings.Contains(string(c.Request().Header.Peek("Accept")), "html") {
				templateName := "site/error"
				if isAdminPath(c.Path()) {
					templateName = "admin/error"
				}
				return c.Status(status).Render(templateName, injectSiteData(c, fiber.Map{
					"title":   title,
					"msg":     errMsg,
					"noindex": true,
				}))
			}
			_, e = c.Status(status).WriteString(errMsg)
			return e
		},
	})

	app.Use(func(c *fiber.Ctx) error {
		p := c.Path()
		if len(p) > 1 {
			hasSlash := p[len(p)-1] == '/'
			trimmed := strings.TrimRight(p, "/")
			isList := trimmed == "/posts" || trimmed == "/books" || trimmed == "/tags" || trimmed == "/search" ||
				strings.HasPrefix(trimmed, "/tags/") || strings.HasPrefix(trimmed, "/posts/") || strings.HasPrefix(trimmed, "/books/")
			needRedirect := (isList && !hasSlash) || (!isList && hasSlash)
			if needRedirect {
				q := string(c.Request().URI().QueryString())
				target := trimmed
				if isList {
					target += "/"
				}
				if q != "" {
					target += "?" + q
				}
				return c.Redirect(target, http.StatusMovedPermanently)
			}
		}
		c.Set("X-Frame-Options", "SAMEORIGIN")
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Set("Content-Security-Policy", "frame-ancestors 'self'")
		isHTML := strings.Contains(c.Get("Accept"), "html")
		if isHTML {
			c.Set("Cache-Control", "public, max-age=60, stale-while-revalidate=300")
		}
		err := c.Next()
		if isHTML {
			if authorized, ok := c.Locals(solitudes.CtxAuthorized).(bool); ok && authorized {
				c.Set("Cache-Control", "private, no-store")
			}
		}
		return err
	})
	// Protocol endpoints authenticate clients with OAuth credentials and must
	// not be subject to browser Origin/Referer CSRF checks.
	if activeOIDCProvider != nil {
		protocolHandler := oidcHTTPHandler(activeOIDCProvider)
		for _, path := range []string{"/.well-known", "/authorize", "/oauth", "/userinfo", "/revoke", "/end_session", "/keys", "/healthz", "/ready"} {
			app.Use(path, protocolHandler)
		}
	}
	app.Use(trans, auth, csrfGuard)
	app.Get("/", index)
	app.Get("/favicon.ico", faviconHandler)
	app.Get("/logo.png", logoHandler)
	app.Get("/feed/:format?", feedHandler)
	app.Get("/posts/:page?/", posts)
	app.Get("/books/:page?/", book)
	app.Get("/search/", search)
	app.Get("/tags/:tag/:page?/", tags)
	app.Get("/tags/", tagsCloud)
	app.Get("/r/go", goRedirect)
	app.Get("/robots.txt", robotsHandler)
	app.Get("/sitemap.xml", sitemapHandler)
	app.Post("/logout", requireAccount, logoutHandler)
	app.Get("/captcha", generateCaptcha)
	app.Post("/api/comment", commentHandler)
	app.Post("/api/count", count)
	app.Get("/api/commenter-info", commenterInfoHandler)

	// Email tracking endpoints: redirect (primary) + pixel (backup), both use token lookup
	app.Get("/r/:token", trackEmailReadRedirect)
	app.Get("/static/i/:token", trackEmailRead)
	app.Get("/static/:kind/:theme/*", themeStaticHandler)
	app.Get("/upload/*", uploadStaticHandler)

	app.Get("/admin/login", guestRequired, login)
	app.Post("/admin/login", guestRequired, loginHandler)
	app.Get("/admin/register", guestRequired, registerPage)
	app.Post("/admin/register", guestRequired, registerHandler)
	app.Post("/admin/resend-verification", resendVerification)
	app.Get("/admin/verify-email", verifyEmailHandler)
	app.Get("/account", requireAccount, accountPage)
	app.Post("/account/password", requireAccount, changeAccountPassword)
	app.Get("/oidc/consent", consentPage)
	app.Post("/oidc/consent", requireAccount, consentHandler)
	app.Get("/auth/:provider/callback", oauthCallback)
	app.Post("/auth/:provider/link", requireAccount, beginOAuthLink)
	app.Get("/auth/:provider", guestRequired, beginOAuthLogin)
	app.Post("/auth/passkey/login/begin", beginPasskeyLogin)
	app.Post("/auth/passkey/login/finish", finishPasskeyLogin)
	app.Post("/account/passkeys/begin", requireAccount, beginPasskeyRegistration)
	app.Post("/account/passkeys/finish", requireAccount, finishPasskeyRegistration)
	app.Delete("/account/passkeys/:id", requireAccount, deletePasskey)

	admin := app.Group("/admin/", loginRequired)
	admin.Get("/", requireAdmin, manager)
	admin.Get("/users", requireAdmin, usersPage)
	admin.Post("/users/:id/role", requireAdmin, setUserRole)
	admin.Get("/oidc/clients", requireAdmin, oidcClientsPage)
	admin.Post("/oidc/clients", requireAdmin, createOIDCClient)
	admin.Post("/oidc/clients/:id/disable", requireAdmin, disableOIDCClient)
	admin.Post("/oidc/keys/rotate", requireAdmin, rotateOIDCKeys)
	admin.Get("/publish", publish)
	admin.Post("/publish", publishHandler)
	admin.Post("/rebuild-full-text-search", requireAdmin, rebuildFullTextSearch)
	admin.Post("/upload", upload)
	admin.Post("/fetch", requireAdmin, fetch)
	admin.Get("/comments", requireAdmin, comments)
	admin.Delete("/comments", requireAdmin, deleteComment)
	admin.Post("/report-spam", requireAdmin, reportSpam)
	admin.Post("/restore-spam", requireAdmin, restoreSpam)
	admin.Get("/articles", manageArticle)
	admin.Delete("/articles", deleteArticle)
	admin.Get("/media", requireAdmin, media)
	admin.Delete("/media", requireAdmin, mediaHandler)
	admin.Get("/settings", requireAdmin, settings)
	admin.Post("/settings", requireAdmin, settingsHandler)
	admin.Get("/tags", requireAdmin, tagsManagePage)
	admin.Delete("/tags", requireAdmin, deleteTag)
	admin.Patch("/tags", requireAdmin, renameTag)
	admin.Get("/api/search-tags", searchTags)
	admin.Get("/api/search-books", searchBooks)
	admin.Get("/theme/preview/:kind/:name", themePreview)

	if extraRoutes != nil {
		extraRoutes(app)
	}
	app.Get("/:slug/:version?", article)
	app.Use(page404)

	if solitudes.System.Config.Debug {
		app.Use(logger.New())
	}

	return app
}

// Serve web service
func Serve() {
	if err := newApp().Listen(":8080"); err != nil {
		log.Printf("web server stopped: %v", err)
	}
}

func themePreview(c *fiber.Ctx) error {
	kind := c.Params("kind")
	name := c.Params("name")
	if (kind != "site" && kind != "admin") || !themeNamePattern.MatchString(name) {
		return c.SendStatus(http.StatusNotFound)
	}
	root, err := os.OpenRoot(themeResourcePath(kind, name, ""))
	if err != nil {
		return c.SendStatus(http.StatusNotFound)
	}
	defer root.Close()
	file, err := root.Open("screenshot.png")
	if err != nil {
		return c.SendStatus(http.StatusNotFound)
	}
	info, err := file.Stat()
	if err != nil || info.IsDir() || info.Size() > int64(^uint(0)>>1) {
		file.Close()
		return c.SendStatus(http.StatusNotFound)
	}
	c.Type("png")
	return c.SendStream(file, int(info.Size()))
}

func serveUploadOrThemeFile(c *fiber.Ctx, uploadPath, themePath string) error {
	if err := sendRootFile(c, "data/upload", filepath.Base(uploadPath)); err == nil {
		return nil
	}
	return sendRootFile(c, ThemeStaticRoot("site", solitudes.System.Config.Site.Theme), themePath)
}

func sendRootFile(c *fiber.Ctx, rootPath, relativePath string) error {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return fiber.ErrNotFound
	}
	defer root.Close()
	file, err := root.Open(relativePath)
	if err != nil {
		return fiber.ErrNotFound
	}
	info, err := file.Stat()
	if err != nil || info.IsDir() || info.Size() > int64(^uint(0)>>1) {
		file.Close()
		return fiber.ErrNotFound
	}
	c.Type(filepath.Ext(relativePath))
	return c.SendStream(file, int(info.Size()))
}

func faviconHandler(c *fiber.Ctx) error {
	return serveUploadOrThemeFile(c, "data/upload/favicon.ico", "images/favicon.ico")
}

func logoHandler(c *fiber.Ctx) error {
	return serveUploadOrThemeFile(c, "data/upload/logo.png", "images/logo.png")
}

// goRedirect 处理外部链接跳转
func goRedirect(c *fiber.Ctx) error {
	encodedURL := c.Query("url")
	if encodedURL == "" {
		return c.Status(http.StatusBadRequest).SendString("Missing url parameter")
	}

	// 显示跳转提示页面，实际解析由前端 JS 完成
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	return c.Render("site/redirect", injectSiteData(c, fiber.Map{
		"title":             tr.T("redirect_title"),
		"msg":               tr.T("redirect_msg"),
		"continue_text":     tr.T("redirect_continue"),
		"auto_redirect":     tr.T("redirect_auto"),
		"seconds":           tr.T("redirect_seconds"),
		"error_no_url":      tr.T("redirect_error_no_url"),
		"error_invalid_url": tr.T("redirect_error_invalid_url"),
		"noindex":           true,
	}))
}

func page404(c *fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	templateName := "site/error"
	if isAdminPath(c.Path()) {
		templateName = "admin/error"
	}
	c.Status(http.StatusNotFound).Render(templateName, injectSiteData(c, fiber.Map{
		"title":   tr.T("404_title"),
		"msg":     tr.T("404_msg"),
		"noindex": true,
	}))
	return nil
}

func robotsHandler(c *fiber.Ctx) error {
	domain := solitudes.System.Config.Site.Domain
	robotsTxt := fmt.Sprintf(`User-agent: *
Allow: /
Disallow: /admin/
Disallow: /r/
Disallow: /feed/
Disallow: /api/
Disallow: /captcha

Sitemap: https://%s/sitemap.xml
`, domain)
	c.Set("Content-Type", "text/plain")
	c.Status(http.StatusOK).SendString(robotsTxt)
	return nil
}

func sitemapHandler(c *fiber.Ctx) error {
	domain := solitudes.System.Config.Site.Domain
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
`)
	sb.WriteString(fmt.Sprintf(`  <url>
    <loc>https://%s/</loc>
    <changefreq>daily</changefreq>
    <priority>1.0</priority>
  </url>
`, domain))
	var articles []model.Article
	if err := solitudes.System.DB.Order("created_at DESC").Find(&articles).Error; err != nil {
		return fmt.Errorf("failed to fetch articles for sitemap: %w", err)
	}
	for _, article := range articles {
		if article.IsPrivate {
			continue
		}
		sb.WriteString(fmt.Sprintf(`  <url>
    <loc>https://%s/%s</loc>
    <lastmod>%s</lastmod>
    <changefreq>monthly</changefreq>
    <priority>0.8</priority>
  </url>
`, domain, article.Slug, article.UpdatedAt.Format("2006-01-02")))
	}
	sb.WriteString(fmt.Sprintf(`  <url>
    <loc>https://%s/posts/</loc>
    <changefreq>daily</changefreq>
    <priority>0.6</priority>
  </url>
  <url>
    <loc>https://%s/books/</loc>
    <changefreq>weekly</changefreq>
    <priority>0.6</priority>
  </url>
  <url>
    <loc>https://%s/tags/</loc>
    <changefreq>weekly</changefreq>
    <priority>0.5</priority>
  </url>
`, domain, domain, domain))
	var tags []string
	if err := solitudes.System.DB.Raw(`SELECT DISTINCT t FROM articles, unnest(articles.tags) AS t WHERE t IS NOT NULL`).Scan(&tags).Error; err != nil {
		return fmt.Errorf("failed to fetch tags for sitemap: %w", err)
	}
	for _, tag := range tags {
		sb.WriteString(fmt.Sprintf(`  <url>
    <loc>https://%s/tags/%s/</loc>
    <changefreq>weekly</changefreq>
    <priority>0.5</priority>
  </url>
`, domain, url.QueryEscape(tag)))
	}
	sb.WriteString(`</urlset>`)
	c.Set("Content-Type", "application/xml")
	return c.Status(http.StatusOK).SendString(sb.String())
}

func setFuncMap(engine *html.Engine) {
	funcMap := template.FuncMap{
		"md5": func(origin string) string {
			hasher := md5.New()
			hasher.Write([]byte(origin))
			return hex.EncodeToString(hasher.Sum(nil))
		},
		"add": func(a, b int) int {
			return a + b
		},
		"tocTemplateData": newTOCTemplateData,
		"tocNumberLabel":  tocNumberLabel,
		"uint2str": func(i uint) string {
			return fmt.Sprintf("%d", i)
		},
		"int2str": func(i int) string {
			return fmt.Sprintf("%d", i)
		},
		"json": func(x interface{}) string {
			b, _ := json.Marshal(x)
			return string(b)
		},
		"yaml": func(x interface{}) string {
			b, _ := yaml.Marshal(x)
			return string(b)
		},
		"unsafe": func(raw string) template.HTML {
			return template.HTML(raw)
		},
		"tf": func(t time.Time, f string) string {
			return t.Format(f)
		},
		"iso8601": func(t time.Time) string {
			return t.Format(time.RFC3339)
		},
		"md": mdRender,
		"articleIdx": func(t model.Article) string {
			return t.GetIndexID()
		},
		"oldVersions": func(latestVersion uint, slug string) string {
			var sb strings.Builder
			for i := latestVersion - 1; i > 0; i-- {
				sb.WriteString(fmt.Sprintf(`<a href="/%s/v%d">v%d</a>`, slug, i, i))
				if i > 1 {
					sb.WriteString(", ")
				}
			}
			return sb.String()
		},
		"last": func(x int, a interface{}) bool {
			return x == reflect.ValueOf(a).Len()-1
		},
		"trim": strings.TrimSpace,
		"ptrStrEq": func(ptr *string, val string) bool {
			if ptr == nil {
				return false
			}
			return *ptr == val
		},
		"articleData": func(article *model.Article, tr *translator.Translator) fiber.Map {
			return fiber.Map{
				"article": article,
				"tr":      tr,
				"Conf":    solitudes.System.Config,
			}
		},
		"commentsData": func(comments []*model.Comment, tr *translator.Translator) fiber.Map {
			return fiber.Map{
				"comments": comments,
				"tr":       tr,
				"Conf":     solitudes.System.Config,
			}
		},
		"substr": func(v interface{}, start, length int) string {
			var s string
			switch t := v.(type) {
			case string:
				s = t
			case template.HTML:
				s = string(t)
			default:
				s = fmt.Sprint(v)
			}
			runes := []rune(s)
			l := len(runes)
			if l == 0 || start < 0 || start >= l {
				return ""
			}
			end := start + length
			if end > l {
				end = l
			}
			if start >= end {
				return ""
			}
			return string(runes[start:end])
		},
		"mdExcerpt": mdExcerpt,
		"hasPrefix": strings.HasPrefix,
		"urlencode": func(s string) string {
			return url.QueryEscape(s)
		},
		"firstImage": func(content string, fallback string) string {
			if img := mdFirstImage(content); img != "" {
				return img
			}
			return fallback
		},
		"jsonEscape": func(s string) string {
			b, _ := json.Marshal(s)
			// json.Marshal returns "quoted string", strip outer quotes
			return string(b[1 : len(b)-1])
		},
		"externalLink": func(urlStr string) string {
			// 将外部链接转换为 /r/go?url=base64 格式
			if urlStr == "" {
				return ""
			}
			encoded := base64.URLEncoding.EncodeToString([]byte(urlStr))
			return "/r/go?url=" + encoded
		},
	}
	for name, fn := range funcMap {
		engine.AddFunc(name, fn)
	}
}

func auth(c *fiber.Ctx) error {
	token := c.Cookies(solitudes.AuthCookie)
	account, err := sessionLookup(token)
	if err == nil && account != nil {
		c.Locals(solitudes.CtxAccount, account)
		c.Locals(solitudes.CtxAuthorized, true)
	} else {
		c.Locals(solitudes.CtxAuthorized, false)
	}
	return c.Next()
}

func loginRequired(c *fiber.Ctx) error {
	account := currentAccount(c)
	if account == nil {
		c.Redirect("/admin/login", http.StatusFound)
		return nil
	}
	if !account.Role.CanPublish() {
		return fiber.ErrForbidden
	}
	return c.Next()
}

func csrfSameOriginHeader(c *fiber.Ctx, header string) bool {
	raw := c.Get(header)
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return strings.EqualFold(u.Host, c.Get(fiber.HeaderHost)) && strings.EqualFold(u.Scheme, c.Protocol())
}

func csrfGuard(c *fiber.Ctx) error {
	switch c.Method() {
	case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
		return c.Next()
	}
	origin := c.Get(fiber.HeaderOrigin)
	referer := c.Get(fiber.HeaderReferer)
	if origin == "" && referer == "" {
		return fiber.NewError(fiber.StatusForbidden, "missing Origin and Referer")
	}
	if origin != "" && !csrfSameOriginHeader(c, fiber.HeaderOrigin) {
		return fiber.NewError(fiber.StatusForbidden, "cross-origin request blocked")
	}
	if origin == "" && !csrfSameOriginHeader(c, fiber.HeaderReferer) {
		return fiber.NewError(fiber.StatusForbidden, "cross-origin request blocked")
	}
	return c.Next()
}

func guestRequired(c *fiber.Ctx) error {
	if account := currentAccount(c); account != nil {
		if account.Role.CanPublish() {
			return c.Redirect("/admin", http.StatusFound)
		}
		return c.Redirect("/account", http.StatusFound)
	}
	return c.Next()
}

func injectSiteData(c *fiber.Ctx, data fiber.Map) fiber.Map {
	var title, keywords, desc string
	siteName := solitudes.System.Config.Site.SpaceName
	siteDesc := solitudes.System.Config.Site.SpaceDesc

	if k, ok := data["title"]; ok && k.(string) != "" {
		title = data["title"].(string) + " - " + siteName
		if len([]rune(title)) < 30 && siteDesc != siteName {
			title = title + " | " + siteDesc
		}
		if len([]rune(title)) < 30 {
			title = title + " | " + solitudes.System.Config.Site.SpaceKeywords
		}
	} else {
		if siteDesc != "" && siteDesc != siteName {
			title = siteName + " - " + siteDesc
		} else {
			title = siteName
		}
		if len([]rune(title)) < 30 {
			title = title + " | " + solitudes.System.Config.Site.SpaceKeywords
		}
	}

	if k, ok := data["keywords"]; ok && k.(string) != "" {
		keywords = data["keywords"].(string)
	} else {
		keywords = solitudes.System.Config.Site.SpaceKeywords
	}

	if k, ok := data["desc"]; ok && k.(string) != "" {
		desc = data["desc"].(string)
	} else {
		desc = siteDesc
	}
	if len([]rune(desc)) < 60 {
		desc = desc + " - " + siteName + " | " + solitudes.System.Config.Site.SpaceKeywords
	}

	ogType := "website"
	if k, ok := data["og_type"]; ok && k.(string) != "" {
		ogType = k.(string)
	}

	noindex := false
	if k, ok := data["noindex"]; ok {
		noindex = k.(bool)
	}

	var soli = make(map[string]interface{})
	soli["Conf"] = solitudes.System.Config
	soli["Theme"] = solitudes.System.Config.Site.ThemeConfig
	soli["Title"] = title
	soli["Keywords"] = keywords
	soli["BuildVersion"] = solitudes.BuildVersion
	soli["Desc"] = desc
	soli["OgType"] = ogType
	soli["Noindex"] = noindex
	account := currentAccount(c)
	soli["Account"] = account
	soli["Login"] = account != nil && account.Role.CanPublish()
	soli["Data"] = data
	soli["Tr"] = c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	soli["Path"] = c.Path()
	soli["Now"] = time.Now()

	return soli
}

func trans(c *fiber.Ctx) error {
	t, _ := translator.Trans.FindTranslator(getAcceptLanguages(c.Get("Accept-Language"))...)
	c.Locals(solitudes.CtxTranslator, &translator.Translator{Trans: t, Translator: t.(locales.Translator)})
	return c.Next()
}

func getAcceptLanguages(accepted string) []string {
	if accepted == "" {
		return []string{}
	}

	options := strings.Split(accepted, ",")
	l := len(options)

	languages := make([]string, l)

	for i := 0; i < l; i++ {
		locale := strings.SplitN(options[i], ";", 2)
		languages[i] = strings.Trim(locale[0], " ")
	}

	if lo.ContainsBy(languages, func(item string) bool {
		return strings.HasPrefix(item, "zh")
	}) {
		return []string{"zh", "en"}
	}

	return []string{"en", "zh"}
}
