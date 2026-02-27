# Controller

Plain Go structs with value receivers. Each handler method takes `*pickle.Context` and returns `pickle.Response`.

## Writing a controller

```go
// app/http/controllers/user_controller.go
package controllers

import (
    pickle "myapp/app/http"
    "myapp/app/http/requests"
    "myapp/app/models"
)

type UserController struct {
    pickle.Controller
}

func (c UserController) Index(ctx *pickle.Context) pickle.Response {
    users, err := models.QueryUser().All()
    if err != nil {
        return ctx.Error(err)
    }
    return ctx.JSON(200, users)
}

func (c UserController) Show(ctx *pickle.Context) pickle.Response {
    user, err := models.QueryUser().
        WhereID(uuid.MustParse(ctx.Param("id"))).
        First()
    if err != nil {
        return ctx.NotFound("user not found")
    }
    return ctx.JSON(200, user)
}

func (c UserController) Store(ctx *pickle.Context) pickle.Response {
    req, bindErr := requests.BindCreateUserRequest(ctx.Request())
    if bindErr != nil {
        return ctx.JSON(bindErr.Status, bindErr)
    }

    user := &models.User{
        Name:  req.Name,
        Email: req.Email,
    }
    if err := models.QueryUser().Create(user); err != nil {
        return ctx.Error(err)
    }
    return ctx.JSON(201, user)
}
```

## Conventions

- Embed `pickle.Controller` so the generator can identify controller types.
- Use **value receivers** (not pointer receivers): `func (c UserController)`, not `func (c *UserController)`.
- Controllers live in `app/http/controllers/`.
- One controller per resource, named `{Resource}Controller`.
- Standard methods: `Index`, `Show`, `Store`, `Update`, `Destroy`.

## ResourceController

To use `r.Resource()` in routes, implement all five CRUD methods:

```go
type ResourceController interface {
    Index(*Context) Response
    Show(*Context) Response
    Store(*Context) Response
    Update(*Context) Response
    Destroy(*Context) Response
}
```

## Request binding

Controllers that accept input call generated `Bind` functions from the `requests` package:

```go
req, bindErr := requests.BindCreateUserRequest(ctx.Request())
if bindErr != nil {
    return ctx.JSON(bindErr.Status, bindErr)
}
```

The `Bind` function deserializes JSON, validates all fields, and returns either the typed request struct or a `*BindingError` with status code and human-readable validation messages.

## Controller location

Controllers live in `app/http/controllers/`. They import `pickle "myapp/app/http"` for the Context and Response types.
