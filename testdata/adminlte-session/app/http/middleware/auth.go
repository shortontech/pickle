package middleware

import (
	pickle "github.com/shortontech/pickle/testdata/adminlte-session/app/http"
	"github.com/shortontech/pickle/testdata/adminlte-session/app/http/auth"
)

func Auth(ctx *pickle.Context, next func() pickle.Response) pickle.Response {
	info, err := auth.Authenticate(ctx.Request())
	if err != nil {
		return ctx.RedirectToRoute("auth.login", nil)
	}
	ctx.SetAuth(info)
	return next()
}
