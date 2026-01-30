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
	"strings"
	"time"

	"github.com/88250/lute"
	"github.com/88250/lute/ast"
	luteHtml "github.com/88250/lute/html"
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

// themeStaticHandler handles static file requests dynamically based on kind and theme
func themeStaticHandler(c *fiber.Ctx) error {
	kind := c.Params("kind")
	themeName := c.Params("theme")
	relativePath := c.Params("*")

	if kind != "site" && kind != "admin" {
		return page404(c)
	}

	// 拒绝目录列表请求（如 /static/site/cactus/ 或 /static/site/cactus/css/）
	if relativePath == "" || strings.HasSuffix(relativePath, "/") {
		return page404(c)
	}

	themeStaticPath := ThemeStaticRoot(kind, themeName)
	fullPath := filepath.Join(themeStaticPath, relativePath)

	cleanPath := filepath.Clean(fullPath)
	if _, err := os.Stat(cleanPath); err != nil {
		return page404(c)
	}

	return c.SendFile(cleanPath)
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

// Serve web service
func Serve() {
	// 加载模板
	if err := LoadTemplates(); err != nil {
		log.Printf("Warning: Failed to load templates: %v", err)
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
			if e == gorm.ErrRecordNotFound {
				return page404(c)
			}
			title := "Unknown error"
			errMsg := e.Error()
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
				return c.Status(http.StatusInternalServerError).Render(templateName, injectSiteData(c, fiber.Map{
					"title": title,
					"msg":   errMsg,
				}))
			}
			_, e = c.Status(http.StatusInternalServerError).WriteString(errMsg)
			return e
		},
	})

	app.Use(trans, auth)
	app.Get("/", index)
	app.Get("/favicon.ico", faviconHandler)
	app.Get("/logo.png", logoHandler)
	app.Get("/feed/:format?", feedHandler)
	app.Get("/posts/:page?", posts)
	app.Get("/books/:page?", book)
	app.Get("/search/", search)
	app.Get("/tags/:tag/:page?", tags)
	app.Get("/tags/", tagsCloud)
	app.Get("/r/go", goRedirect)
	app.Get("/robots.txt", robotsHandler)
	app.Get("/sitemap.xml", sitemapHandler)
	app.Post("/logout", loginRequired, logoutHandler)
	app.Get("/captcha", generateCaptcha)
	app.Post("/api/comment", commentHandler)
	app.Post("/api/count", count)
	app.Get("/api/commenter-info", commenterInfoHandler)

	// Email tracking endpoints: redirect (primary) + pixel (backup), both use token lookup
	app.Get("/r/:token", trackEmailReadRedirect)
	app.Get("/static/i/:token", trackEmailRead)
	app.Get("/static/:kind/:theme/*", themeStaticHandler)
	app.Static("/upload", "data/upload")

	app.Get("/admin/login", guestRequired, login)
	app.Post("/admin/login", guestRequired, loginHandler)

	admin := app.Group("/admin/", loginRequired)
	admin.Get("/", manager)
	admin.Get("/publish", publish)
	admin.Post("/publish", publishHandler)
	admin.Get("/rebuild-full-text-search", rebuildFullTextSearch)
	admin.Post("/upload", upload)
	admin.Post("/fetch", fetch)
	admin.Get("/comments", comments)
	admin.Delete("/comments", deleteComment)
	admin.Post("/report-spam", reportSpam)
	admin.Get("/articles", manageArticle)
	admin.Delete("/articles", deleteArticle)
	admin.Get("/media", media)
	admin.Delete("/media", mediaHandler)
	admin.Get("/settings", settings)
	admin.Post("/settings", settingsHandler)
	admin.Get("/tags", tagsManagePage)
	admin.Delete("/tags", deleteTag)
	admin.Patch("/tags", renameTag)
	admin.Get("/api/search-tags", searchTags)
	admin.Get("/api/search-books", searchBooks)
	admin.Get("/theme/preview/:kind/:name", themePreview)

	app.Get("/:slug/:version?", article)
	app.Use(page404)

	if solitudes.System.Config.Debug {
		app.Use(logger.New())
	}

	app.Listen(":8080")
}

func themePreview(c *fiber.Ctx) error {
	kind := c.Params("kind")
	name := c.Params("name")
	if kind != "site" && kind != "admin" {
		return c.SendStatus(http.StatusNotFound)
	}
	path := fmt.Sprintf("resource/themes/%s/%s/screenshot.png", kind, name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return c.SendStatus(http.StatusNotFound)
	}
	return c.SendFile(filepath.Clean(path))
}

func serveUploadOrThemeFile(c *fiber.Ctx, uploadPath, themePath string) error {
	if _, err := os.Stat(uploadPath); err == nil {
		return c.SendFile(uploadPath)
	}
	fullThemePath := filepath.Join(ThemeStaticRoot("site", solitudes.System.Config.Site.Theme), themePath)
	return c.SendFile(filepath.Clean(fullThemePath))
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
	}))
}

func page404(c *fiber.Ctx) error {
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	templateName := "site/error"
	if isAdminPath(c.Path()) {
		templateName = "admin/error"
	}
	c.Status(http.StatusNotFound).Render(templateName, injectSiteData(c, fiber.Map{
		"title": tr.T("404_title"),
		"msg":   tr.T("404_msg"),
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
    <loc>https://%s/%s/</loc>
    <lastmod>%s</lastmod>
  </url>
`, domain, article.Slug, article.UpdatedAt.Format("2006-01-02")))
	}
	sb.WriteString(`  <url>
    <loc>https://`)
	sb.WriteString(domain)
	sb.WriteString(`/posts/</loc>
  </url>
`)
	var tags []string
	if err := solitudes.System.DB.Raw(`SELECT DISTINCT t FROM articles, unnest(articles.tags) AS t WHERE t IS NOT NULL`).Scan(&tags).Error; err != nil {
		return fmt.Errorf("failed to fetch tags for sitemap: %w", err)
	}
	for _, tag := range tags {
		sb.WriteString(fmt.Sprintf(`  <url>
    <loc>https://%s/tags/%s/</loc>
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
		"hasPrefix": strings.HasPrefix,
		"urlencode": func(s string) string {
			return url.QueryEscape(s)
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
	if len(token) > 0 && token == solitudes.System.Config.User.Token && solitudes.System.Config.User.TokenExpires > time.Now().Unix() {
		c.Locals(solitudes.CtxAuthorized, true)
	} else {
		c.Locals(solitudes.CtxAuthorized, false)
	}
	return c.Next()
}

func loginRequired(c *fiber.Ctx) error {
	if !c.Locals(solitudes.CtxAuthorized).(bool) {
		c.Redirect("/admin/login", http.StatusFound)
		return nil
	}
	return c.Next()
}

func guestRequired(c *fiber.Ctx) error {
	if c.Locals(solitudes.CtxAuthorized).(bool) {
		c.Redirect("/admin", http.StatusFound)
		return nil
	}
	return c.Next()
}

func injectSiteData(c *fiber.Ctx, data fiber.Map) fiber.Map {
	var title, keywords, desc string

	// custom title
	if k, ok := data["title"]; ok && k.(string) != "" {
		title = data["title"].(string) + " | " + solitudes.System.Config.Site.SpaceName
	} else {
		title = solitudes.System.Config.Site.SpaceName
	}
	// custom keywords
	if k, ok := data["keywords"]; ok && k.(string) != "" {
		keywords = data["keywords"].(string)
	} else {
		keywords = solitudes.System.Config.Site.SpaceKeywords
	}
	// custom desc
	if k, ok := data["desc"]; ok && k.(string) != "" {
		desc = data["desc"].(string)
	} else {
		desc = solitudes.System.Config.Site.SpaceDesc
	}

	var soli = make(map[string]interface{})
	soli["Conf"] = solitudes.System.Config
	soli["Theme"] = solitudes.System.Config.Site.ThemeConfig
	soli["Title"] = title
	soli["Keywords"] = keywords
	soli["BuildVersion"] = solitudes.BuildVersion
	soli["Desc"] = desc
	soli["Login"] = c.Locals(solitudes.CtxAuthorized)
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
