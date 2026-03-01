# Middleware

Functions that wrap controller handlers. Each middleware receives the context and a `next` function, and decides whether to continue the chain or return early.

## Signature

```go
func MyMiddleware(ctx *pickle.Context, next func() pickle.Response) pickle.Response
```

The type alias is `pickle.MiddlewareFunc`:

```go
type MiddlewareFunc func(ctx *Context, next func() Response) Response
```

## Writing middleware

**Basic auth check:**

```go
func Auth(ctx *pickle.Context, next func() pickle.Response) pickle.Response {
    token := ctx.BearerToken()
    if token == "" {
        return ctx.Unauthorized("missing token")
    }

    claims, err := validateJWT(token)
    if err != nil {
        return ctx.Unauthorized("invalid token")
    }

    ctx.SetAuth(claims)
    return next()
}
```

**Parameterized middleware** — return a `MiddlewareFunc`:

```go
func RequireRole(roles ...string) pickle.MiddlewareFunc {
    return func(ctx *pickle.Context, next func() pickle.Response) pickle.Response {
        if !slices.Contains(roles, ctx.Auth().Role) {
            return ctx.Forbidden("insufficient permissions")
        }
        return next()
    }
}
```

**Post-processing** — inspect or modify the response:

```go
func RequestTimer(ctx *pickle.Context, next func() pickle.Response) pickle.Response {
    start := time.Now()
    resp := next()
    return resp.Header("X-Request-Duration", time.Since(start).String())
}
```

## Applying middleware

**To a group** — all routes in the group inherit it:

```go
r.Group("/admin", func(r *pickle.Router) {
    r.Get("/dashboard", controllers.AdminController{}.Dashboard)
}, middleware.Auth, middleware.RequireRole("admin"))
```

**To a single route:**

```go
r.Post("/transfer", controllers.TransferController{}.Store, middleware.Auth)
```

## Execution order

Middleware executes as nested calls. Group middleware runs first (outermost), then per-route middleware, then the controller:

```
Request → RateLimit → Auth → RequireRole → Controller → Response
```

Any layer can short-circuit by returning without calling `next()`. The response bubbles back up through each layer.

## Middleware location

Middleware files live in `app/http/middleware/`. They're plain Go — no code generation needed.
