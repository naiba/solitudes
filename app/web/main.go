package main

import (
	"log"
	"time"

	"github.com/google/wire"

	"github.com/jinzhu/gorm"
	"github.com/naiba/solitudes"
	"github.com/patrickmn/go-cache"
	"github.com/spf13/viper"

	// - db driver
	_ "github.com/jinzhu/gorm/dialects/postgres"
)

func init() {
	solitudes.System = solitudes.NewSolitudes(wire.NewSet(newCache, newConfig, newDatabase))
}

func main() {
	log.Println(solitudes.System)
}

func newCache() *cache.Cache {
	return cache.New(5*time.Minute, 10*time.Minute)
}

func newDatabase(conf *solitudes.Config) *gorm.DB {
	db, err := gorm.Open("postgres", conf.Database)
	if err != nil {
		panic(err)
	}
	return db
}

func newConfig() *solitudes.Config {
	viper.SetConfigName("conf")
	viper.AddConfigPath("resource/config")
	err := viper.ReadInConfig()
	if err != nil {
		panic(err)
	}
	var c solitudes.Config
	viper.Unmarshal(&c)
	return &c
}
