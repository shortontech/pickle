package routes

import (
	pickle "github.com/shortontech/pickle/testdata/adminlte-session/app/http"
	"github.com/shortontech/pickle/testdata/adminlte-session/app/http/auth/session"
	"github.com/shortontech/pickle/testdata/adminlte-session/app/http/controllers"
	"github.com/shortontech/pickle/testdata/adminlte-session/app/http/middleware"
)

var Web = pickle.Routes(func(r *pickle.Router) {
	r.Get("/assets/:asset", controllers.DashboardController{}.Asset)
	r.Get("/login", controllers.SessionController{}.Create, session.CSRF).Name("login")
	r.Post("/login", controllers.SessionController{}.Store, session.CSRF).Name("login.store")
	r.Post("/logout", controllers.SessionController{}.Destroy, middleware.Auth, session.CSRF).Name("logout")
	r.Get("/", controllers.DashboardController{}.Index, middleware.Auth).Name("dashboard")
})
