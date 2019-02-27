package solitudes

import (
	"time"

	"github.com/blevesearch/bleve"

	"github.com/jinzhu/gorm"
	"github.com/patrickmn/go-cache"
	"go.uber.org/dig"
)

const (
	// CtxAuthorized 用户已认证
	CtxAuthorized = "cazed"
	// CtxPassPreHandler 通过了PreHandler
	CtxPassPreHandler = "cpph"
	// CtxRequestParams 路由参数
	CtxRequestParams = "crp"
	// AuthCookie 用户认证使用的Cookie名
	AuthCookie = "i_like_solitude"
)

// SysVeriable 全局变量
type SysVeriable struct {
	C            *Config
	D            *gorm.DB
	H            *cache.Cache
	S            bleve.Index
	Token        string
	TokenExpires time.Time
}

// Injector 运行时依赖注入
var Injector *dig.Container

// System 全局变量
var System *SysVeriable

// Templates 文章模板
var Templates = map[byte]string{
	1: "Article template",
	2: "Page template",
}

// TemplateIndex 模板索引
var TemplateIndex = map[byte]string{
	1: "article",
	2: "page",
}
