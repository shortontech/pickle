# Getting Started

Everything you need to go from zero to a running Pickle app.

## What Pickle does

You write controllers, migrations, request classes, and middleware in a Laravel-like syntax. Pickle watches your project and generates all the Go boilerplate — models, query builders, request bindings, config glue, and the app entrypoint. The output is plain Go. No runtime dependency, no reflection magic, just a static binary.

## Creating a project

```bash
pickle create myapp --module github.com/you/myapp
cd myapp
```

This scaffolds the full directory structure:

```
myapp/
├── cmd/server/main.go          ← Binary entrypoint
├── config/
│   ├── app.go                  ← App config (name, port, debug)
│   └── database.go             ← DB connections
├── routes/web.go               ← All API routes in one file
├── app/
│   ├── http/
│   │   ├── controllers/        ← Your business logic
│   │   ├── middleware/          ← Auth, rate limiting, etc.
│   │   └── requests/           ← Input validation structs
│   └── models/                 ← Generated from migrations
├── database/migrations/        ← Schema definitions
├── .env                        ← Environment variables
└── go.mod
```

Pickle also runs the generator and `go mod tidy`, so the project compiles immediately.

## The workflow

1. **Write a migration** — define your database table
2. **Write a request class** — define what input you accept
3. **Write a controller** — handle the request, talk to the database
4. **Add a route** — wire the controller to a URL
5. **Run `pickle --watch`** — Pickle generates models, query builders, bindings, and everything else

Repeat. The generated files update every time you save.

## Step by step: adding a resource

### 1. Migration

```go
// database/migrations/2026_02_27_120000_create_posts_table.go
package migrations

type CreatePostsTable_2026_02_27_120000 struct {
    Migration
}

func (m *CreatePostsTable_2026_02_27_120000) Up() {
    m.CreateTable("posts", func(t *Table) {
        t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
        t.UUID("user_id").NotNull().ForeignKey("users", "id")
        t.String("title").NotNull()
        t.Text("body").NotNull()
        t.Timestamps()
    })
}

func (m *CreatePostsTable_2026_02_27_120000) Down() {
    m.DropTableIfExists("posts")
}
```

Pickle generates `models/post.go` with the struct and `models/post_query.go` with typed query methods like `WhereUserID()`, `WhereTitle()`, etc.

### 2. Request class

```go
// app/http/requests/create_post.go
package requests

type CreatePostRequest struct {
    Title string `json:"title" validate:"required,min=1,max=200"`
    Body  string `json:"body" validate:"required"`
}
```

Pickle generates `requests.BindCreatePostRequest()` which deserializes JSON and validates in one call.

### 3. Controller

```go
// app/http/controllers/post_controller.go
package controllers

import (
    pickle "myapp/app/http"
    "myapp/app/http/requests"
    "myapp/app/models"
    "github.com/google/uuid"
)

type PostController struct {
    pickle.Controller
}

func (c PostController) Index(ctx *pickle.Context) pickle.Response {
    posts, err := models.QueryPost().All()
    if err != nil {
        return ctx.Error(err)
    }
    return ctx.JSON(200, posts)
}

func (c PostController) Store(ctx *pickle.Context) pickle.Response {
    req, bindErr := requests.BindCreatePostRequest(ctx.Request())
    if bindErr != nil {
        return ctx.JSON(bindErr.Status, bindErr)
    }

    post := &models.Post{
        UserID: uuid.MustParse(ctx.Auth().UserID),
        Title:  req.Title,
        Body:   req.Body,
    }
    if err := models.QueryPost().Create(post); err != nil {
        return ctx.Error(err)
    }
    return ctx.JSON(201, post)
}
```

### 4. Route

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
        r.Get("/posts", controllers.PostController{}.Index)

        r.Group("/posts", func(r *pickle.Router) {
            r.Post("/", controllers.PostController{}.Store)
        }, middleware.Auth)
    })
})
```

### 5. Generate and run

```bash
pickle --watch          # regenerates on every save
pickle migrate          # run pending migrations
go run ./cmd/server/    # start the server
```

## What you write vs. what Pickle generates

**You write** (source of truth — never overwritten):
- `database/migrations/` — table definitions
- `app/http/controllers/` — business logic
- `app/http/requests/` — input validation
- `app/http/middleware/` — auth, rate limiting
- `routes/web.go` — API surface
- `config/` — app and database config
- `cmd/server/main.go` — entrypoint

**Pickle generates** (overwritten on every run — don't edit):
- `app/models/*.go` — structs with `json`/`db` tags from migrations
- `app/models/*_query.go` — typed query builders (`WhereEmail()`, `WithPosts()`)
- `app/models/pickle_gen.go` — generic `QueryBuilder[T]`
- `app/http/pickle_gen.go` — `Context`, `Response`, `Router`, middleware types
- `app/http/requests/bindings_gen.go` — `Bind` functions for each request struct
- `app/commands/pickle_gen.go` — app lifecycle, CLI commands, migration runner
- `config/pickle_gen.go` — config accessors
- `database/migrations/*_gen.go` — schema types, migration registry, runner

## Running migrations

```bash
go run ./cmd/server/ migrate           # run pending
go run ./cmd/server/ migrate:rollback  # undo last batch
go run ./cmd/server/ migrate:fresh     # drop everything, re-run all
go run ./cmd/server/ migrate:status    # show what's applied
```

Or via the pickle CLI: `pickle migrate`, `pickle migrate:rollback`, etc.

## Environment variables

Configuration is driven by `.env` at your project root. The `Env(key, fallback)` helper loads it automatically. Environment variables always take precedence over `.env` values.

```
APP_PORT=8080
DB_HOST=127.0.0.1
DB_PORT=5432
DB_DATABASE=myapp
DB_USERNAME=postgres
DB_PASSWORD=secret
```

## The exit route

If you stop using Pickle, everything still works. The generated code is plain Go with zero dependency on Pickle. Delete the `pickle` binary, stop running the generator, and your project compiles exactly as before. Edit the generated files directly if you want — they're yours now.
