package main

import (
	"os"

	"github.com/naiba/solitudes/wengine"
)

func main() {
	if _, err := os.Stat("data/upload"); os.IsNotExist(err) {
		os.Mkdir("data/upload", os.ModeDir|os.ModePerm)
	}
	if err := wengine.WEngine(); err != nil {
		panic(err)
	}
}
