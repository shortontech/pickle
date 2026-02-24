package basiccrud

func Auth(ctx *Context, next func() Response) Response {
	token := ctx.BearerToken()
	if token == "" {
		return ctx.Unauthorized("missing token")
	}

	// TODO: validate token and set auth info
	return next()
}
