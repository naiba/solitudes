package solitudes

import (
	"log"
	"os"
	"sync"
	"time"

	"github.com/panjf2000/ants"

	"github.com/jinzhu/gorm"
	"github.com/patrickmn/go-cache"
	"github.com/spf13/viper"
	"go.uber.org/dig"

	"github.com/blevesearch/bleve"
	"github.com/yanyiwu/gojieba"

	// - bleve adapter
	_ "github.com/yanyiwu/gojieba/bleve"

	// - db driver
	_ "github.com/jinzhu/gorm/dialects/postgres"
)

const fullTextSearchIndexDir = "data/bleve.article"

func newBleveIndex() bleve.Index {
	index, err := bleve.Open(fullTextSearchIndexDir)
	if err == bleve.ErrorIndexPathDoesNotExist {
		mapping := bleve.NewIndexMapping()
		err := mapping.AddCustomTokenizer("gojieba",
			map[string]interface{}{
				"dictpath":     gojieba.DICT_PATH,
				"hmmpath":      gojieba.HMM_PATH,
				"userdictpath": gojieba.USER_DICT_PATH,
				"idf":          gojieba.IDF_PATH,
				"stop_words":   gojieba.STOP_WORDS_PATH,
				"type":         "gojieba",
			},
		)
		if err != nil {
			panic(err)
		}
		err = mapping.AddCustomAnalyzer("gojieba",
			map[string]interface{}{
				"type":      "gojieba",
				"tokenizer": "gojieba",
			},
		)
		if err != nil {
			panic(err)
		}
		mapping.DefaultAnalyzer = "gojieba"
		index, err = bleve.New(fullTextSearchIndexDir, mapping)
		if err != nil {
			panic(err)
		}
	} else if err != nil {
		panic(err)
	}

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
	if conf.Web.Database == "" {
		return nil
	}
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
		newBleveIndex,
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
	if err := System.Search.Close(); err != nil {
		panic(err)
	}
	if err := os.RemoveAll(fullTextSearchIndexDir); err != nil {
		panic(err)
	}
	System.Search = newBleveIndex()
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
		err := System.Search.Index(as[i].GetIndexID(), as[i].ToIndexData())
		if err != nil {
			panic(err)
		}
	}
	for i := 0; i < len(hs); i++ {
		err := System.Search.Index(hs[i].GetIndexID(), hs[i].ToIndexData())
		if err != nil {
			panic(err)
		}
	}
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
