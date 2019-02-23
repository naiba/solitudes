package wengine

import (
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/jinzhu/gorm"

	"github.com/biezhi/gorm-paginator/pagination"
	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/x/soligin"
	"github.com/naiba/solitudes/x/soliwriter"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/russross/blackfriday.v2"

	"github.com/gin-gonic/gin"
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
	if lf.Email != solitudes.System.C.Web.User.Email ||
		bcrypt.CompareHashAndPassword([]byte(solitudes.System.C.Web.User.Password),
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
	c.SetCookie(solitudes.AuthCookie, string(token), int(time.Hour*24*90), "/", solitudes.System.C.Web.Domain, false, false)
	c.Redirect(http.StatusFound, "/admin")
}

func login(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/login", gin.H{})
}

func logoutHandler(c *gin.Context) {
	if !strings.Contains(c.Request.Referer(), "://"+solitudes.System.C.Web.Domain+"/") {
		c.String(http.StatusOK, "CSRF protect")
		return
	}
	solitudes.System.TokenExpires = time.Now()
	solitudes.System.Token = ""
	c.Redirect(http.StatusFound, "/")
}

func index(c *gin.Context) {
	var as []solitudes.Article
	solitudes.System.D.Order("id DESC").Limit(10).Find(&as)
	c.HTML(http.StatusOK, "default/index", soligin.Soli(gin.H{
		"bio":      string(blackfriday.Run([]byte(solitudes.System.C.Web.Bio))),
		"articles": as,
	}))
}

func article(c *gin.Context) {
	slug := c.MustGet(solitudes.CtxRequestParams).([]string)
	var a solitudes.Article

	if err := solitudes.System.D.Where("slug = ?", slug[1]).First(&a).Error; err == gorm.ErrRecordNotFound {
		c.HTML(http.StatusNotFound, "default/404", soligin.Soli(gin.H{}))
		return
	} else if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	c.HTML(http.StatusOK, "default/"+solitudes.TemplateIndex[a.TemplateID], gin.H{
		"article": a,
	})
}

func archive(c *gin.Context) {
	pageSlice := c.MustGet(solitudes.CtxRequestParams).([]string)
	var page int64
	if len(pageSlice) == 2 {
		page, _ = strconv.ParseInt(pageSlice[1], 10, 32)
	}
	var articles []solitudes.Article
	pg := pagination.Paging(&pagination.Param{
		DB:      solitudes.System.D,
		Page:    int(page),
		Limit:   15,
		OrderBy: []string{"id desc"},
	}, &articles)
	c.HTML(http.StatusOK, "default/archive", soligin.Soli(gin.H{
		"articles": listArticleByYear(articles),
		"page":     pg,
	}))
}

func listArticleByYear(as []solitudes.Article) [][]solitudes.Article {
	var listed = make([][]solitudes.Article, 0)
	var lastYear int
	var listItem []solitudes.Article
	for i := 0; i < len(as); i++ {
		currentYear := as[i].UpdatedAt.Year()
		if currentYear != lastYear {
			if len(listItem) > 0 {
				listed = append(listed, listItem)
			}
			listItem = make([]solitudes.Article, 0)
			lastYear = currentYear
		}
		listItem = append(listItem, as[i])
	}
	if len(listItem) > 0 {
		listed = append(listed, listItem)
	}
	return listed
}

func static(root string) gin.HandlerFunc {
	return func(c *gin.Context) {
		i := strings.Index(c.Request.URL.Path[1:], "/")
		// 其实这边 gin 已经过滤了一遍了 我这边再过滤一下
		filepath := path.Clean(root + c.Request.URL.Path[i+1:])
		http.ServeFile(soliwriter.InterceptResponseWriter{
			ResponseWriter: c.Writer,
			ErrH: func(h http.ResponseWriter, s int) {
				h.Header().Set("Content-Type", "text/html")
				h.Header().Set("X-File-Server", "solitudes")
				c.HTML(s, "default/404", soligin.Soli(gin.H{}))
			},
		}, c.Request, filepath)
	}
}
