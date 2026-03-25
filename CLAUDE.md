# CLAUDE.md — Pickle 🥒

> A salty web framework for sour artisans.

## What Is Pickle?

Pickle is a **code generation framework** for Go that gives you a batteries-included developer experience with Go's deployment story. You write controllers, migrations, request classes, and middleware in a concise, expressive syntax — Pickle watches your project and generates all the boring Go boilerplate around them. The output is idiomatic Go. The input is not. That's the point.

**One sentence:** Write expressive code in Go, deploy a single static binary with no runtime.

**The problem:** Go makes you write 200 lines to do what other frameworks do in 3. Every Go project is 60% boilerplate and 40% the thing you actually care about. ORMs are all terrible. There's no real MVC framework. The community thinks this is a feature.

**The solution:** Pickle generates the boilerplate from your intent. You write what matters — controllers, migrations, validation rules, middleware — and `pickle --watch` generates models, query scopes, request bindings, and everything else. The generated code is plain Go. You can read it, debug it, `grep` it. It's not magic. It's just code you didn't have to type.

## Architecture

```
You Write (source of truth):              Pickle Generates (don't edit):
├── app/                                  ├── app/
│   └── http/                             │   ├── http/
│       ├── controllers/                  │   │   ├── pickle_gen.go       ← Context, Response, Router, Middleware
│       │   ├── user_controller.go        │   │   └── requests/
│       │   ├── post_controller.go        │   │       └── bindings_gen.go ← request deserialization + validation
│       │   └── helpers.go                │   └── models/
│       ├── middleware/                   │       ├── pickle_gen.go       ← QueryBuilder[T], ScopeBuilder[T], DB
│       │   └── auth.go                   │       ├── user.go             ← struct from migration
│       └── requests/                     │       ├── user_query.go       ← WhereEmail(), SelectFor(), etc.
│           ├── create_user.go            │       ├── user_scope.go       ← UserScopeBuilder
│           ├── update_user.go            │       ├── post.go
│           ├── create_post.go            │       └── post_query.go
│           └── update_post.go            ├── config/
├── cmd/server/                           │   └── pickle_gen.go           ← Config loading
│   └── main.go                           └── database/
├── routes/                                   └── migrations/
│   └── web.go                                    └── types_gen.go       ← Migration, Table, Column types
├── database/
│   ├── migrations/
│   │   ├── 2026_02_21_100000_create_users_table.go
│   │   └── 2026_02_21_100001_create_posts_table.go
│   ├── policies/                         ← Role lifecycle (create/alter/drop)
│   │   └── 2026_03_23_000001_initial_roles.go
│   ├── policies/graphql/                 ← GraphQL exposure policies
│   │   └── 2026_03_25_000001_core_api.go
│   ├── actions/{model}/                  ← Gated actions (ban.go + ban_gate.go)
│   └── scopes/{model}/                   ← Restricted query scopes
├── config/
│   ├── app.go
│   └── database.go
├── .env
└── go.mod
```

The project follows a convention-based directory layout. Controllers, requests, and middleware each live in their own package under `app/http/`. The generated HTTP types (Context, Response, Router) live in `app/http/` as package `pickle` — controllers and middleware import them. Models live in `app/models/` as a separate package. Migrations live in `database/migrations/` with tickle-generated schema types. Routes live in `routes/`.

**`pickle --watch`** scans for changes and regenerates. You never edit generated files — they get overwritten on the next run.

**The exit route:** If you stop using Pickle, all generated code still compiles. The generated output has zero dependency on Pickle. The only Pickle dependency is in `database/migrations/types_gen.go` — and even that is a self-contained copy of the schema types, not an import.

## Core Concepts

### Migrations → Models

Migrations are the **single source of truth** for your database schema. You write them in Go using the schema DSL. Pickle generates model structs, query scope methods, and relationship helpers from them.

Migration files use timestamp-prefixed naming: `{timestamp}_{description}.go`. The timestamp prefix determines execution order:

```go
// database/migrations/2026_02_21_143052_create_transfers_table.go
type CreateTransfersTable_2026_02_21_143052 struct {
    Migration
}

func (m *CreateTransfersTable_2026_02_21_143052) Up() {
    m.CreateTable("transfers", func(t *Table) {
        t.UUID("id").PrimaryKey().Default("uuid_generate_v7()")
        t.UUID("customer_id").NotNull().ForeignKey("customers", "id")
        t.String("status").NotNull().Default("pending")
        t.Decimal("amount", 18, 2).NotNull()
        t.String("currency", 3).NotNull()
        t.String("processor", 20).NotNull()
        t.String("brale_transfer_id", 255).Nullable()
        t.String("processor_order_id", 255).Nullable()
        t.JSONB("metadata").Nullable()
        t.Timestamps()
    })

    m.AddIndex("transfers", "customer_id")
    m.AddIndex("transfers", "status")
}

func (m *CreateTransfersTable_2026_02_21_143052) Down() {
    m.DropTableIfExists("transfers")
}
```

The `Migration`, `Table`, and `Column` types in the `migrations/` package are generated by tickle from `pkg/schema/` — they're self-contained copies with no import dependency on Pickle.

#### Migration Runner

Pickle scans the `migrations/` directory, sorts files by timestamp prefix, and generates a registry. The runner walks this slice, checks each against the `migrations` table, and executes pending ones.

#### Transactional Migrations

Migrations run inside a database transaction by default. If any statement fails, the entire migration rolls back — no half-applied state. DDL that can't be transactioned (e.g., `CREATE TABLE` on MySQL) runs outside the transaction, while DML and transaction-safe operations run inside it. The `Down()` method handles cleanup for non-transactional DDL (e.g., `DropTableIfExists` is idempotent).

On Postgres, which supports transactional DDL, the entire migration runs in one transaction. The runner is driver-aware — same migration code, different execution strategy.

For migrations that explicitly can't use transactions (e.g., `CREATE INDEX CONCURRENTLY` on Postgres):

```go
func (m *AddSearchIndex_2026_03_01_120000) Transactional() bool { return false }
```

#### Migration State Machine

Each migration tracks its lifecycle in the `migrations` table:

```
Pending → Running → Applied
              ↓
           Failed
Applied → Rolling Back → Rolled Back
              ↓
           Failed
```

States:
- **Pending** — exists on disk, hasn't run
- **Running** — currently executing (lock acquired)
- **Applied** — successfully completed
- **Failed** — errored out (transaction rolled back what it could, error recorded)
- **Rolling Back** — rollback in progress
- **Rolled Back** — successfully reversed

The runner acquires a database lock before transitioning to Running, preventing concurrent execution. If the process crashes and a migration is stuck in Running, the next run surfaces the issue clearly instead of silently skipping or re-running.

```go
// migrations table
m.CreateTable("migrations", func(t *Table) {
    t.String("id").PrimaryKey()          // "2026_02_21_143052_create_transfers_table"
    t.Integer("batch").NotNull()
    t.String("state").NotNull()          // pending, running, applied, failed, rolling_back, rolled_back
    t.Text("error").Nullable()
    t.Timestamp("started_at").Nullable()
    t.Timestamp("completed_at").Nullable()
})
```

Pickle generates:
```go
// models/transfer.go (GENERATED — DO NOT EDIT)
type Transfer struct {
    ID               uuid.UUID        `json:"id" db:"id"`
    CustomerID       uuid.UUID        `json:"customer_id" db:"customer_id"`
    Status           string           `json:"status" db:"status"`
    Amount           decimal.Decimal  `json:"amount" db:"amount"`
    Currency         string           `json:"currency" db:"currency"`
    Processor        string           `json:"processor" db:"processor"`
    BraleTransferID  *string          `json:"brale_transfer_id,omitempty" db:"brale_transfer_id"`
    ProcessorOrderID *string          `json:"processor_order_id,omitempty" db:"processor_order_id"`
    Metadata         *json.RawMessage `json:"metadata,omitempty" db:"metadata"`
    CreatedAt        time.Time        `json:"created_at" db:"created_at"`
    UpdatedAt        time.Time        `json:"updated_at" db:"updated_at"`
}
```

### Query Builder

Each model gets a typed query wrapper with generated scope methods. The generic `QueryBuilder[T]` handles SQL construction; model-specific `TransferQuery` wraps it with type-safe `Where*` and `With*` methods.

```go
// Generated query type
type TransferQuery struct {
    *QueryBuilder[Transfer]
}

func QueryTransfer() *TransferQuery { ... }

// Generated scope methods
func (q *TransferQuery) WhereID(id uuid.UUID) *TransferQuery { ... }
func (q *TransferQuery) WhereCustomerID(id uuid.UUID) *TransferQuery { ... }
func (q *TransferQuery) WhereStatus(status string) *TransferQuery { ... }
func (q *TransferQuery) WithCustomer() *TransferQuery { ... }

// CRUD — inherited from QueryBuilder[T]
func (q *QueryBuilder[T]) First() (*T, error) { ... }
func (q *QueryBuilder[T]) All() ([]T, error) { ... }
func (q *QueryBuilder[T]) Count() (int64, error) { ... }
func (q *QueryBuilder[T]) Create(t *T) error { ... }
func (q *QueryBuilder[T]) Update(t *T) error { ... }
```

Usage:
```go
// Find transfers by customer and status
transfers, err := models.QueryTransfer().
    WhereCustomerID(customerID).
    WhereStatus("pending").
    All()

// Find a specific transfer
transfer, err := models.QueryTransfer().
    WhereBraleTransferID("2xNL6PAF0cbcQHyjMQJ2RKRfbD9").
    First()

// Eager load relationships
user, err := models.QueryUser().
    WhereEmail(email).
    WithPosts().
    First()

// No more db.Where("stauts = ?", status) typos. Ever.
```

### Request Classes → Validation + Deserialization

Request classes define what comes in, how it's validated, and what the controller receives. Pickle generates `BindXxxRequest()` functions that deserialize JSON and run validation.

You write:
```go
// app/http/requests/create_transfer.go
package requests

type CreateTransferRequest struct {
    ExpectationID string `json:"expectation_id" validate:"required,uuid"`
    Amount        string `json:"amount" validate:"required,decimal,min=0.01"`
    Currency      string `json:"currency" validate:"required,oneof=USD EUR GBP"`
    Direction     string `json:"direction" validate:"required,oneof=onramp offramp"`
}
```

Pickle generates `requests.BindCreateTransferRequest(r *http.Request) (CreateTransferRequest, *BindingError)` with JSON deserialization, struct validation, and human-readable error messages.

### Controllers

Controllers are plain Go structs with value receivers. All handlers take `*pickle.Context` and return `pickle.Response`. Controllers that need request binding call the generated `Bind` function from the `requests` package.

```go
// app/http/controllers/transfer_controller.go
package controllers

import (
    pickle "myapp/app/http"
    "myapp/app/http/requests"
    "myapp/app/models"
)

type TransferController struct {
    pickle.Controller
}

func (c TransferController) Store(ctx *pickle.Context) pickle.Response {
    req, bindErr := requests.BindCreateTransferRequest(ctx.Request())
    if bindErr != nil {
        return ctx.JSON(bindErr.Status, bindErr)
    }

    transfer := &models.Transfer{
        CustomerID: uuid.MustParse(ctx.Auth().UserID),
        Amount:     decimal.RequireFromString(req.Amount),
        Currency:   req.Currency,
        Status:     "pending_approval",
    }

    if err := models.QueryTransfer().Create(transfer); err != nil {
        return ctx.Error(err)
    }

    return ctx.JSON(201, transfer)
}

func (c TransferController) Show(ctx *pickle.Context) pickle.Response {
    transfer, err := models.QueryTransfer().
        WhereID(uuid.MustParse(ctx.Param("id"))).
        First()

    if err != nil {
        return ctx.NotFound("transfer not found")
    }

    return ctx.JSON(200, transfer)
}

func (c TransferController) Index(ctx *pickle.Context) pickle.Response {
    transfers, err := models.QueryTransfer().
        WhereCustomerID(uuid.MustParse(ctx.Auth().UserID)).
        All()

    if err != nil {
        return ctx.Error(err)
    }

    return ctx.JSON(200, transfers)
}
```

### Routes

Routes are defined in `routes/web.go`. The `Router` type is both a descriptor and a runtime router — it collects route definitions and registers them directly onto `net/http.ServeMux` via `RegisterRoutes()`. No code generation needed for routing.

```go
// routes/web.go
package routes

import (
    pickle "myapp/app/http"
    "myapp/app/http/controllers"
    "myapp/app/http/middleware"
)

var API = pickle.Routes(func(r *pickle.Router) {
    r.Group("/api", middleware.RateLimit, func(r *pickle.Router) {
        r.Post("/auth/login", controllers.AuthController{}.Login)

        r.Group("/transfers", middleware.Auth, middleware.RequireKYB, func(r *pickle.Router) {
            r.Get("/", controllers.TransferController{}.Index)
            r.Get("/:id", controllers.TransferController{}.Show)
            r.Post("/", controllers.TransferController{}.Store)
        })

        r.Group("/admin", middleware.Auth, middleware.RequireRole("admin"), func(r *pickle.Router) {
            r.Resource("/users", controllers.UserController{})
        })
    })
})
```

Wire it up in main:
```go
func main() {
    mux := http.NewServeMux()
    routes.API.RegisterRoutes(mux)
    http.ListenAndServe(":8080", mux)
}
```

Key features:
- **Groups** with shared prefixes and middleware that cascade to all child routes
- **Per-route middleware** passed as additional arguments after the handler
- **`r.Resource()`** registers standard CRUD routes (Index, Show, Store, Update, Destroy) for controllers that implement `ResourceController`
- **One file, whole app** — open `routes/web.go` and see every endpoint, its middleware, and its grouping
- **No code generation** — the Router handles registration at runtime using Go 1.22+ `ServeMux` patterns

### Middleware Stack

Middleware uses a simple function signature. Each middleware explicitly calls `next()` to continue the chain, or returns early to short-circuit it.

```go
// app/http/middleware/auth.go
package middleware

import pickle "myapp/app/http"

func Auth(ctx *pickle.Context, next func() pickle.Response) pickle.Response {
    token := ctx.BearerToken()
    if token == "" {
        return ctx.Unauthorized("missing token")
    }

    claims, err := ValidateJWT(token)
    if err != nil {
        return ctx.Unauthorized("invalid token")
    }

    ctx.SetAuth(claims)
    return next()
}
```

Parameterized middleware returns a `pickle.MiddlewareFunc`:

```go
func RequireRole(roles ...string) pickle.MiddlewareFunc {
    return func(ctx *pickle.Context, next func() pickle.Response) pickle.Response {
        if !ctx.HasAnyRole(roles...) {
            return ctx.Forbidden("insufficient role")
        }
        return next()
    }
}
```

Middleware can also do post-processing since it receives the `Response` from `next()`:

```go
func RequestTimer(ctx *pickle.Context, next func() pickle.Response) pickle.Response {
    start := time.Now()
    resp := next()
    resp.Header("X-Request-Duration", time.Since(start).String())
    return resp
}
```

The stack executes as nested calls. Group middleware runs first (outermost), then per-route middleware, then the controller:

```
Request → RateLimit → Auth → LoadRoles → RequireRole → Controller → Response
```

Any layer can return early. The response bubbles back up through the stack, hitting post-processing in each layer on the way out.

## Tickle — Generator of Generators

Tickle is Pickle's internal build tool. It takes Go source files from Pickle's own packages and turns them into embedded templates that the generator writes into user projects. Tickle runs at Pickle development time, not at user project generation time.

```bash
# Run from pickle repo root — no arguments
go run ./pkg/tickle/cmd/
```

This pre-generates embedded templates in `pkg/generator/`:
- `embed_http.go` — Context, Response, Router, Middleware, Controller, RBAC middleware (from `pkg/cooked/`)
- `embed_query.go` — QueryBuilder[T], ScopeBuilder[T], transactions, locks, encryption (from `pkg/cooked/`)
- `embed_schema.go` — Migration, Table, Column, Policy, GraphQLPolicy types (from `pkg/schema/`)
- `embed_policy.go` — PolicyRunner, GraphQLPolicyRunner, derive functions (from `pkg/migration/`)
- `embed_migration.go` — Migration runner, SQL generators (from `pkg/migration/`)
- `embed_config.go`, `embed_graphql.go`, `embed_scheduler.go` — other subsystems
- `embed_*_migrations.go` — per-file embeds for auth, RBAC, GraphQL, and audit driver migrations

Each template uses `__PACKAGE__` as a placeholder. At generation time, the generator does a simple string replace with the target package name and writes the file. No AST parsing, no runtime file I/O from Pickle's source tree.

**The exit route:** Because tickle copies Pickle's types into the user's project, the generated output has zero import dependency on Pickle. If you delete Pickle, your project still compiles.

## Project Structure

```
pickle/
├── cmd/pickle/
│   └── main.go                    ← CLI entrypoint: pickle --watch, pickle generate
├── pkg/
│   ├── generator/
│   │   ├── generate.go            ← Main orchestrator
│   │   ├── core_generator.go      ← Writes pre-tickled templates with package substitution
│   │   ├── model_generator.go     ← Generates model structs from schema
│   │   ├── scope_generator.go     ← Generates typed query scopes (WhereX, SelectFor, ScopeBuilder)
│   │   ├── binding_generator.go   ← Generates request deserialization + validation
│   │   ├── schema_inspector.go    ← Generates temp program to extract schema from migrations
│   │   ├── action_generator.go    ← Scans actions, validates gates, generates wiring
│   │   ├── scope_wiring_generator.go ← Reads user scopes, generates query wrappers
│   │   ├── rbac_generator.go      ← Writes RBAC baked-in migrations
│   │   ├── rbac_gate_generator.go ← Generates default gates from Can() declarations
│   │   ├── audit_generator.go     ← Writes audit trail baked-in migrations
│   │   ├── column_annotation_generator.go ← Generates XxxSees() from derived roles
│   │   ├── graphql_exposure_generator.go  ← Filters models by GraphQL policies
│   │   └── embed_*.go             ← PRE-TICKLED templates (gitignored, regenerated by tickle)
│   ├── cooked/                    ← Source-of-truth Go types (tickled into templates)
│   │   ├── context.go             ← Context with auth + role methods
│   │   ├── response.go
│   │   ├── router.go
│   │   ├── middleware.go
│   │   ├── rbac_middleware.go     ← RequireRole, RequireAdmin
│   │   ├── controller.go
│   │   ├── query.go               ← QueryBuilder[T] + ScopeBuilder[T]
│   │   ├── scopes.go              ← Scope templates (pickle:scope directives)
│   │   ├── audit.go               ← Audit trail hooks
│   │   ├── errors.go              ← Typed errors (ErrUnauthorized, etc.)
│   │   ├── rbac/migrations/       ← Baked-in: roles, role_actions, role_user, rbac_changelog
│   │   ├── graphql/migrations/    ← Baked-in: graphql_changelog, exposures, actions
│   │   └── audit/migrations/      ← Baked-in: model_types, action_types, user_actions
│   ├── tickle/                    ← Generator of generators
│   │   ├── tickle.go              ← Reads Go files, merges with package substitution
│   │   ├── scopes.go              ← Scope block parser and expander
│   │   └── cmd/main.go            ← CLI: run `tickle` with no args from repo root
│   ├── schema/                    ← DSL types (source of truth for tickle)
│   │   ├── migration.go           ← Migration base type
│   │   ├── policy.go              ← Policy base type (CreateRole, AlterRole, DropRole)
│   │   ├── graphql_policy.go      ← GraphQLPolicy (Expose, Unexpose, ControllerAction)
│   │   ├── table.go
│   │   ├── column.go              ← Column with VisibleTo map + RoleSees()
│   │   └── types.go
│   ├── migration/                 ← Runners (//go:build ignore — tickled into user projects)
│   │   ├── runner.go              ← Migration runner
│   │   ├── policy_runner.go       ← Role policy runner + DeriveRoles
│   │   └── graphql_policy_runner.go ← GraphQL policy runner + DeriveGraphQLState
│   ├── squeeze/                   ← Linting & validation rules
│   │   ├── rules.go               ← Core squeeze rules
│   │   ├── rbac_rules.go          ← RBAC-specific rules
│   │   └── action_rules.go        ← Action/scope rules
│   ├── scaffold/                  ← CLI scaffolding
│   │   ├── scaffold.go            ← Core scaffolds (controller, migration, etc.)
│   │   └── rbac_scaffold.go       ← Policy, action, scope, graphql-policy scaffolds
│   ├── mcp/                       ← MCP server for AI assistants
│   │   ├── server.go
│   │   └── rbac_tools.go          ← roles:list, roles:show, graphql:list
│   └── watcher/
│       └── watcher.go             ← File system watcher for --watch mode
├── testdata/
│   └── basic-crud/                ← Test app: users, posts (compiles standalone)
│       ├── app/
│       │   ├── http/
│       │   │   ├── controllers/   ← UserController, PostController
│       │   │   ├── middleware/    ← Auth
│       │   │   ├── requests/     ← CreateUser, UpdateUser, etc.
│       │   │   └── pickle_gen.go ← Generated: Context, Response, Router
│       │   └── models/           ← Generated: User, Post, queries
│       ├── routes/web.go
│       ├── database/migrations/
│       └── config/
├── go.mod
└── CLAUDE.md
```

## CLI Commands

```bash
pickle --watch              # Watch for changes, regenerate on save
pickle generate            # One-shot: generate all files
pickle migrate             # Run pending migrations, role policies, GraphQL policies
pickle migrate:rollback    # Rollback last migration batch
pickle migrate:status      # Show migration status
pickle policies:status     # Show role policy status
pickle policies:rollback   # Rollback last role policy batch
pickle graphql:status      # Show GraphQL policy status
pickle graphql:rollback    # Rollback last GraphQL policy batch
pickle make:controller     # Scaffold a new controller
pickle make:migration      # Scaffold a new migration
pickle make:request        # Scaffold a new request class
pickle make:middleware      # Scaffold a new middleware
pickle make:policy         # Scaffold a new role policy
pickle make:action         # Scaffold a new action + gate
pickle make:scope          # Scaffold a new query scope
pickle make:graphql-policy # Scaffold a new GraphQL exposure policy
```

## What Pickle Is NOT

- **Not a runtime framework** — Pickle is a build tool. The generated code uses Go's stdlib and has no dependency on Pickle. There's no Pickle process running in production.
- **Not opinionated about your database driver** — Uses `database/sql` under the hood. Bring your own driver (`pgx`, `lib/pq`, whatever).
- **Not magic** — All generated code is visible, readable, debuggable Go. No reflection at runtime. No interface{} soup. Just structs and methods.
- **Not trying to replace Go idioms** — The generated OUTPUT is idiomatic Go. The input (your controllers, migrations) is expressive and concise. Pickle is the translator between "how you think" and "how Go wants it."

## Security

Pickle makes the secure path the default and the insecure path impossible or visibly wrong.

### By Design — Structural Prevention

- **SQL injection** — impossible. `QueryBuilder[T]` generates parameterized queries. There's no API for string interpolation.
- **Mass assignment** — request structs define exactly which fields are accepted. POSTing `{"role": "admin"}` does nothing if `CreateUserRequest` doesn't have a `Role` field.
- **Validation bypass** — controllers call generated `Bind` functions that validate before returning the typed struct.
- **CSRF** — the session auth driver ships HMAC double-submit cookie CSRF middleware (`session.CSRF`). Tokens are generated from a random nonce HMAC-signed with the session ID using `SESSION_SECRET`. Safe methods (GET, HEAD, OPTIONS) pass through; state-changing methods require a valid `X-CSRF-TOKEN` header. Bearer-token API requests skip CSRF automatically. Cookies are set with `Secure`, `SameSite=Strict`, and `HttpOnly=false` (JS must read the token).
- **Rate limiting** — built into the router, not just middleware. Every request hits a per-IP token bucket *before* middleware or handlers execute — same level as panic recovery. Configured via `RATE_LIMIT_RPS` (default: 10) and `RATE_LIMIT_BURST` (default: 20) in `.env`. Returns 429 with `Retry-After` header. Disabled with `RATE_LIMIT=false`. For per-route overrides, `pickle.RateLimit(rps, burst)` returns a `MiddlewareFunc` that runs its own independent limiter. Proxy-aware: reads `X-Forwarded-For` and `X-Real-IP` before falling back to `RemoteAddr`. Stale buckets are cleaned up automatically.
- **Panic recovery** — the router catches panics in handlers and returns a 500 response instead of crashing the process. Recovered panics are forwarded to the `OnError` reporter for external error tracking (Sentry, Datadog, etc.).
- **Secrets** — `pickle new` scaffolds a `.gitignore` that excludes `.env` and `.env.local`. Secrets never end up in version control by default.

### By Design — RBAC and Gates

- **Role-based access control** — roles are defined in policy files, not config. `CreateRole("admin").Manages().Can("users.ban")` is code, not a database record. The policy runner applies them transactionally with full rollback support.
- **Gate enforcement** — every action requires a gate. Generate fails if a gate is missing. The generator renames the action method to unexported (`Ban()` → `ban()`) so it can only be called through the gated model method. Same-package bypass is caught by squeeze.
- **Action audit trail** — every successful action execution is recorded in an append-only `user_actions` table in the same transaction as the action. Gate denials and failures don't produce audit rows — nothing changed, nothing to audit.
- **Column visibility** — role-specific column annotations (`ComplianceSees()`, `SupportSees()`) generate `SelectFor(role)` query scopes that restrict SELECT clauses by role. Unknown roles see only `Public()` columns. `Manages()` roles see everything.

### By Design — Auth Drivers

Pickle ships opinionated auth drivers that eliminate common JWT and session pitfalls:

- **JWT driver** — pure Go HMAC implementation (HS256/HS384/HS512), no third-party JWT library. Algorithm is pinned server-side — tokens with a mismatched `alg` header are rejected, preventing alg=none and key confusion attacks. Expiry is enforced. Issuer is validated when configured. Tokens are tracked in a `jwt_tokens` allowlist table — a token must exist in the table *and* not be revoked to be valid. Revocation is instant: `RevokeToken(jti)` for single logout, `RevokeAllForUser(userID)` for password changes or account compromise.
- **Session driver** — server-side sessions with CSRF protection built in. The `CSRF` middleware is part of the session package and works automatically when the session driver is active.

### By Review — One-File Audit

- **IDOR / broken access control** — open `routes/web.go`, see every endpoint and its middleware stack. Missing `Auth` or `RequireRole` is immediately visible.
- **Middleware gaps** — the central route file makes it obvious which endpoints are public and which are protected. A security review is a 30-second read.

### By Tooling — Standard Scanner Compatibility

Generated code is plain, idiomatic Go. `go vet`, `gosec`, `staticcheck`, Snyk, Semgrep — they all work on Pickle's output with zero configuration. No framework abstractions to unwrap, no `interface{}` soup, no runtime reflection. Security scanners see exactly what runs in production.

This is the advantage of code generation over runtime frameworks. A scanner can't reason about Goravel's magic method resolution or custom abstractions. It can reason about a struct, a function, and a parameterized query — because that's just Go.

## Design Decisions

### Why a central route file?

Go doesn't have decorators or annotations. A central route file (`routes/web.go`) means one file, entire app surface area visible at a glance. It's idiomatic Go (just function calls). The `Router` type collects route definitions and registers them at runtime — no code generation needed for routing.

### Why migrations in Go instead of SQL?

1. Type-safe column definitions — `t.String("email").Unique()` vs `VARCHAR(255) UNIQUE`
2. Pickle runs migrations to extract schema and generate models — can't easily do that with raw SQL
3. Rollbacks are co-located with the up migration
4. Conditional logic if needed (`if postgres { ... } else if sqlite { ... }`)

### Generated files: where do they go?

Generated files live alongside user code in their respective packages: `app/http/pickle_gen.go`, `app/http/requests/bindings_gen.go`, `app/models/`, `database/migrations/types_gen.go`. Every generated file has a `// Code generated by Pickle. DO NOT EDIT.` header.

### Query builder: not a full ORM

Pickle generates typed query methods, not a full Eloquent clone. The query builder compiles to prepared statements with parameterized queries. No string interpolation. No SQL injection. Keep it simple, ship it, iterate.

## Dependencies (Minimal)

- `github.com/fsnotify/fsnotify` — File watching for `--watch`
- `github.com/go-playground/validator/v10` — Struct validation (in generated bindings)
- `github.com/shopspring/decimal` — Decimal types for financial math (in generated models)
- `github.com/google/uuid` — UUID support (in generated models)
- `database/sql` + `net/http` — Go stdlib, used by generated code

## Linting Generated Code

Go's linter will complain about unused functions in generated files. Solutions:
1. Add `//nolint` directives to generated file headers
2. Configure `golangci-lint` to exclude generated `_gen.go` files and `models/`
3. Use `_ = Transfer.Query().WhereMiddleName` if it makes you feel better (don't actually do this)

The unused functions cost nothing at runtime. They're bytes in a binary. Your binary is maybe 2MB bigger. Your development velocity is 10x faster. The math works out.

## LLM Context Efficiency

Pickle is designed to be legible to AI. A Pickle project compresses the information an LLM needs to be effective — what might take 50k tokens of context in a raw Go project becomes 2-3k tokens of actual signal.

### Why It Works

Convention over configuration means the LLM never has to search. Validation is in request structs. Business logic is in controllers. Routes are in `routes.go`. Every single time, no exceptions. The LLM doesn't need to *read* the project — it needs to *ask questions* about it.

- **Controllers are pure business logic** — no boilerplate to read past. 20 lines of intent, not 200 lines of wiring.
- **Request structs are self-documenting API contracts** — struct tags tell the LLM exactly what's accepted and how it's validated.
- **`routes/web.go` is the entire API surface** — one file, every endpoint, every middleware stack.
- **`QueryTransfer().WhereStatus("pending").All()`** — reads like a sentence. No query builder internals to understand.
- **Generated files never need to be read** — they're an implementation detail. The LLM works at the same abstraction level the developer works at.

### MCP Server Integration

A Pickle MCP server gives LLMs queryable access to the project without consuming context on source files:

- **`pickle schema:show transfers`** — returns the exact table structure with visibility annotations. No reading migration files.
- **`pickle routes:list`** — every endpoint, its middleware, its request class. One call.
- **`pickle roles:list`** — all current roles with permissions. Derived from policy files.
- **`pickle roles:show admin`** — single role with column visibility per table and action grants.
- **`pickle graphql:list`** — exposed models with their operations. Derived from GraphQL policies.
- **`pickle make:migration`**, **`pickle make:controller`**, **`pickle make:action`** — the LLM scaffolds via tools, not by writing boilerplate.

LLMs understand both MVC conventions and Go. Pickle sits at the intersection — the LLM already understands the intent (convention-based MVC) and the output (idiomatic Go). The framework is the bridge between two things the LLM already knows.

### Microservices Sweet Spot

Pickle microservices are small enough to fit entirely in context. A typical service — 3-5 controllers, a handful of migrations, one `routes.go` — is maybe 2-3k tokens. The LLM can hold the *complete* service in its head at once, not a summary. Combined with clear service boundaries and the MCP server, the workflow becomes: describe what you want → LLM scaffolds the entire service → review, tweak, deploy a static binary.

## Development

```bash
# Build the CLI
go build -o pickle ./cmd/pickle/

# Run tickle to regenerate embedded templates (after changing pkg/cooked/ or pkg/schema/)
go run ./pkg/tickle/cmd/

# Run tests
go test ./...

# Generate from a test project
go run ./cmd/pickle/ generate --project ./testdata/basic-crud/

# Run in watch mode
go run ./cmd/pickle/ --watch --project ./testdata/basic-crud/
```

## Generator Rules — Non-Negotiable Conventions

These rules are structural. They are not suggestions. Do not improvise alternatives, consolidate files for convenience, or skip steps because they seem redundant. Every rule exists to preserve the override pattern, naming conventions, and single-source-of-truth guarantees that Pickle promises its users.

### File Naming

- **Generated files** end in `_gen.go`. Always. No exceptions.
- **Migration files** must have a timestamp prefix: `{timestamp}_{description}.go`. The timestamp determines execution order globally. A migration file without a timestamp prefix is invalid.
- **Generated migration files** combine both rules: `{timestamp}_{description}_gen.go`.
- **Model files** are named after the resource: `user.go`, `transfer.go`. Generated models are `user.go` (not `user_gen.go`) because the user never writes models — they are always generated.
- **One struct per file.** Never merge multiple migration structs, multiple models, or multiple request bindings into a single file. If there are three migrations, there are three files.

### The Override Pattern

This is Pickle's core extensibility contract:

- `foo_gen.go` is generated by Pickle. It gets overwritten on every generation run.
- `foo.go` is written by the user. It is never touched by the generator.
- **If `foo.go` exists, the generator must not write `foo_gen.go`.** The user's version takes precedence. Always.
- The generator must **check for the non-generated version before writing**. This is not optional.

This applies everywhere: migrations, models, config, auth drivers. If a driver ships a `create_users_table_gen.go` migration and the user creates `create_users_table.go`, the user's version wins and the generator skips the `_gen.go` file entirely.

### Migration Conventions

- Migrations are the **single source of truth** for database schema. Everything else is derived.
- The migration runner collects **all** migrations across all subdirectories, sorts by timestamp globally, and executes in order. Directory structure is organizational, not executional.
- Each migration file contains **one migration struct**. The struct name encodes the timestamp: `CreateUsersTable_2026_02_21_100000`.
- The migration ID is derived from the struct name's timestamp suffix, not from the filename. The filename and struct name must agree.
- Auth drivers and other built-in modules emit their migrations as individual `_gen.go` files with proper timestamp prefixes — not as a single merged file.
- `Up()` and `Down()` must be symmetric. If `Up()` creates something, `Down()` must clean it up.

### Directory → Package Mapping

- `database/migrations/*.go` → `app/models/*.go`
- `database/migrations/auth/*.go` → `app/models/auth/*.go`
- The subdirectory name becomes the Go package name. One level deep. No deeper nesting.
- The migration runner flattens all subdirectories for execution order. The directory is for code organization and package boundaries only.

### Controller Conventions

- Every controller method has the same signature: `func (c XxxController) Method(ctx *pickle.Context) pickle.Response`. No variations.
- Value receivers, not pointer receivers.
- One controller per resource.
- Controllers never contain raw SQL. If the query is complex, it belongs in a view definition in a migration.

### What the Generator Must Never Do

- **Never merge multiple structs into one file** to "simplify" output.
- **Never skip the override check** before writing a `_gen.go` file.
- **Never invent naming conventions.** Use timestamp prefixes for migrations, `_gen.go` for generated files, `_query.go` for query scopes. These are fixed.
- **Never put raw SQL in generated controller code or query builders.** Complex queries belong in view migrations.
- **Never add configuration options** where a convention exists. Pickle is opinionated. The opinion is the feature.

### Driver Migrations

Any driver (auth, RBAC, audit, etc.) can ship its own migrations. Driver migrations live in a subdirectory of `database/migrations/` named after the driver — never in the user's root migration directory. For example, the session auth driver writes to `database/migrations/auth/`, RBAC writes to `database/migrations/rbac/`, audit writes to `database/migrations/audit/`. Driver migrations follow all the same rules: timestamp prefixes, `_gen.go` suffix, one struct per file, override pattern applies. The migration runner flattens all subdirectories for execution order. The directory is for ownership boundaries, not execution order.

## Philosophy

Go's community confused *simplicity* with *tedium*. They're not the same thing. Writing the same HTTP handler boilerplate for the 47th time isn't simple — it's wasteful. Manually syncing struct definitions to database schemas isn't explicit — it's error-prone.

Pickle takes the position that if boilerplate can be generated, it should be. The generated code is visible, readable, and idiomatic Go. You sacrifice nothing. You gain everything.

**Expressive DX. Go binary. No runtime. 🥒**
