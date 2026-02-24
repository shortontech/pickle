package main

import (
	"fmt"
	"net/http"

	basiccrud "github.com/shortontech/pickle/testdata/basic-crud"
)

func main() {
	mux := http.NewServeMux()
	basiccrud.API.RegisterRoutes(mux)

	fmt.Println("listening on :9999")
	http.ListenAndServe(":9999", mux)
}
