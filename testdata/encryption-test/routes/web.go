package routes

import (
	pickle "github.com/shortontech/pickle/testdata/encryption-test/app/http"
	"github.com/shortontech/pickle/testdata/encryption-test/app/http/controllers"
)

var API = pickle.Routes(func(r *pickle.Router) {
	r.Group("/api", func(r *pickle.Router) {
		r.Get("/users", controllers.UserController{}.Index)
		r.Get("/users/:id", controllers.UserController{}.Show)
	})
})
