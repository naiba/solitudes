package wengine

import (
	"net/http"
	"strings"
	"time"

	"github.com/naiba/solitudes"
	"golang.org/x/crypto/bcrypt"

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
