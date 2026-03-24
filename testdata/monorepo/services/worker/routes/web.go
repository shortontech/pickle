package routes

import (
	pickle "monorepo/services/worker/http"
	"monorepo/services/worker/http/controllers"
	"monorepo/services/worker/http/middleware"
)

var API = pickle.Routes(func(r *pickle.Router) {
	r.Group("/api/jobs", func(r *pickle.Router) {
		r.Get("/", controllers.JobController{}.Index)
		r.Get("/:id", controllers.JobController{}.Show)
	}, middleware.Auth)
})
