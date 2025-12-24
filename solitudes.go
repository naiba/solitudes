package solitudes

import (
	"log"
	"os"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/patrickmn/go-cache"
	"github.com/yanyiwu/gojieba"
	"go.uber.org/dig"
	"golang.org/x/sync/singleflight"
	"gopkg.in/yaml.v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/naiba/solitudes/internal/model"
	_ "github.com/naiba/solitudes/pkg/blevejieba"
)

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
	log.Println("Config", c)
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
	if err := System.DB.AutoMigrate(&model.Article{}, &model.ArticleHistory{}, &model.Comment{}, &model.User{}); err != nil {
		panic(err)
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

func init() {
	Injector = dig.New()
	provide()
	if System.DB != nil {
		migrate()
	}
}
