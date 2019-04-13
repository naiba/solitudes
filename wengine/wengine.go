package wengine

import (
	"fmt"
	"html/template"
	"net/http"
	"reflect"
	"regexp"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/naiba/com"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/x/soligin"
	csrf "github.com/utrack/gin-csrf"
	"gopkg.in/russross/blackfriday.v2"
)

// WEngine web engine
func WEngine() error {
	if !solitudes.System.Config.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.SetFuncMap(template.FuncMap{
		"md5": func(origin string) string {
			return com.MD5(origin)
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
		"unsafe": func(raw string) template.HTML {
			return template.HTML(raw)
		},
		"tf": func(t time.Time, f string) string {
			return t.Format(f)
		},
		"md": func(raw string) template.HTML {
			return template.HTML(string(blackfriday.Run([]byte(raw), blackfriday.WithRenderer(blackfriday.NewHTMLRenderer(
				blackfriday.HTMLRendererParameters{
					Flags: blackfriday.CommonHTMLFlags,
				},
			)), blackfriday.WithExtensions(blackfriday.NoIntraEmphasis|
				blackfriday.Tables|
				blackfriday.FencedCode|
				blackfriday.Autolink|
				blackfriday.Strikethrough|
				blackfriday.SpaceHeadings|
				blackfriday.HeadingIDs|
				blackfriday.BackslashLineBreak|
				blackfriday.DefinitionLists|
				blackfriday.AutoHeadingIDs))))
		},
		"last": func(x int, a interface{}) bool {
			return x == reflect.ValueOf(a).Len()-1
		},
	})
	r.LoadHTMLGlob("resource/theme/**/*")
	store := cookie.NewStore([]byte("secret"))
	r.Use(sessions.Sessions("solisession", store))
	r.Use(csrf.Middleware(csrf.Options{
		Secret: solitudes.System.Config.Web.User.Password,
		ErrorFunc: func(c *gin.Context) {
			c.HTML(http.StatusBadRequest, "default/error", soligin.Soli(c, false, gin.H{
				"title": c.MustGet(solitudes.CtxTranslator).(*solitudes.Translator).T("csrf_rotectoion"),
				"msg":   "Wow ... Native.",
			}))
			c.Abort()
		},
	}))
	r.Use(soligin.Translator)

	r.Any("/*shit", routerSwitch)

	return r.Run(":8080")
}

type shitGin struct {
	Match  *regexp.Regexp
	Pre    []gin.HandlerFunc
	Routes map[string]gin.HandlerFunc
}

func routerSwitch(c *gin.Context) {
	var params []string
	for j := 0; j < len(shits); j++ {
		params = shits[j].Match.FindStringSubmatch(c.Request.URL.Path)
		if len(params) == 0 {
			continue
		}
		if f, ok := shits[j].Routes[c.Request.Method]; ok {
			c.Set(solitudes.CtxRequestParams, params)
			for i := 0; i < len(shits[j].Pre); i++ {
				shits[j].Pre[i](c)
			}
			if len(shits[j].Pre) > 0 && !c.MustGet(solitudes.CtxPassPreHandler).(bool) {
				// 如果没有通过 pre handler
				return
			}
			f(c)
			return
		}
		c.HTML(http.StatusMethodNotAllowed, "default/error", soligin.Soli(c, false, gin.H{
			"title": c.MustGet(solitudes.CtxTranslator).(*solitudes.Translator).T("method_not_allowed"),
			"msg":   c.MustGet(solitudes.CtxTranslator).(*solitudes.Translator).T("are_you_lost"),
		}))
		return
	}
	tr := c.MustGet(solitudes.CtxTranslator).(*solitudes.Translator)
	c.HTML(http.StatusNotFound, "default/error", soligin.Soli(c, false, gin.H{
		"title": tr.T("404_title"),
		"msg":   tr.T("404_msg"),
	}))
}
