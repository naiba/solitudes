package soligin

import (
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
