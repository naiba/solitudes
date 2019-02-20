package main

import (
	"log"

	"github.com/naiba/solitudes"
)

func init() {
}

func main() {
	solitudes.Solitudes.Invoke(func(cf *solitudes.Config) {
		log.Println("[System config]", cf)
	})
}
