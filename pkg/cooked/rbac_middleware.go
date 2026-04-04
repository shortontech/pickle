package cooked

import "errors"

var errNoRoleLoader = errors.New("pickle: RoleLoaderFunc not configured — ensure RBAC generation ran")

// RoleLoaderFunc queries the database for the authenticated user's roles.
// The generated code sets this to a function that queries role_user JOIN roles.
// Returns a slice of RoleInfo for the given user ID.
var RoleLoaderFunc func(userID string) ([]RoleInfo, error)

// LoadRoles is middleware that queries the database for the authenticated user's
// roles and populates the context. Must run after Auth middleware.
func LoadRoles(ctx *Context, next func() Response) Response {
	if ctx.auth == nil {
		return ctx.Unauthorized("LoadRoles requires authentication — add Auth middleware before LoadRoles")
	}
	if RoleLoaderFunc == nil {
		return ctx.Error(errNoRoleLoader)
	}
	roles, err := RoleLoaderFunc(ctx.auth.UserID)
	if err != nil {
		return ctx.Error(err)
	}
	ctx.SetRoles(roles)
	return next()
}

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
