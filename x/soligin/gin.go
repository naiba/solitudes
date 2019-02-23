package soligin

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/naiba/solitudes"
)

// Soli 输出共同的参数
func Soli(data map[string]interface{}) gin.H {
	var soli = make(map[string]interface{})
	soli["Conf"] = solitudes.System.C
	soli["Data"] = data
	return soli
}

// Authorize 用户认证中间件
func Authorize(c *gin.Context) {
	c.Set(solitudes.CtxPassPreHandler, true)
	token, _ := c.Cookie(solitudes.AuthCookie)
	if len(token) > 0 && token == solitudes.System.Token && solitudes.System.TokenExpires.After(time.Now()) {
		c.Set(solitudes.CtxAuthorized, true)
	} else {
		c.Set(solitudes.CtxAuthorized, false)
	}
}

// LimitOption 限制设置
type LimitOption struct {
	NeedLogin bool
	NeedGuest bool
}

// Limit 访问限制中间件
func Limit(lo LimitOption) gin.HandlerFunc {
	return func(c *gin.Context) {
		if lo.NeedGuest && c.MustGet(solitudes.CtxAuthorized).(bool) {
			c.Redirect(http.StatusFound, "/")
			c.Abort()
			c.Set(solitudes.CtxPassPreHandler, false)
			return
		} else if lo.NeedLogin && !c.MustGet(solitudes.CtxAuthorized).(bool) {
			c.Redirect(http.StatusFound, "/login/")
			c.Abort()
			c.Set(solitudes.CtxPassPreHandler, false)
			return
		}
		c.Set(solitudes.CtxPassPreHandler, true)
	}
}

// SetNoCache 此页面不准缓存
func SetNoCache(c *gin.Context) {
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
}
