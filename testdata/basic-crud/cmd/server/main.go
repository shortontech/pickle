package main

import (
	"log"
	"net/http"

	basiccrud "github.com/shortontech/pickle/testdata/basic-crud"
	"github.com/shortontech/pickle/testdata/basic-crud/config"
	"github.com/shortontech/pickle/testdata/basic-crud/models"
)

func main() {
	config.Init()
	models.DB = config.Database.Open()

	mux := http.NewServeMux()
	basiccrud.API.RegisterRoutes(mux)

	log.Printf("listening on :%s", config.App.Port)
	http.ListenAndServe(":"+config.App.Port, mux)
}
