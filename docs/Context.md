# Context

The request context passed to every controller and middleware. Wraps the HTTP request/response and provides helpers for params, auth, and response building.

Every controller method receives `*pickle.Context` as its only argument and returns `pickle.Response`. Context is created automatically by the router — you never construct one yourself.

## Reading input

```go
// URL path parameter (e.g. /users/:id)
id := ctx.Param("id")

// Query string parameter (e.g. /users?page=2)
page := ctx.Query("page")

// Bearer token from Authorization header
token := ctx.BearerToken()

// Cookie value by name
csrfToken, err := ctx.Cookie("csrf_token")

// Raw *http.Request for anything else
req := ctx.Request()
```

## Authentication

Auth middleware calls `ctx.SetAuth(claims)` to store the authenticated user. Controllers read it back with `ctx.Auth()`.

```go
// In middleware:
ctx.SetAuth(&pickle.AuthInfo{
    UserID: "abc-123",
    Role:   "admin",
    Claims: jwtClaims, // any type — your JWT struct, session, etc.
})

// In controllers:
userID := ctx.Auth().UserID
role := ctx.Auth().Role
claims := ctx.Auth().Claims // type-assert to your claims type
```

`ctx.Auth()` returns `nil` if no auth middleware has run. The `AuthInfo` struct has three fields:
- `UserID string` — the user's ID
- `Role string` — the user's role
- `Claims any` — the raw claims object (JWT claims, session data, etc.)

If you pass an `*AuthInfo` directly to `SetAuth`, it's stored as-is. Any other type is wrapped in `AuthInfo{Claims: v}`.

## Roles

Role middleware populates role data on the context. Controllers read it back with role helper methods.

```go
// Primary role (string)
role := ctx.Role()

// All assigned roles ([]string slugs)
roles := ctx.Roles()

// Check for a specific role
if ctx.HasRole("editor") { ... }

// Check for any of several roles
if ctx.HasAnyRole("editor", "admin", "superadmin") { ... }

// Convenience check for the "admin" role
if ctx.IsAdmin() { ... }

// Set roles (called by LoadRoles middleware, not controllers)
ctx.SetRoles([]pickle.RoleInfo{
    {Slug: "admin", Name: "Administrator"},
    {Slug: "editor", Name: "Editor"},
})
```

`ctx.Role()` returns the first role's slug, or `""` if no roles are set. `ctx.Roles()` returns all role slugs. `ctx.HasRole()` and `ctx.HasAnyRole()` do exact slug matching. `ctx.IsAdmin()` is shorthand for `ctx.HasRole("admin")`.

## Building responses

Context provides convenience methods that return `pickle.Response`:

```go
// JSON response with status code
return ctx.JSON(200, user)
return ctx.JSON(201, transfer)

// Error responses
return ctx.Error(err)            // 500 + {"error": err.Error()}
return ctx.NotFound("not found") // 404 + {"error": "not found"}
return ctx.Unauthorized("bad token") // 401
return ctx.Forbidden("no access")    // 403

// No content
return ctx.NoContent() // 204, empty body
```

All JSON responses set `Content-Type: application/json` automatically.

## Method reference

| Method | Returns | Description |
|--------|---------|-------------|
| `Request()` | `*http.Request` | Underlying HTTP request |
| `ResponseWriter()` | `http.ResponseWriter` | Underlying response writer |
| `Param(name)` | `string` | URL path parameter by name |
| `Query(name)` | `string` | Query string parameter by name |
| `BearerToken()` | `string` | Token from `Authorization: Bearer` header |
| `Cookie(name)` | `string, error` | Cookie value by name |
| `SetAuth(claims)` | — | Store auth info (called by middleware) |
| `Auth()` | `*AuthInfo` | Retrieve auth info, nil if unauthenticated |
| `JSON(status, data)` | `Response` | JSON response |
| `NoContent()` | `Response` | 204 response |
| `Error(err)` | `Response` | 500 response |
| `NotFound(msg)` | `Response` | 404 response |
| `Unauthorized(msg)` | `Response` | 401 response |
| `Forbidden(msg)` | `Response` | 403 response |
| `SetRoles(roles)` | — | Store role info (called by middleware) |
| `Role()` | `string` | Primary role slug, empty if none |
| `Roles()` | `[]string` | All role slugs |
| `HasRole(slug)` | `bool` | Whether the user has the given role |
| `HasAnyRole(slugs...)` | `bool` | Whether the user has any of the given roles |
| `IsAdmin()` | `bool` | Shorthand for `HasRole("admin")` |
