package middleware

import pickle "github.com/shortontech/pickle/testdata/encryption-test/app/http"

func Auth(ctx *pickle.Context, next func() pickle.Response) pickle.Response {
	token := ctx.BearerToken()
	if token == "" {
		return ctx.Unauthorized("missing token")
	}
	return next()
}
