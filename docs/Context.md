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
| `SetAuth(claims)` | — | Store auth info (called by middleware) |
| `Auth()` | `*AuthInfo` | Retrieve auth info, nil if unauthenticated |
| `JSON(status, data)` | `Response` | JSON response |
| `NoContent()` | `Response` | 204 response |
| `Error(err)` | `Response` | 500 response |
| `NotFound(msg)` | `Response` | 404 response |
| `Unauthorized(msg)` | `Response` | 401 response |
| `Forbidden(msg)` | `Response` | 403 response |
