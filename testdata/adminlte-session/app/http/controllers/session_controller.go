package controllers

import (
	pickle "github.com/shortontech/pickle/testdata/adminlte-session/app/http"
	"github.com/shortontech/pickle/testdata/adminlte-session/app/http/auth/session"
)

type SessionController struct{ pickle.Controller }

func (SessionController) Create(ctx *pickle.Context) pickle.Response {
	data := pickle.LoginData{}
	data.Page.Title = "Sign in"
	data.Page.Heading = "AdminLTE session demo"
	return pickle.Login(ctx, data)
}

// Store is intentionally a deterministic test-only login. The fixture proves
// session creation/cookies without pretending this is production credential
// verification.
func (SessionController) Store(ctx *pickle.Context) pickle.Response {
	cookies, err := session.Create(ctx, "018f0f4d-7b2a-7c26-8000-000000000001", "admin")
	if err != nil {
		return ctx.Error(err)
	}
	return cookies.Apply(ctx.Redirect("/"))
}

func (SessionController) Destroy(ctx *pickle.Context) pickle.Response {
	response, err := session.Destroy(ctx)
	if err != nil {
		return ctx.Error(err)
	}
	response.StatusCode = 303
	return response.Header("Location", "/login").Header("Cache-Control", "no-store")
}
