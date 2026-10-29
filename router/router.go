package router

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
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

	"github.com/go-chi/chi/v5"

	"github.com/88250/lute"
	"github.com/88250/lute/ast"
	luteHtml "github.com/88250/lute/html"
	luteRender "github.com/88250/lute/render"
	luteUtil "github.com/88250/lute/util"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-playground/locales"
	gv "github.com/go-playground/validator/v10"
	html "github.com/gofiber/template/html/v2"
	"github.com/samber/lo"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/pkg/content"
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
				{"alt", luteUtil.BytesToStr(luteHtml.EscapeHTML([]byte(n.Text())))},
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

// themeStaticHandler handles static file requests dynamically based on kind and theme
func themeStaticHandler(writer http.ResponseWriter, httpRequest *http.Request) error {
	kind := routeParam(httpRequest, "kind")
	themeName := routeParam(httpRequest, "theme")
	relativePath := routeParam(httpRequest, "*")

	if (kind != "site" && kind != "admin") || !themeNamePattern.MatchString(themeName) {
		return page404(writer, httpRequest)
	}

	// 拒绝目录列表请求（如 /static/site/cactus/ 或 /static/site/cactus/css/）
	if relativePath == "" || strings.HasSuffix(relativePath, "/") {
		return page404(writer, httpRequest)
	}

	// URL wildcard 必须在主题静态目录内解析，不能再拼成宿主路径后 SendFile。
	staticRoot, err := os.OpenRoot(ThemeStaticRoot(kind, themeName))
	if err != nil {
		return page404(writer, httpRequest)
	}
	defer staticRoot.Close()

	assetFile, err := staticRoot.Open(relativePath)
	if err != nil {
		return page404(writer, httpRequest)
	}

	assetInfo, err := assetFile.Stat()
	if err != nil || assetInfo.IsDir() || assetInfo.Size() > int64(^uint(0)>>1) {
		assetFile.Close()
		return page404(writer, httpRequest)
	}

	writer.Header().Set("Cache-Control", "public, max-age=2592000")
	setContentType(writer, filepath.Ext(relativePath))
	return sendStream(writer, httpRequest, assetFile, int(assetInfo.Size()))
}

// uploadStaticHandler serves only files inside data/upload, including when
// the directory contains an unexpected symlink.
func uploadStaticHandler(writer http.ResponseWriter, httpRequest *http.Request) error {
	relativePath := routeParam(httpRequest, "*")
	if relativePath == "" || strings.HasSuffix(relativePath, "/") {
		return page404(writer, httpRequest)
	}
	root, err := os.OpenRoot("data/upload")
	if err != nil {
		return page404(writer, httpRequest)
	}
	defer root.Close()
	file, err := root.Open(relativePath)
	if err != nil {
		return page404(writer, httpRequest)
	}
	info, err := file.Stat()
	if err != nil || info.IsDir() || info.Size() > int64(^uint(0)>>1) {
		file.Close()
		return page404(writer, httpRequest)
	}
	writer.Header().Set("Cache-Control", "public, max-age=2592000")
	setContentType(writer, filepath.Ext(relativePath))
	return sendStream(writer, httpRequest, file, int(info.Size()))
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

var mdImageRegex = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
var htmlImageRegex = regexp.MustCompile(`<img\s[^>]*src=["']([^"']+)["']`)

func mdExcerpt(markdown string, maxLen int) string {
	return content.Excerpt(markdown, maxLen)
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
func newApp() *chi.Mux {
	return newAppWithRoutes(nil)
}

func newAppWithRoutes(extraRoutes func(chi.Router)) *chi.Mux {
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

	app := chi.NewRouter()
	app.Use(requestLifecycle(globalDynamicEngine, applicationError), prepareRoutePath, middleware.GetHead)
	app.Use(before(canonicalURLRedirect), securityHeaders, auditMiddleware)
	if solitudes.System.Config.Debug {
		app.Use(requestAccessLog)
	}
	// Protocol authentication is independent of browser sessions and CSRF checks.
	if activeOIDCProvider != nil {
		for _, path := range []string{"/.well-known", "/authorize", "/oauth", "/userinfo", "/revoke", "/end_session", "/keys", "/healthz", "/ready"} {
			app.Handle(path, serveEndpoint(oidcHTTPHandler(activeOIDCProvider)))
			app.Handle(path+"/*", serveEndpoint(oidcHTTPHandler(activeOIDCProvider)))
		}
	}
	app.Group(func(site chi.Router) {
		site.Use(before(trans), before(auth), before(csrfGuard))
		site.Get("/", serveEndpoint(index))
		site.Get("/favicon.ico", serveEndpoint(faviconHandler))
		site.Get("/logo.png", serveEndpoint(logoHandler))
		site.Get("/feed", serveEndpoint(feedHandler))
		site.Get("/feed/{format}", serveEndpoint(feedHandler))
		site.Get("/posts", serveEndpoint(posts))
		site.Get("/posts/{page}", serveEndpoint(posts))
		site.Get("/books", serveEndpoint(book))
		site.Get("/books/{page}", serveEndpoint(book))
		site.Get("/search", serveEndpoint(search))
		site.Get("/readers", serveEndpoint(readerCircle))
		site.Get("/users/{id}", serveEndpoint(publicUserProfile))
		site.Get("/tags/{tag}", serveEndpoint(tags))
		site.Get("/tags/{tag}/{page}", serveEndpoint(tags))
		site.Get("/tags", serveEndpoint(tagsCloud))
		site.Get("/r/go", serveEndpoint(goRedirect))
		site.Get("/robots.txt", serveEndpoint(robotsHandler))
		site.Get("/sitemap.xml", serveEndpoint(sitemapHandler))
		site.Get("/llms.txt", serveEndpoint(llmsDirectory))
		site.Get("/read/{id}/article.md", serveEndpoint(articleMachineContent))
		site.Post("/logout", serveEndpoint(requireAccount, logoutHandler))
		site.Get("/captcha", serveEndpoint(generateCaptcha))
		site.Post("/api/comment", serveEndpoint(commentHandler))
		site.Post("/api/count", serveEndpoint(count))
		site.Get("/api/commenter-info", serveEndpoint(commenterInfoHandler))
		site.Get("/r/{token}", serveEndpoint(trackEmailReadRedirect))
		site.Get("/static/i/{token}", serveEndpoint(trackEmailRead))
		site.Get("/static/{kind}/{theme}/*", serveEndpoint(themeStaticHandler))
		site.Get("/upload/*", serveEndpoint(uploadStaticHandler))
		site.Get("/login", serveEndpoint(guestRequired, login))
		site.Post("/login", serveEndpoint(guestRequired, loginHandler))
		site.Get("/register", serveEndpoint(guestRequired, registerPage))
		site.Post("/register", serveEndpoint(guestRequired, registerHandler))
		site.Post("/resend-verification", serveEndpoint(resendVerification))
		site.Get("/verify-email", serveEndpoint(verifyEmailHandler))
		site.Get("/account", serveEndpoint(requireAccount, accountPage))
		site.Post("/account/profile", serveEndpoint(requireAccount, updateAccountProfile))
		site.Post("/account/password", serveEndpoint(requireAccount, changeAccountPassword))
		site.Post("/account/identities/{provider}/unlink", serveEndpoint(requireAccount, unlinkAccountIdentity))
		site.Get("/account/oidc/clients", serveEndpoint(requireAccount, oidcClientsPage))
		site.Post("/account/oidc/clients", serveEndpoint(requireAccount, createOIDCClient))
		site.Post("/account/oidc/clients/{id}/metadata", serveEndpoint(requireAccount, updateOIDCClient))
		site.Post("/account/oidc/clients/{id}/disable", serveEndpoint(requireAccount, disableOIDCClient))
		site.Post("/account/oidc/clients/{id}/delete", serveEndpoint(requireAccount, deleteOIDCClient))
		site.Get("/account/oidc/authorizations", serveEndpoint(requireAccount, authorizedApplicationsPage))
		site.Post("/account/oidc/authorizations/{id}/revoke", serveEndpoint(requireAccount, revokeApplicationGrant))
		site.Get("/oidc/consent", serveEndpoint(consentPage))
		site.Post("/oidc/consent", serveEndpoint(requireAccount, consentHandler))
		site.Get("/auth/{provider}/callback", serveEndpoint(oauthCallback))
		site.Post("/auth/{provider}/link", serveEndpoint(requireAccount, beginOAuthLink))
		site.Get("/auth/{provider}", serveEndpoint(guestRequired, beginOAuthLogin))
		site.Post("/auth/passkey/login/begin", serveEndpoint(beginPasskeyLogin))
		site.Post("/auth/passkey/login/finish", serveEndpoint(finishPasskeyLogin))
		site.Post("/account/passkeys/begin", serveEndpoint(requireAccount, beginPasskeyRegistration))
		site.Post("/account/passkeys/finish", serveEndpoint(requireAccount, finishPasskeyRegistration))
		site.Delete("/account/passkeys/{id}", serveEndpoint(requireAccount, deletePasskey))

		// Removed login endpoints must return 404 before the admin authentication guard.
		for _, path := range []string{"/admin/login", "/admin/register", "/admin/verify-email", "/admin/resend-verification"} {
			site.Handle(path, serveEndpoint(page404))
		}
		site.Route("/admin", func(admin chi.Router) {
			admin.Use(before(loginRequired))
			admin.Get("/", serveEndpoint(requireAdmin, manager))
			admin.Get("/users", serveEndpoint(requireAdmin, usersPage))
			admin.Get("/users/{id}", serveEndpoint(requireAdmin, adminUserDetail))
			admin.Get("/audit", serveEndpoint(requireAdmin, auditPage))
			admin.Post("/users/{id}/role", serveEndpoint(requireAdmin, setUserRole))
			admin.Get("/auth/providers", serveEndpoint(requireAdmin, loginProvidersPage))
			admin.Post("/auth/providers", serveEndpoint(requireAdmin, saveLoginProviders))
			admin.Get("/oidc/clients", serveEndpoint(requireAdmin, oidcClientsPage))
			admin.Get("/oidc/clients/{id}", serveEndpoint(requireAdmin, adminClientDetail))
			admin.Post("/oidc/clients", serveEndpoint(requireAdmin, createOIDCClient))
			admin.Post("/oidc/clients/{id}/metadata", serveEndpoint(requireAdmin, updateOIDCClient))
			admin.Post("/oidc/clients/{id}/disable", serveEndpoint(requireAdmin, disableOIDCClient))
			admin.Post("/oidc/clients/{id}/delete", serveEndpoint(requireAdmin, deleteOIDCClient))
			admin.Post("/oidc/keys/rotate", serveEndpoint(requireAdmin, rotateOIDCKeys))
			admin.Get("/publish", serveEndpoint(publish))
			admin.Post("/publish", serveEndpoint(publishHandler))
			admin.Post("/rebuild-full-text-search", serveEndpoint(requireAdmin, rebuildFullTextSearch))
			admin.Post("/upload", serveEndpoint(upload))
			admin.Post("/fetch", serveEndpoint(requireAdmin, fetch))
			admin.Get("/comments", serveEndpoint(requireAdmin, comments))
			admin.Delete("/comments", serveEndpoint(requireAdmin, deleteComment))
			admin.Post("/report-spam", serveEndpoint(requireAdmin, reportSpam))
			admin.Post("/restore-spam", serveEndpoint(requireAdmin, restoreSpam))
			admin.Get("/articles", serveEndpoint(manageArticle))
			admin.Delete("/articles", serveEndpoint(deleteArticle))
			admin.Get("/media", serveEndpoint(requireAdmin, media))
			admin.Delete("/media", serveEndpoint(requireAdmin, mediaHandler))
			admin.Get("/settings", serveEndpoint(requireAdmin, settings))
			admin.Post("/settings", serveEndpoint(requireAdmin, settingsHandler))
			admin.Get("/redirects", serveEndpoint(requireAdmin, redirectManagement))
			admin.Post("/redirects", serveEndpoint(requireAdmin, saveRedirect))
			admin.Delete("/redirects", serveEndpoint(requireAdmin, deleteRedirects))
			admin.Get("/tags", serveEndpoint(requireAdmin, tagsManagePage))
			admin.Delete("/tags", serveEndpoint(requireAdmin, deleteTag))
			admin.Patch("/tags", serveEndpoint(requireAdmin, renameTag))
			admin.Get("/api/search-tags", serveEndpoint(searchTags))
			admin.Get("/api/search-books", serveEndpoint(searchBooks))
			admin.Get("/theme/preview/{kind}/{name}", serveEndpoint(themePreview))

		})
		if extraRoutes != nil {
			extraRoutes(site)
		}
		// Fixed system routes win over redirects; only article routes consult them.
		site.Group(func(articles chi.Router) {
			articles.Use(before(registeredRedirect))
			articles.Get("/{slug}/compare", serveEndpoint(articleCompare))
			articles.Get("/{slug}/compare/{versions}", serveEndpoint(articleCompare))
			articles.Get("/{slug}", serveEndpoint(article))
			articles.Get("/{slug}/{version}", serveEndpoint(article))
			articles.NotFound(serveEndpoint(registeredRedirect, page404))
		})
	})
	return app
}

func applicationError(writer http.ResponseWriter, httpRequest *http.Request, e error) error {
	// 404 页面
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return page404(writer, httpRequest)
	}
	if requestValue(httpRequest, solitudes.CtxTranslator) == nil {
		_ = trans(writer, httpRequest)
	}
	tr := requestValue(httpRequest, solitudes.CtxTranslator).(*translator.Translator)
	title := tr.T("500_title")
	errMsg := e.Error()
	status := http.StatusInternalServerError
	var responseError *httpError
	if errors.As(e, &responseError) {
		status = responseError.Code
	}
	if status >= http.StatusInternalServerError {
		log.Printf("request failed request_id=%s status=%d error_type=%T", writer.Header().Get("X-Request-ID"), status, e)
		errMsg = tr.T("500_msg")
	} else if status == http.StatusForbidden {
		title, errMsg = tr.T("403_title"), tr.T("403_msg")
	} else {
		title = fmt.Sprintf("%d %s", status, http.StatusText(status))
	}
	writer.Header().Set("Cache-Control", "no-store")
	if strings.Contains(string(requestHeader(httpRequest, "Accept")), "html") {
		templateName := "site/error"
		if isAdminPath(requestPath(httpRequest)) {
			templateName = "admin/error"
		}
		return renderPage(writer, httpRequest, status, templateName, injectSiteData(writer, httpRequest, viewData{
			"title":   title,
			"msg":     errMsg,
			"status":  status,
			"noindex": true,
		}))
	}
	_, e = writeResponseString(writer, status, errMsg)
	return e
}

// Serve web service
func Serve() {
	if err := (&http.Server{Addr: ":8080", Handler: newApp(), ReadHeaderTimeout: 10 * time.Second}).ListenAndServe(); err != nil {
		log.Printf("web server stopped: %v", err)
	}
}

func themePreview(writer http.ResponseWriter, httpRequest *http.Request) error {
	kind := routeParam(httpRequest, "kind")
	name := routeParam(httpRequest, "name")
	if (kind != "site" && kind != "admin") || !themeNamePattern.MatchString(name) {
		return writeStatus(writer, http.StatusNotFound)
	}
	root, err := os.OpenRoot(themeResourcePath(kind, name, ""))
	if err != nil {
		return writeStatus(writer, http.StatusNotFound)
	}
	defer root.Close()
	file, err := root.Open("screenshot.png")
	if err != nil {
		return writeStatus(writer, http.StatusNotFound)
	}
	info, err := file.Stat()
	if err != nil || info.IsDir() || info.Size() > int64(^uint(0)>>1) {
		file.Close()
		return writeStatus(writer, http.StatusNotFound)
	}
	setContentType(writer, "png")
	return sendStream(writer, httpRequest, file, int(info.Size()))
}

func serveUploadOrThemeFile(writer http.ResponseWriter, httpRequest *http.Request, uploadPath, themePath string) error {
	if err := sendRootFile(writer, httpRequest, "data/upload", filepath.Base(uploadPath)); err == nil {
		return nil
	}
	return sendRootFile(writer, httpRequest, ThemeStaticRoot("site", solitudes.System.Config.Site.Theme), themePath)
}

func sendRootFile(writer http.ResponseWriter, httpRequest *http.Request, rootPath, relativePath string) error {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return newHTTPError(http.StatusNotFound)
	}
	defer root.Close()
	file, err := root.Open(relativePath)
	if err != nil {
		return newHTTPError(http.StatusNotFound)
	}
	info, err := file.Stat()
	if err != nil || info.IsDir() || info.Size() > int64(^uint(0)>>1) {
		file.Close()
		return newHTTPError(http.StatusNotFound)
	}
	setContentType(writer, filepath.Ext(relativePath))
	return sendStream(writer, httpRequest, file, int(info.Size()))
}

func faviconHandler(writer http.ResponseWriter, httpRequest *http.Request) error {
	return serveUploadOrThemeFile(writer, httpRequest, "data/upload/favicon.ico", "images/favicon.ico")
}

func logoHandler(writer http.ResponseWriter, httpRequest *http.Request) error {
	return serveUploadOrThemeFile(writer, httpRequest, "data/upload/logo.png", "images/logo.png")
}

// goRedirect 处理外部链接跳转
func goRedirect(writer http.ResponseWriter, httpRequest *http.Request) error {
	encodedURL := queryValue(httpRequest, "url")
	if encodedURL == "" {
		return writeText(writer, http.StatusBadRequest, "Missing url parameter")
	}

	// 显示跳转提示页面，实际解析由前端 JS 完成
	tr := requestValue(httpRequest, solitudes.CtxTranslator).(*translator.Translator)
	return renderPage(writer, httpRequest, http.StatusOK, "site/redirect", injectSiteData(writer, httpRequest, viewData{
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

func page404(writer http.ResponseWriter, httpRequest *http.Request) error {
	writer.Header().Set("Cache-Control", "no-store")
	tr := requestValue(httpRequest, solitudes.CtxTranslator).(*translator.Translator)
	templateName := "site/error"
	if isAdminPath(requestPath(httpRequest)) {
		templateName = "admin/error"
	}
	return renderPage(writer, httpRequest, http.StatusNotFound, templateName, injectSiteData(writer, httpRequest, viewData{
		"title":   tr.T("404_title"),
		"msg":     tr.T("404_msg"),
		"status":  http.StatusNotFound,
		"noindex": true,
	}))

}

func robotsHandler(writer http.ResponseWriter, httpRequest *http.Request) error {
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
	writer.Header().Set("Content-Type", "text/plain")
	return writeText(writer, http.StatusOK, robotsTxt)

}

func sitemapHandler(writer http.ResponseWriter, httpRequest *http.Request) error {
	type sitemapURL struct {
		Location   string `xml:"loc"`
		LastMod    string `xml:"lastmod,omitempty"`
		ChangeFreq string `xml:"changefreq"`
		Priority   string `xml:"priority"`
	}
	sitemap := struct {
		XMLName xml.Name     `xml:"http://www.sitemaps.org/schemas/sitemap/0.9 urlset"`
		URLs    []sitemapURL `xml:"url"`
	}{}
	baseURL := "https://" + solitudes.System.Config.Site.Domain
	sitemap.URLs = append(sitemap.URLs, sitemapURL{Location: baseURL + "/", ChangeFreq: "daily", Priority: "1.0"})
	var articles []model.Article
	if err := solitudes.System.DB.Select("slug", "updated_at", "created_at").Where("visibility = ?", model.VisibilityPublic).Order("created_at DESC").Find(&articles).Error; err != nil {
		return fmt.Errorf("failed to fetch articles for sitemap: %w", err)
	}
	for _, article := range articles {
		modified := article.UpdatedAt
		if modified.IsZero() {
			modified = article.CreatedAt
		}
		entry := sitemapURL{Location: baseURL + "/" + url.PathEscape(article.Slug), ChangeFreq: "monthly", Priority: "0.8"}
		if !modified.IsZero() {
			entry.LastMod = modified.Format("2006-01-02")
		}
		sitemap.URLs = append(sitemap.URLs, entry)
	}
	sitemap.URLs = append(sitemap.URLs,
		sitemapURL{Location: baseURL + "/posts/", ChangeFreq: "daily", Priority: "0.6"},
		sitemapURL{Location: baseURL + "/books/", ChangeFreq: "weekly", Priority: "0.6"},
		sitemapURL{Location: baseURL + "/tags/", ChangeFreq: "weekly", Priority: "0.5"},
	)
	var listedReaders int64
	if err := eligibleReaders(solitudes.System.DB).Count(&listedReaders).Error; err != nil {
		return fmt.Errorf("count public reader circle members: %w", err)
	}
	if listedReaders > 0 {
		sitemap.URLs = append(sitemap.URLs, sitemapURL{Location: baseURL + "/readers/", ChangeFreq: "daily", Priority: "0.5"})
	}
	var tags []string
	if err := solitudes.System.DB.Raw(`SELECT DISTINCT t FROM articles, unnest(articles.tags) AS t WHERE visibility = 'public' AND t IS NOT NULL AND t <> '' ORDER BY t`).Scan(&tags).Error; err != nil {
		return fmt.Errorf("failed to fetch tags for sitemap: %w", err)
	}
	for _, tag := range tags {
		sitemap.URLs = append(sitemap.URLs, sitemapURL{Location: baseURL + "/tags/" + url.PathEscape(tag) + "/", ChangeFreq: "weekly", Priority: "0.5"})
	}
	body, err := xml.MarshalIndent(sitemap, "", "  ")
	if err != nil {
		return fmt.Errorf("encode sitemap: %w", err)
	}
	writer.Header().Set("Content-Type", "application/xml; charset=utf-8")
	return writeText(writer, http.StatusOK, xml.Header+string(body))
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
		"md":                 mdRender,
		"articleMarkdown":    articleMarkdown,
		"articleMarkdownURL": articleMarkdownURL,
		"randomIndices":      randomIndices,
		"dict":               templateDict,
		"pathEscape":         url.PathEscape,
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
		"mdExcerpt":       mdExcerpt,
		"hasPrefix":       strings.HasPrefix,
		"tocHeadingCount": tocHeadingCount,
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
		"externalLink": externalLink,
		"profileBio":   profileBio,
	}
	for name, fn := range funcMap {
		engine.AddFunc(name, fn)
	}
}

func auth(writer http.ResponseWriter, httpRequest *http.Request) error {
	token := requestCookie(httpRequest, solitudes.AuthCookie)
	account, err := sessionLookup(token)
	if err != nil {
		account = nil
	}
	requestValue(httpRequest, solitudes.CtxAccount, account)
	return nil
}

func loginRequired(writer http.ResponseWriter, httpRequest *http.Request) error {
	account := currentAccount(writer, httpRequest)
	if account == nil {
		if httpRequest.Method == http.MethodGet {
			return redirectResponse(writer, httpRequest, "/login?return_to="+url.QueryEscape(requestTarget(httpRequest)), http.StatusFound)
		}
		return redirectResponse(writer, httpRequest, "/login", http.StatusFound)
	}
	if !account.Role.CanPublish() {
		return newHTTPError(http.StatusForbidden)
	}
	return nil
}

func csrfSameOriginHeader(writer http.ResponseWriter, httpRequest *http.Request, header string) bool {
	raw := requestHeader(httpRequest, header)
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return strings.EqualFold(u.Host, requestHeader(httpRequest, "Host")) && strings.EqualFold(u.Scheme, requestScheme(httpRequest))
}

func csrfGuard(writer http.ResponseWriter, httpRequest *http.Request) error {
	switch httpRequest.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return nil
	}
	origin := requestHeader(httpRequest, "Origin")
	referer := requestHeader(httpRequest, "Referer")
	if origin == "" && referer == "" {
		return newHTTPError(http.StatusForbidden, "missing Origin and Referer")
	}
	if origin != "" && !csrfSameOriginHeader(writer, httpRequest, "Origin") {
		return newHTTPError(http.StatusForbidden, "cross-origin request blocked")
	}
	if origin == "" && !csrfSameOriginHeader(writer, httpRequest, "Referer") {
		return newHTTPError(http.StatusForbidden, "cross-origin request blocked")
	}
	return nil
}

func guestRequired(writer http.ResponseWriter, httpRequest *http.Request) error {
	if account := currentAccount(writer, httpRequest); account != nil {
		// A validated pending OIDC flow may require a fresh sign-in. Do not
		// redirect an already signed-in browser back into a max_age/login loop.
		if requestPath(httpRequest) == "/login" || strings.HasPrefix(requestPath(httpRequest), "/auth/") {
			returnTo := queryValue(httpRequest, "return_to")
			if httpRequest.Method == http.MethodPost {
				returnTo = formValue(httpRequest, "return_to")
			}
			application, err := loginOIDCApplication(writer, httpRequest, safeReturnPath(returnTo))
			if err != nil {
				return err
			}
			if application != nil {
				return nil
			}
		}
		if target := safeReturnPath(queryValue(httpRequest, "return_to")); target != "" {
			return redirectResponse(writer, httpRequest, target, http.StatusFound)
		}
		return redirectResponse(writer, httpRequest, "/account", http.StatusFound)
	}
	return nil
}

func injectSiteData(writer http.ResponseWriter, httpRequest *http.Request, data viewData) viewData {
	var title, keywords, desc string
	siteName := solitudes.System.Config.Site.SpaceName
	siteDesc := solitudes.System.Config.Site.SpaceDesc

	if k, ok := data["title"]; ok && k.(string) != "" {
		title = data["title"].(string) + " - " + siteName
	} else {
		if siteDesc != "" && siteDesc != siteName {
			title = siteName + " - " + siteDesc
		} else {
			title = siteName
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
	buildVersion := solitudes.BuildVersion
	if strings.HasPrefix(buildVersion, "_BuildV") {
		buildVersion = "dev"
	}
	soli["BuildVersion"] = buildVersion
	soli["Desc"] = desc
	soli["OgType"] = ogType
	soli["Noindex"] = noindex
	if canonical, ok := data["canonical_path"].(string); ok {
		soli["CanonicalPath"] = safeReturnPath(canonical)
	}
	account := currentAccount(writer, httpRequest)
	soli["Account"] = account
	soli["Queries"] = &TemplateQueries{db: solitudes.System.DB, account: account, notice: accessNoticeFor(writer, httpRequest)}
	soli["Login"] = account != nil && account.Role.CanPublish()
	soli["Data"] = data
	soli["Tr"] = requestValue(httpRequest, solitudes.CtxTranslator).(*translator.Translator)
	soli["Path"] = requestPath(httpRequest)
	soli["Now"] = time.Now()

	return soli
}

func trans(writer http.ResponseWriter, httpRequest *http.Request) error {
	t, _ := translator.Trans.FindTranslator(getAcceptLanguages(requestHeader(httpRequest, "Accept-Language"))...)
	requestValue(httpRequest, solitudes.CtxTranslator, &translator.Translator{Trans: t, Translator: t.(locales.Translator)})
	return nil
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
