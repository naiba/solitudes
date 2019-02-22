package wengine

import (
	"html/template"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/x/soligin"
)

// WEngine web engine
func WEngine() error {
	if !solitudes.System.C.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.SetFuncMap(template.FuncMap{
		"unsafe": func(raw string) template.HTML {
			return template.HTML(raw)
		},
		"tf": func(t time.Time, f string) string {
			return t.Format(f)
		},
	})
	r.LoadHTMLGlob("resource/theme/**/*")

	r.Any("/*shit", routerSwitch)

	// // manager router
	// m := o.Group("")
	// m.Use(soligin.Limit(soligin.LimitOption{NeedLogin: true}))
	// m.GET("/logout", logoutHandler)
	// a := m.Group("/admin")
	// {
	// 	a.GET("/", manager)
	// 	a.GET("/publish", publish)
	// 	a.POST("/publish", publishHandler)
	// 	a.POST("/upload", upload)
	// }

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
		Match: regexp.MustCompile(`^/$`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: index,
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^/archive`),
		Routes: map[string]gin.HandlerFunc{
			//TODO: archive
			http.MethodGet: index,
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^/login/`),
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
		Match: regexp.MustCompile(`^/static/`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: static("resource/static"),
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^/upload/`),
		Routes: map[string]gin.HandlerFunc{
			http.MethodGet: static("resource/upload"),
		},
	},
	shitGin{
		Match: regexp.MustCompile(`^/.*$`),
		Routes: map[string]gin.HandlerFunc{
			//TODO: slug
			http.MethodGet: login,
		},
	},
}

func routerSwitch(c *gin.Context) {
	log.Println(c.Request.URL.Path)
	for j := 0; j < len(shits); j++ {
		if shits[j].Match.MatchString(c.Request.URL.Path) {
			if f, ok := shits[j].Routes[c.Request.Method]; ok {
				for i := 0; i < len(shits[j].Pre); i++ {
					log.Println("run pre", i)
					shits[j].Pre[i](c)
				}
				log.Println("run after")
				f(c)
				return
			}
			c.String(http.StatusMethodNotAllowed, "method not allowed")
			return
		}
	}
}
