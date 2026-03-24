package routes

import (
	pickle "monorepo/services/api/http"
	"monorepo/services/api/http/controllers"
	"monorepo/services/api/http/middleware"
)

var API = pickle.Routes(func(r *pickle.Router) {
	r.Group("/api", func(r *pickle.Router) {
		r.Get("/users", controllers.UserController{}.Index)
		r.Get("/users/:id", controllers.UserController{}.Show)

		r.Group("/orders", func(r *pickle.Router) {
			r.Get("/", controllers.OrderController{}.Index)
			r.Post("/", controllers.OrderController{}.Store)
		}, middleware.Auth)
	})
})
