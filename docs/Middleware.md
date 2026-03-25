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

## Built-in: CSRF protection

The session auth driver ships `session.CSRF` middleware for cross-site request forgery protection. It uses the HMAC double-submit cookie pattern — a token bound to the session ID is set as a JS-readable cookie and must be echoed back in the `X-CSRF-TOKEN` header on state-changing requests.

```go
r.Group("/app", auth.DefaultAuthMiddleware, session.CSRF, func(r *pickle.Router) {
    r.Post("/transfers", controllers.TransferController{}.Store)
    r.Delete("/transfers/:id", controllers.TransferController{}.Destroy)
})
```

Behavior:
- **GET/HEAD/OPTIONS** — sets the `csrf_token` cookie if missing, passes through
- **POST/PUT/PATCH/DELETE** — validates `X-CSRF-TOKEN` header against the session, returns 403 if missing or invalid
- **Bearer token requests** — bypasses CSRF entirely (API clients using `Authorization: Bearer` don't need it)

Requires `SESSION_SECRET` in your `.env`. The squeeze `csrf_missing` rule flags any state-changing route missing CSRF when your project uses sessions.

## Role middleware

Pickle provides built-in middleware for role-based access control. The role middleware chain runs after authentication.

**LoadRoles** — queries the database for the authenticated user's roles and populates the context:

```go
func LoadRoles(ctx *pickle.Context, next func() pickle.Response) pickle.Response
```

Must run after `Auth`. It calls `ctx.SetRoles()` with the user's assigned roles from the `user_roles` / `roles` tables.

**RequireRole** — parameterized middleware that checks for specific roles:

```go
func RequireRole(roles ...string) pickle.MiddlewareFunc {
    return func(ctx *pickle.Context, next func() pickle.Response) pickle.Response {
        if !ctx.HasAnyRole(roles...) {
            return ctx.Forbidden("insufficient permissions")
        }
        return next()
    }
}
```

**RequireAdmin** — shorthand for `RequireRole("admin")`:

```go
func RequireAdmin(ctx *pickle.Context, next func() pickle.Response) pickle.Response
```

### Middleware ordering

Role middleware must follow this order: `Auth` -> `LoadRoles` -> `RequireRole`. Auth sets the user identity, LoadRoles fetches their roles, and RequireRole gates access.

```go
r.Group("/admin", middleware.Auth, middleware.LoadRoles, middleware.RequireRole("admin"), func(r *pickle.Router) {
    r.Get("/dashboard", controllers.AdminController{}.Dashboard)
    r.Resource("/users", controllers.UserController{})
})

r.Group("/editor", middleware.Auth, middleware.LoadRoles, middleware.RequireRole("editor", "admin"), func(r *pickle.Router) {
    r.Get("/drafts", controllers.PostController{}.Drafts)
})
```

Squeeze's `role_without_load` rule flags routes that use `RequireRole` without `LoadRoles` in the middleware chain.

## Middleware location

Middleware files live in `app/http/middleware/`. They're plain Go — no code generation needed.
