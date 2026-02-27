package middleware

import (
	pickle "github.com/shortontech/pickle/testdata/basic-crud/app/http"
	"github.com/shortontech/pickle/testdata/basic-crud/app/http/auth"
)

func Auth(ctx *pickle.Context, next func() pickle.Response) pickle.Response {
	info, err := auth.Authenticate(ctx.Request())
	if err != nil {
		return ctx.Unauthorized(err.Error())
	}
	ctx.SetAuth(info)
	return next()
}
