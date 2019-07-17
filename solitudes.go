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

	"github.com/go-ego/riot"
	"github.com/go-ego/riot/types"

	// - db driver
	_ "github.com/jinzhu/gorm/dialects/postgres"
)

const fullTextSearchIndexDir = "data/roitIndex"

func newRiotSearch() *riot.Engine {
	searcher := &riot.Engine{}
	opts := types.EngineOpts{
		Using: 1,
		IndexerOpts: &types.IndexerOpts{
			IndexType: types.DocIdsIndex,
		},
		UseStore:      true,
		StoreFolder:   fullTextSearchIndexDir,
		GseDict:       "./dict/dictionary.txt",
		StopTokenFile: "./dict/stop_tokens.txt",
		StoreEngine:   "bg", // bg: badger, lbd: leveldb, bolt: bolt
	}
	searcher.Init(opts)
	searcher.Flush()
	log.Println("RIOT: recover index number", searcher.NumDocsIndexed())
	searcher.Close()
	return searcher
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
	s *riot.Engine, p *ants.Pool) *SysVeriable {
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
		newRiotSearch,
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
	if err := os.RemoveAll(fullTextSearchIndexDir); err != nil {
		panic(err)
	}
	System.Search = newRiotSearch()
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
		System.Search.Index(as[i].GetIndexID(), as[i].ToIndexData())
	}
	for i := 0; i < len(hs); i++ {
		System.Search.Index(hs[i].GetIndexID(), hs[i].ToIndexData())
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
