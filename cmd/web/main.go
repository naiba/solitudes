package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/naiba/solitudes"
	"github.com/naiba/solitudes/pkg/translator"
	"github.com/naiba/solitudes/router"
)

func main() {
	if len(os.Args) > 1 {
		if os.Args[1] != "init-admin" {
			fmt.Fprintln(os.Stderr, "Usage: solitudes [init-admin --email you@example.com --nickname Administrator]")
			os.Exit(2)
		}
		flags := flag.NewFlagSet("init-admin", flag.ExitOnError)
		email := flags.String("email", "", "administrator email")
		nickname := flags.String("nickname", "Administrator", "public display name")
		flags.Parse(os.Args[2:])
		if flags.NArg() != 0 {
			fmt.Fprintln(os.Stderr, "init-admin does not accept positional arguments")
			os.Exit(2)
		}
		password, err := solitudes.CreateInitialAdministrator(*email, *nickname)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Administrator initialization failed:", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stdout, "Administrator created. Store this password securely; it is shown only once:")
		fmt.Fprintln(os.Stdout, password)
		fmt.Fprintln(os.Stdout, "Start the server, sign in at /login, and change the password in /account.")
		return
	}
	solitudes.Init()
	translator.Init()
	if _, err := os.Stat("data/upload"); os.IsNotExist(err) {
		err = os.Mkdir("data/upload", os.ModeDir|os.ModePerm)
		if err != nil {
			panic(err)
		}
	}
	router.Serve()
}
