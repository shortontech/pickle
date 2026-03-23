package routes

import (
	pickle "cron-test/app/http"
	"cron-test/app/http/controllers"
)

var API = pickle.Routes(func(r *pickle.Router) {
	r.Group("/api", func(r *pickle.Router) {
		r.Get("/", controllers.WelcomeController{}.Index)
	})
})
