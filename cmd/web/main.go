package main

import (
	"os"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/router"
)

func main() {
	solitudes.Init()
	if _, err := os.Stat("data/upload"); os.IsNotExist(err) {
		err = os.Mkdir("data/upload", os.ModeDir|os.ModePerm)
		if err != nil {
			panic(err)
		}
	}
	router.Serve()
}
