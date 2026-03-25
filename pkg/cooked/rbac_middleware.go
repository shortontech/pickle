package cooked

// RequireRole returns middleware that checks if the user has any of the given roles.
// Returns 403 Forbidden if the user lacks all specified roles.
func RequireRole(roles ...string) MiddlewareFunc {
	return func(ctx *Context, next func() Response) Response {
		if !ctx.HasAnyRole(roles...) {
			return ctx.Forbidden("insufficient role")
		}
		return next()
	}
}

// RequireAdmin returns middleware that checks if the user has a Manages role.
// Returns 403 Forbidden if the user is not an admin.
func RequireAdmin(ctx *Context, next func() Response) Response {
	if !ctx.IsAdmin() {
		return ctx.Forbidden("admin access required")
	}
	return next()
}
