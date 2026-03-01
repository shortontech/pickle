# Router

Defines your API routes in a single file. Registers handlers onto Go 1.22+ `net/http.ServeMux` with path parameters, middleware groups, and resource routes.

All routes are defined in `routes/web.go`. The Router is both a route collector and a runtime registrar — no code generation needed for routing.

## Defining routes

```go
// routes/web.go
package routes

import (
    pickle "myapp/app/http"
    "myapp/app/http/controllers"
    "myapp/app/http/middleware"
)

var API = pickle.Routes(func(r *pickle.Router) {
    r.Group("/api", func(r *pickle.Router) {
        r.Post("/login", controllers.AuthController{}.Login)

        r.Group("/users", func(r *pickle.Router) {
            r.Get("/", controllers.UserController{}.Index)
            r.Get("/:id", controllers.UserController{}.Show)
            r.Post("/", controllers.UserController{}.Store)
        }, middleware.Auth)
    })
})
```

## HTTP methods

```go
r.Get(path, handler, ...middleware)
r.Post(path, handler, ...middleware)
r.Put(path, handler, ...middleware)
r.Patch(path, handler, ...middleware)
r.Delete(path, handler, ...middleware)
```

Each method takes a path, a handler `func(*Context) Response`, and optional per-route middleware.

## Path parameters

Use `:name` syntax. Parameters are read via `ctx.Param("name")`:

```go
r.Get("/users/:id", controllers.UserController{}.Show)
// ctx.Param("id") → "abc-123"
```

Internally, `:id` is converted to Go 1.22+ `{id}` patterns.

## Groups

Groups share a path prefix and middleware. The body function comes second; middleware follows as variadic arguments:

```go
r.Group("/admin", func(r *pickle.Router) {
    r.Get("/dashboard", controllers.AdminController{}.Dashboard)
    r.Get("/users", controllers.AdminController{}.Users)
}, middleware.Auth, middleware.RequireRole("admin"))
```

Groups nest. Middleware cascades from outer to inner groups.

## Resource routes

`r.Resource()` registers all five CRUD routes for a controller that implements `ResourceController`:

```go
r.Resource("/posts", controllers.PostController{})
```

Registers:
| Method | Path | Handler |
|--------|------|---------|
| GET | /posts | Index |
| GET | /posts/:id | Show |
| POST | /posts | Store |
| PUT | /posts/:id | Update |
| DELETE | /posts/:id | Destroy |

The controller must implement the `ResourceController` interface:

```go
type ResourceController interface {
    Index(*Context) Response
    Show(*Context) Response
    Store(*Context) Response
    Update(*Context) Response
    Destroy(*Context) Response
}
```

## Registering routes

In `cmd/server/main.go`, routes are wired up via the generated `commands` package. If you need manual control:

```go
mux := http.NewServeMux()
routes.API.RegisterRoutes(mux)
http.ListenAndServe(":8080", mux)
```

Or use the convenience method:

```go
routes.API.ListenAndServe(":8080")
```

## Method reference

| Method | Description |
|--------|-------------|
| `Routes(fn)` | Create a new Router via a configuration function |
| `Get/Post/Put/Patch/Delete(path, handler, ...mw)` | Register a route |
| `Group(prefix, fn, ...mw)` | Create a sub-router with shared prefix and middleware |
| `Resource(prefix, controller, ...mw)` | Register CRUD routes for a ResourceController |
| `AllRoutes()` | Return flattened list of all routes with resolved prefixes/middleware |
| `RegisterRoutes(mux)` | Wire all routes onto an `*http.ServeMux` |
| `ListenAndServe(addr)` | Convenience: create mux, register routes, start server |
