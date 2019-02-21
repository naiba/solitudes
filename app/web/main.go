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

	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "default/index", soligin.Soli(gin.H{
			"Bio": string(blackfriday.Run([]byte(solitudes.System.C.Web.Bio))),
		}))
	})
	r.GET("/login", func(c *gin.Context) {
		c.HTML(http.StatusOK, "admin/login", gin.H{})
	})
	admin := r.Group("/admin")
	{
		admin.GET("/", func(c *gin.Context) {
		})
	}
	r.Run()
}
