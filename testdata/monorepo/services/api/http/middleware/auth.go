package middleware

import pickle "monorepo/services/api/http"

func Auth(ctx *pickle.Context, next func() pickle.Response) pickle.Response {
	token := ctx.BearerToken()
	if token == "" {
		return ctx.Unauthorized("missing token")
	}
	ctx.SetAuth(&pickle.AuthInfo{UserID: "test-user-id"})
	return next()
}
