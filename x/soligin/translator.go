package soligin

import (
	"github.com/naiba/solitudes"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/locales"
	"github.com/go-playground/pure"
)

// Translator 翻译中间件
func Translator(c *gin.Context) {
	t, _ := solitudes.Trans.FindTranslator(pure.AcceptedLanguages(c.Request)...)
	c.Set(solitudes.CtxTranslator, &solitudes.Translator{Trans: t, Translator: t.(locales.Translator)})
}
