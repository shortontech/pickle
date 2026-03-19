package routes

import (
	pickle "github.com/shortontech/ledger/app/http"
	"github.com/shortontech/ledger/app/http/controllers"
	"github.com/shortontech/ledger/app/http/middleware"
)

var API = pickle.Routes(func(r *pickle.Router) {
	r.Group("/api", func(r *pickle.Router) {
		r.Post("/auth/login", controllers.AuthController{}.Login)

		r.Group("/accounts", func(r *pickle.Router) {
			r.Get("/", controllers.AccountController{}.Index)
			r.Post("/", controllers.AccountController{}.Store)
			r.Get("/:id", controllers.AccountController{}.Show)
			r.Get("/:id/balance", controllers.AccountController{}.Balance)
			r.Put("/:id", controllers.AccountController{}.Update)
			r.Delete("/:id", controllers.AccountController{}.Destroy)
		}, middleware.Auth)

		r.Group("/:account_id/transactions", func(r *pickle.Router) {
			r.Get("/", controllers.TransactionController{}.Index)
			r.Post("/", controllers.TransactionController{}.Store)
			r.Get("/:id", controllers.TransactionController{}.Show)
			r.Post("/:id/reverse", controllers.TransactionController{}.Reverse)
		}, middleware.Auth)
	})
})
