package main

import (
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"

	"cron-test/app/commands"
)

func main() {
	commands.NewApp().Run(os.Args[1:])
}
