package solitudes

import (
	"time"

	"github.com/jinzhu/gorm"
	cache "github.com/patrickmn/go-cache"
	"github.com/spf13/viper"
	"go.uber.org/dig"

	// - db driver
	_ "github.com/jinzhu/gorm/dialects/postgres"
)

func newCache() *cache.Cache {
	return cache.New(5*time.Minute, 10*time.Minute)
}

func newDatabase(conf *Config) *gorm.DB {
	db, err := gorm.Open("postgres", conf.Database)
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
	viper.AddConfigPath("resource/config")
	err := viper.ReadInConfig()
	if err != nil {
		panic(err)
	}
	var c Config
	viper.Unmarshal(&c)
	return &c
}

func migrate(db *gorm.DB) error {
	return db.AutoMigrate(User{},
		Label{}, Article{}, Comment{},
		ArticleLabel{}).Error
}

func provide() {
	err := Solitudes.Provide(newCache)
	if err != nil {
		panic(err)
	}
	err = Solitudes.Provide(newConfig)
	if err != nil {
		panic(err)
	}
	err = Solitudes.Provide(newDatabase)
	if err != nil {
		panic(err)
	}
}

func init() {
	Solitudes = dig.New()
	provide()
	if err := Solitudes.Invoke(migrate); err != nil {
		panic(err)
	}
}

// Solitudes 运行时依赖注入
var Solitudes *dig.Container
