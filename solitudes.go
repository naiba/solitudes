package solitudes

import (
	"log"
	"os"
	"sync"
	"time"

	"github.com/blevesearch/bleve"
	"github.com/panjf2000/ants"
	"github.com/yanyiwu/gojieba"

	"github.com/jinzhu/gorm"
	"github.com/patrickmn/go-cache"
	"github.com/spf13/viper"
	"go.uber.org/dig"

	// db driver
	_ "github.com/jinzhu/gorm/dialects/postgres"

	// gojirba
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

func newSafeCache() *SafeCache {
	return &SafeCache{
		List: make(map[string]*sync.Cond),
	}
}

func newPool() *ants.Pool {
	p, err := ants.NewPool(20000)
	if err != nil {
		panic(err)
	}
	return p
}

func newDatabase(conf *Config) *gorm.DB {
	db, err := gorm.Open("postgres", conf.Web.Database)
	if err != nil {
		panic(err)
	}
	if conf.Debug {
		db = db.Debug()
	}
	return db
}

func newConfig() *Config {
	viper.SetConfigName("conf")
	viper.SetConfigType("yml")
	viper.AddConfigPath("data/")
	err := viper.ReadInConfig()
	if err != nil {
		panic(err)
	}
	var c Config
	if err = viper.Unmarshal(&c); err != nil {
		panic(err)
	}
	return &c
}

func newSystem(c *Config, d *gorm.DB, h *cache.Cache, sc *SafeCache,
	s bleve.Index, p *ants.Pool) *SysVeriable {
	return &SysVeriable{
		Config:    c,
		DB:        d,
		Cache:     h,
		Search:    s,
		SafeCache: sc,
		Pool:      p,
	}
}

func migrate() {
	if err := System.DB.AutoMigrate(Article{}, ArticleHistory{}, Comment{}).Error; err != nil {
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
		newSafeCache,
		newPool,
	}
	var err error
	for i := 0; i < len(providers); i++ {
		err = Injector.Provide(providers[i])
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
	var as []Article
	var hs []ArticleHistory
	var wg sync.WaitGroup
	wg.Add(2)
	checkPoolSubmit(&wg, System.Pool.Submit(func() {
		System.DB.Find(&as)
		wg.Done()
	}))
	checkPoolSubmit(&wg, System.Pool.Submit(func() {
		System.DB.Preload("Article").Find(&hs)
		wg.Done()
	}))
	wg.Wait()
	for i := 0; i < len(as); i++ {
		System.Search.Index(as[i].GetIndexID(), as[i])
	}
	for i := 0; i < len(hs); i++ {
		System.Search.Index(hs[i].GetIndexID(), hs[i])
	}
	num, err := System.Search.DocCount()
	log.Printf("Doc indexed %d %+v\n", num, err)
}

func checkPoolSubmit(wg *sync.WaitGroup, err error) {
	if err != nil {
		log.Println(err)
		if wg != nil {
			wg.Done()
		}
	}
}

func init() {
	Injector = dig.New()
	provide()
	if System.DB != nil {
		migrate()
	}
}
