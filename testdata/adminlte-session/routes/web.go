package routes

import (
	pickle "github.com/shortontech/pickle/testdata/adminlte-session/app/http"
	"github.com/shortontech/pickle/testdata/adminlte-session/app/http/controllers"
	"github.com/shortontech/pickle/testdata/adminlte-session/app/http/middleware"
)

var Web = pickle.Routes(func(r *pickle.Router) {
	r.Post("/login", controllers.SessionController{}.Store)
	r.Post("/logout", controllers.SessionController{}.Destroy, middleware.Auth)
	r.Get("/", controllers.DashboardController{}.Index, middleware.Auth)
})
