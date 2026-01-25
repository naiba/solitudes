package solitudes

import (
	"log"
	"os"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/patrickmn/go-cache"
	"github.com/robfig/cron/v3"
	"github.com/yanyiwu/gojieba"
	"go.uber.org/dig"
	"golang.org/x/sync/singleflight"
	"gopkg.in/yaml.v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/naiba/solitudes/internal/model"
	"github.com/naiba/solitudes/internal/theme"
	_ "github.com/naiba/solitudes/pkg/blevejieba"
)

// Constants
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

func newBleveSearch() bleve.Index {
	_, err := os.Stat(fullTextSearchIndexPath)
	var index bleve.Index
	if err != nil {
		mapping := bleve.NewIndexMapping()
		mapping.DefaultAnalyzer = "jieba"
		if err := mapping.AddCustomTokenizer("jieba", map[string]interface{}{
			"type":         "jieba",
			"useHmm":       true,
			"tokenizeMode": float64(gojieba.SearchMode),
		}); err != nil {
			panic(err)
		}
		if err := mapping.AddCustomAnalyzer("jieba", map[string]interface{}{
			"type":      "jieba",
			"tokenizer": "jieba",
		}); err != nil {
			panic(err)
		}
		index, err = bleve.New(fullTextSearchIndexPath, mapping)
		if err != nil {
			panic(err)
		}
	} else {
		index, err = bleve.Open(fullTextSearchIndexPath)
		if err != nil {
			panic(err)
		}
	}
	count, err := index.DocCount()
	log.Println("Bleve: DocCount", count, err)
	return index
}

func newCache() *cache.Cache {
	return cache.New(5*time.Minute, 10*time.Minute)
}

func newDatabase(conf *model.Config) *gorm.DB {
	db, err := gorm.Open(postgres.Open(conf.Database), &gorm.Config{})
	if err != nil {
		log.Println(conf)
		panic(err)
	}
	if conf.Debug {
		db = db.Debug()
	}
	return db
}

func newConfig() *model.Config {
	configFile := "data/conf.yml"
	content, err := os.ReadFile(configFile)
	if err != nil {
		panic(err)
	}
	var c model.Config
	err = yaml.Unmarshal(content, &c)
	if err != nil {
		panic(err)
	}
	c.ConfigFilePath = configFile

	themesRoot := "resource/themes"
	themes, err := theme.LoadThemes(themesRoot)
	if err != nil {
		log.Printf("load themes failed: %v", err)
		themes = &theme.ThemeList{}
	}

	availableSite := make(map[string]bool)
	for _, meta := range themes.Site {
		if meta.ID == "" {
			continue
		}
		availableSite[meta.ID] = true
	}
	availableAdmin := make(map[string]bool)
	for _, meta := range themes.Admin {
		if meta.ID == "" {
			continue
		}
		availableAdmin[meta.ID] = true
	}

	model.ApplyThemeFallback(&c, "cactus", "default", availableSite, availableAdmin)

	// 校验主题配置
	model.ValidateThemeConfigStartup(&c, themes)

	// 同步主题配置
	model.SyncThemeConfig(&c, themes)

	log.Printf("Config: %+v", c)
	return &c
}

func newSystem(c *model.Config, d *gorm.DB, h *cache.Cache,
	s bleve.Index) *SysVeriable {
	return &SysVeriable{
		Config:    c,
		DB:        d,
		Cache:     h,
		Search:    s,
		SafeCache: new(singleflight.Group),
	}
}

func migrate() {
	if err := System.DB.Exec("CREATE EXTENSION IF NOT EXISTS \"uuid-ossp\";").Error; err != nil {
		panic(err)
	}
	if err := System.DB.AutoMigrate(&model.Article{}, &model.ArticleHistory{}, &model.Comment{}, &model.User{}, &model.FeedVisit{}); err != nil {
		panic(err)
	}
}

func cleanOldFeedVisits() {
	result := System.DB.Where("created_at < ?", time.Now().AddDate(0, 0, -7)).Delete(&model.FeedVisit{})
	if result.Error != nil {
		log.Printf("Failed to clean old feed visits: %v", result.Error)
	} else {
		log.Printf("Cleaned %d old feed visit records", result.RowsAffected)
	}
}

func provide() {
	var providers = []interface{}{
		newCache,
		newConfig,
		newDatabase,
		newSystem,
		newBleveSearch,
	}
	var err error
	for _, provider := range providers {
		err = Injector.Provide(provider)
		if err != nil {
			panic(err)
		}
	}
	err = Injector.Invoke(func(s *SysVeriable) {
		System = s
	})
	if err != nil {
		panic(err)
	}
}

// BuildArticleIndex 重建索引
func BuildArticleIndex() {
	System.Search.Close()
	if err := os.RemoveAll(fullTextSearchIndexPath); err != nil {
		panic(err)
	}
	System.Search = newBleveSearch()
	var as []model.Article
	var hs []model.ArticleHistory
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		System.DB.Find(&as)
	}()
	go func() {
		defer wg.Done()
		System.DB.Preload("Article").Find(&hs)
	}()
	wg.Wait()
	for i := range as {
		System.Search.Index(as[i].GetIndexID(), as[i])
	}
	for i := range hs {
		System.Search.Index(hs[i].GetIndexID(), hs[i])
	}
	num, err := System.Search.DocCount()
	log.Printf("Doc indexed %d %+v\n", num, err)
}

func Init() {
	BuildVersion = BuildVersion[:8]
	Injector = dig.New()
	provide()
	if System.DB != nil {
		migrate()
	}

	c := cron.New()
	_, err := c.AddFunc("0 0 * * *", cleanOldFeedVisits)
	if err != nil {
		log.Printf("Failed to start cron job: %v", err)
	} else {
		c.Start()
		log.Println("Cron job started: cleanOldFeedVisits at 00:00 daily")
	}
}
