package wengine

import (
	"html/template"
	"net/http"
	"reflect"
	"regexp"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/x/soligin"
	"github.com/utrack/gin-csrf"
	"gopkg.in/russross/blackfriday.v2"
)

// WEngine web engine
func WEngine() error {
	if !solitudes.System.C.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.SetFuncMap(template.FuncMap{
		"add": func(a, b int) int {
			return a + b
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
		Secret: solitudes.System.C.Web.User.Password,
		ErrorFunc: func(c *gin.Context) {
			c.HTML(http.StatusBadRequest, "default/error", soligin.Soli(c, true, gin.H{
				"title": "CSRF Protectoion",
				"msg":   "Wow ... Native.",
			}))
			c.Abort()
		},
	}))

	r.Any("/*shit", routerSwitch)

	return r.Run(":8080")
}

/*
Wow ... emmmm 没想到 gin 的路由这么的不近人情。只好自己做一个路由器。
虽然是为了效率，但是有点不优美了，接下来我会考虑更换一个框架。
	- 希望他有 macaron context 的灵活
	- 希望有 fasthttp 的效率
*/
type shitGin struct {
	Match  *regexp.Regexp
	Pre    []gin.HandlerFunc
	Routes map[string]gin.HandlerFunc
}

var shits = []shitGin{
	shitGin{
		Match: regexp.MustCompile(`^\/$`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: index,
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^\/archives/(\d*)/?$`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: archive,
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^\/login$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
			soligin.Limit(soligin.LimitOption{NeedGuest: true}),
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet:  login,
			http.MethodPost: loginHandler,
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^\/logout$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
			soligin.Limit(soligin.LimitOption{NeedLogin: true}),
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodPost: logoutHandler,
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^\/admin\/$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
			soligin.Limit(soligin.LimitOption{NeedLogin: true}),
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: manager,
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^\/count$`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: count,
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^\/admin\/publish$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
			soligin.Limit(soligin.LimitOption{NeedLogin: true}),
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet:  publish,
			http.MethodPost: publishHandler,
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^\/admin\/upload`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
			soligin.Limit(soligin.LimitOption{NeedLogin: true}),
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodPost: upload,
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^\/static\/`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: static("resource/static"),
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^\/upload\/`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: static("resource/data/upload"),
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^\/(.*)$`),
		Pre: []gin.HandlerFunc{
			soligin.Authorize,
		},
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: article,
		},
	},
}

func routerSwitch(c *gin.Context) {
	var params []string
	for j := 0; j < len(shits); j++ {
		params = shits[j].Match.FindStringSubmatch(c.Request.URL.Path)
		if len(params) >= 1 {
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
			c.HTML(http.StatusMethodNotAllowed, "default/error", soligin.Soli(c, true, gin.H{
				"title": "Method Not Allowed",
				"msg":   "Are you lost?",
			}))
			return
		}
	}
}
