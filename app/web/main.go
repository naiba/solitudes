package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/naiba/solitudes"
)

func init() {
}

func main() {
	solitudes.Solitudes.Invoke(func(cf *solitudes.Config) {
		log.Println("[System config]", cf)
	})

	r := gin.New()
	r.Static("static", "resource/static")
	r.LoadHTMLGlob("resource/theme/**/*")
	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "default/index", gin.H{})
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
