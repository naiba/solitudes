package solitudes

import (
	"time"

	"github.com/jinzhu/gorm"
	"github.com/patrickmn/go-cache"
	"github.com/spf13/viper"
	"go.uber.org/dig"

	// - db driver
	_ "github.com/jinzhu/gorm/dialects/postgres"
)

func newCache() *cache.Cache {
	return cache.New(5*time.Minute, 10*time.Minute)
}

func newDatabase(conf *Config) *gorm.DB {
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
	viper.AddConfigPath("resource/data/")
	err := viper.ReadInConfig()
	if err != nil {
		panic(err)
	}
	var c Config
	viper.Unmarshal(&c)
	return &c
}

func newSystem(c *Config, d *gorm.DB, h *cache.Cache) *SysVeriable {
	return &SysVeriable{
		C: c,
		D: d,
		H: h,
	}
}

func migrate() {
	if err := System.D.AutoMigrate(Article{}, Comment{}).Error; err != nil {
		panic(err)
	}
}

func provide() {
	err := Injector.Provide(newCache)
	if err != nil {
		panic(err)
	}
	err = Injector.Provide(newConfig)
	if err != nil {
		panic(err)
	}
	err = Injector.Provide(newDatabase)
	if err != nil {
		panic(err)
	}
	err = Injector.Provide(newSystem)
	if err != nil {
		panic(err)
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
	migrate()
}

// SysVeriable 全局变量
type SysVeriable struct {
	C            *Config
	D            *gorm.DB
	H            *cache.Cache
	Token        string
	TokenExpires time.Time
}

// Injector 运行时依赖注入
var Injector *dig.Container

// System 全局变量
var System *SysVeriable

// Templates 文章模板
var Templates = map[int]string{
	1: "Article template",
	2: "Page template",
}
