package main

import (
	"os"

	"github.com/naiba/solitudes/wengine"
)

func main() {
	if _, err := os.Stat("data/upload"); os.IsNotExist(err) {
		err = os.Mkdir("data/upload", os.ModeDir|os.ModePerm)
		if err != nil {
			panic(err)
		}
	}
	if err := wengine.WEngine(); err != nil {
		panic(err)
	}
}
