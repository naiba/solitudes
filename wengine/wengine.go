package wengine

import (
	"html/template"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/x/soligin"
	"gopkg.in/russross/blackfriday.v2"
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
	})
	r.Static("static", "resource/static")
	r.Static("upload", "resource/data/upload")
	r.LoadHTMLGlob("resource/theme/**/*")

	// router need authorize
	o := r.Group("")
	o.Use(soligin.Authorize)
	o.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "default/index", soligin.Soli(gin.H{
			"Bio": string(blackfriday.Run([]byte(solitudes.System.C.Web.Bio))),
		}))
	})

	// guest router
	g := o.Group("")
	g.Use(soligin.Limit(soligin.LimitOption{NeedGuest: true}))
	{
		g.GET("/login", login)
		g.POST("/login", loginHandler)
	}

	// manager router
	m := o.Group("")
	m.Use(soligin.Limit(soligin.LimitOption{NeedLogin: true}))
	m.GET("/logout", logoutHandler)
	a := m.Group("/admin")
	{
		a.GET("/", manager)
		a.GET("/publish", publish)
		a.POST("/upload", upload)
	}

	return r.Run(":8080")
}
