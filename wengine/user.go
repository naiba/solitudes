package wengine

import (
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/x/soligin"
	"github.com/naiba/solitudes/x/soliwriter"
	"golang.org/x/crypto/bcrypt"
)

type loginForm struct {
	Email    string `form:"email" binding:"required,email"`
	Password string `form:"password" binding:"required"`
	Remember string `form:"remember"`
}

func loginHandler(c *gin.Context) {
	var lf loginForm
	if err := c.ShouldBind(&lf); err != nil {
		c.String(http.StatusOK, err.Error())
		return
	}
	if lf.Email != solitudes.System.Config.Web.User.Email ||
		bcrypt.CompareHashAndPassword([]byte(solitudes.System.Config.Web.User.Password),
			[]byte(lf.Password)) != nil {
		c.String(http.StatusOK, "Invalid email or password")
		return
	}
	token, err := bcrypt.GenerateFromPassword([]byte(lf.Password+time.Now().String()), bcrypt.DefaultCost)
	if err != nil {
		c.String(http.StatusOK, err.Error())
		return
	}
	solitudes.System.Token = string(token)
	if lf.Remember == "on" {
		solitudes.System.TokenExpires = time.Now().AddDate(0, 3, 0)
	} else {
		solitudes.System.TokenExpires = time.Now().Add(time.Hour * 4)
	}
	c.SetCookie(solitudes.AuthCookie, string(token), int(time.Hour*24*90), "/", "", false, false)
	c.Redirect(http.StatusFound, "/admin/")
}

func login(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/login", soligin.Soli(c, true, gin.H{}))
}

func logoutHandler(c *gin.Context) {
	solitudes.System.TokenExpires = time.Now()
	solitudes.System.Token = ""
	c.Redirect(http.StatusFound, "/")
}

func index(c *gin.Context) {
	var as []solitudes.Article
	solitudes.System.DB.Order("updated_at DESC").Limit(10).Find(&as)
	for i := 0; i < len(as); i++ {
		as[i].RelatedCount()
	}
	c.HTML(http.StatusOK, "default/index", soligin.Soli(c, false, gin.H{
		"title":    "Home",
		"bio":      solitudes.System.Config.Web.Bio,
		"articles": as,
	}))
}

func static(root string) gin.HandlerFunc {
	return func(c *gin.Context) {
		i := strings.Index(c.Request.URL.Path[1:], "/")
		// 其实这边 gin 已经过滤了一遍了 我这边再过滤一下
		filepath := path.Clean(root + c.Request.URL.Path[i+1:])
		http.ServeFile(soliwriter.InterceptResponseWriter{
			ResponseWriter: c.Writer,
			ErrH: func(h http.ResponseWriter, s int) {
				h.Header().Set("Content-Type", "text/html,charset=utf8")
				h.Header().Set("X-File-Server", "solitudes")
				tr := c.MustGet(solitudes.CtxTranslator).(*solitudes.Translator)
				c.HTML(s, "default/error", soligin.Soli(c, false, gin.H{
					"title": tr.T("404_title"),
					"msg":   tr.T("404_msg"),
				}))
			},
		}, c.Request, filepath)
	}
}

func count(c *gin.Context) {
	if c.Query("slug") == "" {
		return
	}
	key := c.ClientIP() + c.Query("slug")
	if _, ok := solitudes.System.Cache.Get(key); ok {
		return
	}
	solitudes.System.Cache.Set(key, nil, time.Hour*20)
	solitudes.System.DB.Model(solitudes.Article{}).
		Where("slug = ?", c.Query("slug")).
		UpdateColumn("read_num", gorm.Expr("read_num + ?", 1))
}
