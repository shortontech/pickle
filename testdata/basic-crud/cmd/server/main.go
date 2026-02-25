package main

import (
	"log"
	"net/http"

	"github.com/shortontech/pickle/testdata/basic-crud/app/models"
	"github.com/shortontech/pickle/testdata/basic-crud/config"
	"github.com/shortontech/pickle/testdata/basic-crud/routes"
)

func main() {
	config.Init()
	models.DB = config.Database.Open()

	mux := http.NewServeMux()
	routes.API.RegisterRoutes(mux)

	log.Printf("listening on :%s", config.App.Port)
	http.ListenAndServe(":"+config.App.Port, mux)
}
