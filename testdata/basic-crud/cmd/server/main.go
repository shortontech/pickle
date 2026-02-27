package main

import (
	"log"
	"net/http"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"

	pickle "github.com/shortontech/pickle/testdata/basic-crud/app/http"
	"github.com/shortontech/pickle/testdata/basic-crud/app/commands"
	"github.com/shortontech/pickle/testdata/basic-crud/app/models"
	"github.com/shortontech/pickle/testdata/basic-crud/config"
	"github.com/shortontech/pickle/testdata/basic-crud/routes"
)

func main() {
	config.Init()
	models.DB = config.Database.Open()

	registry := pickle.NewCommandRegistry()
	for _, cmd := range commands.Commands() {
		registry.Register(cmd)
	}
	if registry.HasCommand(os.Args[1:]) {
		if err := registry.Dispatch(os.Args[1:]); err != nil {
			log.Fatal(err)
		}
		return
	}

	mux := http.NewServeMux()
	routes.API.RegisterRoutes(mux)

	log.Printf("listening on :%s", config.App.Port)
	http.ListenAndServe(":"+config.App.Port, mux)
}
