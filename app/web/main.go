package main

import (
	"html/template"
	"net/http"

	"github.com/naiba/solitudes"

	"github.com/gin-gonic/gin"
	"github.com/naiba/solitudes/x/soligin"
	"gopkg.in/russross/blackfriday.v2"
)

func main() {
	r := gin.New()
	r.SetFuncMap(template.FuncMap{
		"unsafe": func(raw string) template.HTML {
			return template.HTML(raw)
		},
	})
	r.Static("static", "resource/static")
	r.LoadHTMLGlob("resource/theme/**/*")

	o := r.Group("")
	o.Use(soligin.Authorize)
	o.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "default/index", soligin.Soli(gin.H{
			"Bio": string(blackfriday.Run([]byte(solitudes.System.C.Web.Bio))),
		}))
	})
	o.GET("/login", soligin.Limit(soligin.LimitOption{
		NeedGuest: true,
	}), func(c *gin.Context) {
		c.HTML(http.StatusOK, "admin/login", gin.H{})
	})
	admin := o.Group("/admin")
	admin.Use(soligin.Limit(soligin.LimitOption{
		NeedLogin: true,
	}))
	{
		admin.GET("/")
	}
	r.Run()
}
