package router

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/naiba/solitudes/internal/model"
	"github.com/samber/lo"
	"gorm.io/gorm"

	"github.com/88250/lute"
	"github.com/go-playground/locales"
	gv "github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	html "github.com/gofiber/template/html/v2"

	"github.com/naiba/solitudes"
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
}

func mdRender(id string, raw string) string {
	return luteEngine.MarkdownStr(id, raw)
}

// Serve web service
func Serve() {
	engine := html.New("resource/theme", ".html")
	setFuncMap(engine)
	dbErrors := []error{
		gorm.ErrInvalidTransaction,
	}
	app := fiber.New(fiber.Config{
		EnableTrustedProxyCheck: solitudes.System.Config.EnableTrustedProxyCheck,
		TrustedProxies:          solitudes.System.Config.TrustedProxies,
		ProxyHeader:             solitudes.System.Config.ProxyHeader,
		Views:                   engine,
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
				return c.Status(http.StatusInternalServerError).Render("default/error", injectSiteData(c, fiber.Map{
					"title": title,
					"msg":   errMsg,
				}))
			}
			_, e = c.Status(http.StatusInternalServerError).WriteString(errMsg)
			return e
		},
	})
	if solitudes.System.Config.Debug {
		app.Use(logger.New())
		engine.Reload(true)
		engine.Debug(true)
	}

	app.Use(trans, auth)
	app.Get("/", index)
	app.Get("/feed/:format?", feedHandler)
	app.Get("/archive/:page?", archive)
	app.Get("/books/:page?", book)
	app.Get("/search/", search)
	app.Get("/tags/:tag/:page?", tags)
	app.Get("/tags/", tagsCloud)
	app.Get("/login", guestRequired, login)
	app.Post("/login", guestRequired, loginHandler)
	app.Post("/logout", loginRequired, logoutHandler)
	app.Get("/captcha", generateCaptcha)
	app.Post("/count", count)
	app.Post("/comment", commentHandler)
	app.Static("/static", "resource/static")
	app.Static("/upload", "data/upload")

	admin := app.Group("/admin", loginRequired)
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

	app.Get("/:slug/:version?", article)
	app.Use(page404)

	app.Listen(":8080")
}

func page404(c *fiber.Ctx) error {
	tr := c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	c.Status(http.StatusNotFound).Render("default/error", injectSiteData(c, fiber.Map{
		"title": tr.T("404_title"),
		"msg":   tr.T("404_msg"),
	}))
	return nil
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
		"unsafe": func(raw string) template.HTML {
			return template.HTML(raw)
		},
		"tf": func(t time.Time, f string) string {
			return t.Format(f)
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
		"artileData": func(article *model.Article, tr *translator.Translator) fiber.Map {
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
		"substr": func(s string, start, length int) string {
			runes := []rune(s)
			if start < 0 || start >= len(runes) {
				return ""
			}
			end := start + length
			if end > len(runes) {
				end = len(runes)
			}
			return string(runes[start:end])
		},
		"hasPrefix": strings.HasPrefix,
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
		c.Redirect("/login", http.StatusFound)
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
	soli["Title"] = title
	soli["Keywords"] = keywords
	soli["BuildVersion"] = solitudes.BuildVersion
	soli["Desc"] = desc
	soli["Login"] = c.Locals(solitudes.CtxAuthorized)
	soli["Data"] = data
	soli["Tr"] = c.Locals(solitudes.CtxTranslator).(*translator.Translator)
	soli["Path"] = c.Path()

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
