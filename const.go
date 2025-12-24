package solitudes

import (
	"github.com/blevesearch/bleve/v2"
	"github.com/patrickmn/go-cache"
	"go.uber.org/dig"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"github.com/naiba/solitudes/internal/model"
)

const (
	// CtxAuthorized 用户已认证
	CtxAuthorized = "cazed"
	// CtxTranslator 翻译
	CtxTranslator = "ct"
	// AuthCookie 用户认证使用的Cookie名
	AuthCookie = "i_like_solitude"
	// CacheKeyPrefixRelatedChapters 缓存键前缀：章节
	CacheKeyPrefixRelatedChapters = "ckprc"
	// CacheKeyPrefixRelatedArticle 缓存键前缀：文章
	CacheKeyPrefixRelatedArticle = "ckpra"
	// CacheKeyPrefixRelatedSiblingArticle 缓存键前缀：相邻文章
	CacheKeyPrefixRelatedSiblingArticle = "ckprsa"
)

// SysVeriable 全局变量
type SysVeriable struct {
	Config    *model.Config
	DB        *gorm.DB
	Cache     *cache.Cache
	Search    bleve.Index
	SafeCache *singleflight.Group
}

const fullTextSearchIndexPath = "data/bleve"

// Injector 运行时依赖注入
var Injector *dig.Container

// System 全局变量
var System *SysVeriable

// BuildVersion 构建版本
var BuildVersion = "_BuildVersion_"

const (
	// ArticleTemplateID represents the article template ID
	ArticleTemplateID byte = 1
	// PageTemplateID represents the page template ID
	PageTemplateID byte = 2
)

// Templates 文章模板
var Templates = map[byte]string{
	ArticleTemplateID: "Article template",
	PageTemplateID:    "Page template",
}

// TemplateIndex 模板索引
var TemplateIndex = map[byte]string{
	ArticleTemplateID: "article",
	PageTemplateID:    "page",
}

func init() {
	BuildVersion = BuildVersion[:8]
}
