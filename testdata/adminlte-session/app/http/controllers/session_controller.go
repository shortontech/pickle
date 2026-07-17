package controllers

import (
	pickle "github.com/shortontech/pickle/testdata/adminlte-session/app/http"
	"github.com/shortontech/pickle/testdata/adminlte-session/app/http/auth/session"
)

type SessionController struct{ pickle.Controller }

// Store is intentionally a deterministic test-only login. The fixture proves
// session creation/cookies without pretending this is production credential
// verification.
func (SessionController) Store(ctx *pickle.Context) pickle.Response {
	cookies, err := session.Create(ctx, "admin@example.test", "admin")
	if err != nil {
		return ctx.Error(err)
	}
	return cookies.Apply(ctx.NoContent())
}

func (SessionController) Destroy(ctx *pickle.Context) pickle.Response {
	response, err := session.Destroy(ctx)
	if err != nil {
		return ctx.Error(err)
	}
	return response
}
