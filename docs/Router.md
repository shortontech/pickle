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

## Named routes

Assign stable names by chaining `Name` from any route registration:

```go
r.Get("/dashboard", controllers.DashboardController{}.Index, middleware.Auth).
    Name("dashboard")
r.Get("/users/:id", controllers.UserController{}.Show, middleware.Auth).
    Name("users.show")
```

Names must be unique across the fully flattened router. Pickle rejects duplicate
names when routes are registered.

Build URLs and redirects without repeating paths:

```go
url := ctx.RouteURL("users.show", pickle.RouteParams{"id": user.ID})
return ctx.RedirectToRoute("dashboard", nil)
```

Every declared path parameter is required and URL-escaped. Missing parameters,
extra parameters, and unknown route names are programming errors.

The current route is available to controllers and middleware:

```go
ctx.RouteName()            // "users.show"
ctx.RouteIs("users.show") // true
ctx.RouteIs("users.*")    // true
```

Literal paths and `ctx.Redirect("/fixed/path")` remain supported.

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
r.Resource("/posts", controllers.PostController{}).Names("posts")
```

Registers:
| Method | Path | Handler |
|--------|------|---------|
| GET | /posts | Index |
| GET | /posts/:id | Show |
| POST | /posts | Store |
| PUT | /posts/:id | Update |
| DELETE | /posts/:id | Destroy |

`Names("posts")` assigns `posts.index`, `posts.show`, `posts.store`,
`posts.update`, and `posts.destroy`.

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
| `Get/Post/Put/Patch/Delete(path, handler, ...mw)` | Register and return a nameable route |
| `Group(prefix, fn, ...mw)` | Create a sub-router with shared prefix and middleware |
| `Resource(prefix, controller, ...mw)` | Register CRUD routes and return a nameable route set |
| `URL(name, params)` | Build a URL for a named route |
| `AllRoutes()` | Return flattened list of all routes with resolved prefixes/middleware |
| `RegisterRoutes(mux)` | Wire all routes onto an `*http.ServeMux` |
| `ListenAndServe(addr)` | Convenience: create mux, register routes, start server |
