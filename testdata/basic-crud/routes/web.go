package routes

import (
	pickle "github.com/shortontech/pickle/testdata/basic-crud/app/http"
	"github.com/shortontech/pickle/testdata/basic-crud/app/http/controllers"
	"github.com/shortontech/pickle/testdata/basic-crud/app/http/middleware"
)

var API = pickle.Routes(func(r *pickle.Router) {
	r.Group("/api", func(r *pickle.Router) {
		r.Resource("/users", controllers.UserController{})

		r.Group("/posts", middleware.Auth, func(r *pickle.Router) {
			r.Get("/", controllers.PostController{}.Index)
			r.Get("/:id", controllers.PostController{}.Show)
			r.Post("/", controllers.PostController{}.Store)
			r.Put("/:id", controllers.PostController{}.Update)
			r.Delete("/:id", controllers.PostController{}.Destroy)
		})
	})
})
