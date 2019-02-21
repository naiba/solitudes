package main

import "github.com/naiba/solitudes/wengine"

func main() {
	if err := wengine.WEngine(); err != nil {
		panic(err)
	}
}
