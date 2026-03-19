package main

import (
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/shortontech/ledger/app/commands"
)

func main() {
	commands.NewApp().Run(os.Args[1:])
}
