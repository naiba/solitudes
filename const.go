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

	// CacheKeyPrefixRelatedChapters 缓存键前缀：取章节
	CacheKeyPrefixRelatedChapters = "ckprc"
	// CacheKeyPrefixRelatedBook 缓存键前缀：取书本信息
	CacheKeyPrefixRelatedBook = "ckprb"
	// CacheKeyPrefixRelatedSiblingArticle 缓存键前缀：取相邻文章
	CacheKeyPrefixRelatedSiblingArticle = "ckprsa"
)

// SysVeriable 全局变量
type SysVeriable struct {
	Config       *Config
	DB           *gorm.DB
	Cache        *cache.Cache
	Search       bleve.Index
	SafeCache    *SafeCache
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
