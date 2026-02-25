package middleware

import pickle "github.com/shortontech/pickle/testdata/basic-crud/app/http"

func Auth(ctx *pickle.Context, next func() pickle.Response) pickle.Response {
	token := ctx.BearerToken()
	if token == "" {
		return ctx.Unauthorized("missing token")
	}

	// TODO: validate token and set auth info
	return next()
}
