package soligin

import (
	"net/http"
	"time"

	csrf "github.com/utrack/gin-csrf"

	"github.com/gin-gonic/gin"
	"github.com/naiba/solitudes"
)

// Soli 输出共同的参数
func Soli(c *gin.Context, protect bool, data map[string]interface{}) gin.H {
	var title, keywords, desc string

	// custom title
	if k, ok := data["title"]; ok && k.(string) != "" {
		title = data["title"].(string) + " | " + solitudes.System.Config.SpaceName
	} else {
		title = solitudes.System.Config.SpaceName
	}
	// custom keywords
	if k, ok := data["keywords"]; ok && k.(string) != "" {
		keywords = data["keywords"].(string)
	} else {
		keywords = solitudes.System.Config.Web.SpaceKeywords
	}
	// custom desc
	if k, ok := data["desc"]; ok && k.(string) != "" {
		desc = data["desc"].(string)
	} else {
		desc = solitudes.System.Config.SpaceDesc
	}

	var soli = make(map[string]interface{})
	soli["Conf"] = solitudes.System.Config
	soli["Title"] = title
	soli["Keywords"] = keywords
	soli["BuildVersion"] = solitudes.BuildVersion
	soli["Desc"] = desc
	soli["Login"], _ = c.Get(solitudes.CtxAuthorized)
	soli["Data"] = data
	soli["Tr"] = c.MustGet(solitudes.CtxTranslator).(*solitudes.Translator)

	if protect {
		soli["CSRF"] = csrf.GetToken(c)
	}

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
			c.Redirect(http.StatusFound, "/admin/")
			c.Set(solitudes.CtxPassPreHandler, false)
			return
		} else if lo.NeedLogin && !c.MustGet(solitudes.CtxAuthorized).(bool) {
			c.Redirect(http.StatusFound, "/login")
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
