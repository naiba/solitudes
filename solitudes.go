package solitudes

import (
	"time"

	"github.com/jinzhu/gorm"
	"github.com/patrickmn/go-cache"
	"github.com/spf13/viper"
	"github.com/yanyiwu/gojieba"
	"go.uber.org/dig"

	"github.com/blevesearch/bleve"
	// - gojieba bleve adapter
	_ "github.com/yanyiwu/gojieba/bleve"

	// - db driver
	_ "github.com/jinzhu/gorm/dialects/postgres"
)

func newBleveIndex() bleve.Index {
	dataPath := "data/bleve.article"
	index, err := bleve.Open(dataPath)
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
		index, err = bleve.New(dataPath, mapping)
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

func newDatabase(conf *Config) *gorm.DB {
	if conf.Web.Database == "" {
		return nil
	}
	db, err := gorm.Open("postgres", conf.Web.Database)
	if err != nil {
		panic(err)
	}
	if err = db.DB().Ping(); err != nil {
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
	viper.Unmarshal(&c)
	return &c
}

func newSystem(c *Config, d *gorm.DB, h *cache.Cache, s bleve.Index) *SysVeriable {
	return &SysVeriable{
		C: c,
		D: d,
		H: h,
		S: s,
	}
}

func migrate() {
	if err := System.D.AutoMigrate(Article{}, ArticleHistory{}, Comment{}).Error; err != nil {
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

func init() {
	Injector = dig.New()
	provide()
	if System.D != nil {
		migrate()
		// 重建索引
		BuildArticleIndex()
	}
}
