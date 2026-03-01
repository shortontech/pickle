package routes

import (
	pickle "github.com/shortontech/pickle/testdata/basic-crud/app/http"
	"github.com/shortontech/pickle/testdata/basic-crud/app/http/controllers"
	"github.com/shortontech/pickle/testdata/basic-crud/app/http/middleware"
)

var API = pickle.Routes(func(r *pickle.Router) {
	r.Group("/api", func(r *pickle.Router) {
		r.Post("/login", controllers.AuthController{}.Login)
		r.Post("/users", controllers.UserController{}.Store)

		r.Group("/users", func(r *pickle.Router) {
			r.Get("/", controllers.UserController{}.Index)
			r.Get("/:id", controllers.UserController{}.Show)
			r.Put("/:id", controllers.UserController{}.Update)
			r.Delete("/:id", controllers.UserController{}.Destroy)
		}, middleware.Auth)

		r.Group("/posts", func(r *pickle.Router) {
			r.Get("/", controllers.PostController{}.Index)
			r.Get("/:id", controllers.PostController{}.Show)
			r.Post("/", controllers.PostController{}.Store)
			r.Put("/:id", controllers.PostController{}.Update)
			r.Delete("/:id", controllers.PostController{}.Destroy)
		}, middleware.Auth)
	})
})
