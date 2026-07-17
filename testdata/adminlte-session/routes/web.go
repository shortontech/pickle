package routes

import (
	pickle "github.com/shortontech/pickle/testdata/adminlte-session/app/http"
	"github.com/shortontech/pickle/testdata/adminlte-session/app/http/auth/session"
	"github.com/shortontech/pickle/testdata/adminlte-session/app/http/controllers"
	"github.com/shortontech/pickle/testdata/adminlte-session/app/http/middleware"
)

var Web = pickle.Routes(func(r *pickle.Router) {
	r.Get("/assets/:asset", controllers.DashboardController{}.Asset)
	r.Get("/login", controllers.SessionController{}.Create, session.CSRF)
	r.Post("/login", controllers.SessionController{}.Store, session.CSRF)
	r.Post("/logout", controllers.SessionController{}.Destroy, middleware.Auth, session.CSRF)
	r.Get("/", controllers.DashboardController{}.Index, middleware.Auth)
})
