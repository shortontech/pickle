# CLAUDE.md — Pickle 🥒

> A salty web framework for sour artisans.

## What Is Pickle?

Pickle is a **code generation framework** for Go that gives you Laravel's developer experience with Go's deployment story. You write controllers, migrations, request classes, and middleware in a Laravel-like syntax — Pickle watches your project and generates all the boring Go boilerplate around them. The output is idiomatic Go. The input is not. That's the point.

**One sentence:** Write Laravel code in Go, deploy a single static binary with no runtime.

**The problem:** Go makes you write 200 lines to do what Laravel does in 3. Every Go project is 60% boilerplate and 40% the thing you actually care about. ORMs are all terrible. There's no real MVC framework. The community thinks this is a feature.

**The solution:** Pickle generates the boilerplate from your intent. You write what matters — controllers, migrations, validation rules, middleware — and `pickle --watch` generates models, route bindings, query scopes, handler wiring, and everything else. The generated code is plain Go. You can read it, debug it, `grep` it. It's not magic. It's just code you didn't have to type.

## Architecture

```
You Write (source of truth):          Pickle Generates (don't edit):
├── controllers/                      ├── generated/
│   └── transfer_controller.go        │   ├── models/
├── migrations/                       │   │   └── transfer.go            ← struct + relationships from migration
│   └── 001_create_transfers.go       │   ├── queries/
├── requests/                         │   │   └── transfer_query.go      ← WhereStatus(), WhereEmail(), etc.
│   └── create_transfer_request.go    │   ├── routes/
├── middleware/                       │   │   └── routes_gen.go          ← handler registration from routes.go
│   └── auth.go                       │   └── bindings/
├── routes.go                         │       └── bindings_gen.go        ← deserialization + validation
└── pickle.yaml                       └──
```

**`pickle --watch`** scans for new or modified controllers, migrations, request classes, and middleware files. When something changes, it regenerates the corresponding output. You never edit generated files — they get overwritten on the next run.

## Core Concepts

### Migrations → Models

Migrations are the **single source of truth** for your database schema. You write them in Go. Pickle generates model structs, query scope methods, and relationship helpers from them. You never manually sync a struct to your schema.

Migration files use Laravel-style naming: `{timestamp}_{description}.go` (e.g., `2026_02_21_143052_create_transfers_table.go`). The timestamp prefix determines execution order. The struct name appends the date to avoid naming collisions:

You write:
```go
// migrations/2026_02_21_143052_create_transfers_table.go
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

#### Migration Runner

Pickle scans the `migrations/` directory, sorts files by timestamp prefix, and generates a registry:

```go
// generated/migrations/registry.go
var Migrations = []pickle.MigrationEntry{
    {ID: "2026_01_15_091200_create_users_table", Migration: &CreateUsersTable_2026_01_15_091200{}},
    {ID: "2026_02_21_143052_create_transfers_table", Migration: &CreateTransfersTable_2026_02_21_143052{}},
    {ID: "2026_02_21_160000_add_currency_to_transfers", Migration: &AddCurrencyToTransfers_2026_02_21_160000{}},
}
```

The runner walks this slice, checks each against the `pickle_migrations` table, and executes pending ones.

#### Transactional Migrations

Migrations run inside a database transaction by default. If any statement fails, the entire migration rolls back — no half-applied state. DDL that can't be transactioned (e.g., `CREATE TABLE` on MySQL) runs outside the transaction, while DML and transaction-safe operations run inside it. The `Down()` method handles cleanup for non-transactional DDL (e.g., `DropTableIfExists` is idempotent).

On Postgres, which supports transactional DDL, the entire migration runs in one transaction. The runner is driver-aware — same migration code, different execution strategy.

For migrations that explicitly can't use transactions (e.g., `CREATE INDEX CONCURRENTLY` on Postgres):

```go
func (m *AddSearchIndex_2026_03_01_120000) Transactional() bool { return false }
```

#### Migration State Machine

Each migration tracks its lifecycle in the `pickle_migrations` table:

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
// pickle_migrations table
m.CreateTable("pickle_migrations", func(t *Table) {
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

// Query scopes — type-safe column filtering (generated methods on generic Query[T])
func (q *Query[Transfer]) WhereID(id uuid.UUID) *Query[Transfer] { ... }
func (q *Query[Transfer]) WhereCustomerID(id uuid.UUID) *Query[Transfer] { ... }
func (q *Query[Transfer]) WhereStatus(status string) *Query[Transfer] { ... }
func (q *Query[Transfer]) WhereCurrency(currency string) *Query[Transfer] { ... }
func (q *Query[Transfer]) WhereProcessor(processor string) *Query[Transfer] { ... }
func (q *Query[Transfer]) WhereBraleTransferID(id string) *Query[Transfer] { ... }
func (q *Query[Transfer]) WhereProcessorOrderID(id string) *Query[Transfer] { ... }
func (q *Query[Transfer]) WhereCreatedAtAfter(t time.Time) *Query[Transfer] { ... }
func (q *Query[Transfer]) WhereCreatedAtBefore(t time.Time) *Query[Transfer] { ... }

// Eager loading (generated from foreign keys)
func (q *Query[Transfer]) WithCustomer() *Query[Transfer] { ... }

// CRUD — provided by the generic Query[T] base
func (q *Query[T]) First() (*T, error) { ... }
func (q *Query[T]) All() ([]T, error) { ... }
func (q *Query[T]) Count() (int64, error) { ... }
func (q *Query[T]) Create(t *T) error { ... }
func (q *Query[T]) Update(t *T) error { ... }
```

The query builder uses a generic `Query[T]` base that handles SQL construction, parameterization, and execution. This base type is generated into your project's `generated/pickle.go` file — no external runtime dependency. Pickle generates type-safe `Where*` and `With*` methods as thin wrappers on the generic builder. CRUD methods (`First`, `All`, `Create`, etc.) come from the generic base and work with any model type.

Usage:
```go
// Find transfers by customer and status
transfers, err := Query[Transfer]().
    WhereCustomerID(customerID).
    WhereStatus("pending").
    All()

// Find a specific Brale transfer
transfer, err := Query[Transfer]().
    WhereBraleTransferID("2xNL6PAF0cbcQHyjMQJ2RKRfbD9").
    First()

// Eager load relationships
user, err := Query[User]().
    WhereEmail(email).
    WithPosts().
    First()

// Nested eager loading
user, err := Query[User]().
    WithPosts(func(q *Query[Post]) { q.WithComments() }).
    First()

// No more db.Where("stauts = ?", status) typos. Ever.
```

### Request Classes → Validation + Deserialization

Request classes define what comes in, how it's validated, and what the controller receives. These ARE idiomatic Go — structs with struct tags. Pickle generates the deserialization and validation wiring.

You write:
```go
// requests/create_transfer_request.go
type CreateTransferRequest struct {
    ExpectationID string `json:"expectation_id" validate:"required,uuid"`
    Amount        string `json:"amount" validate:"required,decimal,min=0.01"`
    Currency      string `json:"currency" validate:"required,oneof=USD EUR GBP"`
    Direction     string `json:"direction" validate:"required,oneof=onramp offramp"`
    DestChain     string `json:"destination_chain" validate:"required_if=Direction onramp,oneof=solana ethereum base polygon"`
    DestToken     string `json:"destination_token" validate:"required_if=Direction onramp,oneof=SBC USDC USDP"`
    SourceRail    string `json:"source_rail" validate:"required_if=Direction onramp,oneof=wire ach_credit ach_debit"`
}
```

Pickle generates the handler wiring that deserializes JSON, runs validation, and passes the typed request to your controller method. Your controller never sees `*http.Request` — it sees `CreateTransferRequest` already validated.

### Controllers

Controllers are plain Go structs with methods. They contain only business logic — no route declarations, no middleware config. Controllers don't know how they're wired up.

```go
// controllers/transfer_controller.go
type TransferController struct {
    Controller // embed base
}

func (c *TransferController) Store(req CreateTransferRequest, ctx *Context) Response {
    transfer := &Transfer{
        CustomerID: ctx.Auth().CustomerID,
        Amount:     decimal.RequireFromString(req.Amount),
        Currency:   req.Currency,
        Status:     "pending_approval",
        Processor:  selectProcessor(req),
    }

    if err := Query[Transfer]().Create(transfer); err != nil {
        return ctx.Error(err)
    }

    return ctx.JSON(201, transfer)
}

func (c *TransferController) Show(ctx *Context) Response {
    transfer, err := Query[Transfer]().
        WhereID(ctx.Param("id")).
        WhereCustomerID(ctx.Auth().CustomerID).
        First()

    if err != nil {
        return ctx.NotFound("transfer not found")
    }

    return ctx.JSON(200, transfer)
}

func (c *TransferController) Index(ctx *Context) Response {
    transfers, err := Query[Transfer]().
        WhereCustomerID(ctx.Auth().CustomerID).
        All()

    if err != nil {
        return ctx.Error(err)
    }

    return ctx.JSON(200, transfers)
}
```

### Routes → Route Wiring

Routes are defined in a central `routes.go` file — one file that shows your entire app's surface area. This is idiomatic Go, not comments or magic. Pickle reads the AST and generates the `net/http` handler registration, middleware chaining, and request deserialization.

You write:
```go
// routes.go
var API = pickle.Routes(func(r *pickle.Router) {
    r.Group("/api", RateLimit, func(r *pickle.Router) {
        r.Post("/auth/login", AuthController{}.Login)

        r.Group("/transfers", Auth, RequireKYB, func(r *pickle.Router) {
            r.Get("/", TransferController{}.Index)
            r.Get("/:id", TransferController{}.Show)
            r.Post("/", TransferController{}.Store, RequireRole("admin", "finance"))
        })

        r.Group("/admin", Auth, RequireRole("admin"), func(r *pickle.Router) {
            r.Resource("/users", UserController{})
        })
    })
})
```

Key features:
- **Groups** with shared prefixes and middleware that cascade to all child routes
- **Per-route middleware** passed as additional arguments after the handler
- **`r.Resource()`** generates standard CRUD routes (Index, Show, Store, Update, Destroy) for controllers that implement those methods
- **One file, whole app** — open `routes.go` and see every endpoint, its middleware, and its grouping

The `pickle.Router` type is generated into your project's `generated/pickle.go`. It's a thin descriptor — it doesn't actually route anything at runtime. It collects route definitions that Pickle's generator turns into real handler registration code.

### Middleware Stack

Middleware implements a simple interface. Each middleware explicitly calls `next()` to continue the chain, or returns early to short-circuit it. Not calling `next()` stops the request — no implicit pass-through.

```go
// middleware/auth.go
type AuthMiddleware struct{}

func (m *AuthMiddleware) Handle(ctx *Context, next func() Response) Response {
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

// Constructor used in routes.go
func Auth(ctx *Context, next func() Response) Response {
    return (&AuthMiddleware{}).Handle(ctx, next)
}
```

Parameterized middleware uses a constructor that returns the middleware function:

```go
// middleware/require_role.go
func RequireRole(roles ...string) pickle.MiddlewareFunc {
    return func(ctx *Context, next func() Response) Response {
        if !slices.Contains(roles, ctx.Auth().Role) {
            return ctx.Forbidden("insufficient permissions")
        }
        return next()
    }
}
```

Middleware can also do post-processing since it receives the `Response` from `next()`:

```go
func RequestTimer(ctx *Context, next func() Response) Response {
    start := time.Now()
    resp := next()
    resp.Header("X-Request-Duration", time.Since(start).String())
    return resp
}
```

The stack executes as nested calls. Group middleware runs first (outermost), then per-route middleware, then the controller:

```
Request → RateLimit → Auth → RequireKYB → RequireRole → RequestValidation → Controller → Response
```

Any layer can return early. The response bubbles back up through the stack, hitting post-processing in each layer on the way out.

## Project Structure

Pickle is a single Go module. There is no separate runtime package — the CLI generates all core types (`Query[T]`, `Context`, `Response`, `Router`, middleware types) directly into the project's `generated/` directory as a `pickle.go` file. Zero runtime dependencies on Pickle itself. The output is standalone Go.

Test apps live in `testdata/` (ignored by `go build` automatically). Tests run the generator against these fixtures and diff output against `expected/`.

```
pickle/
├── cmd/pickle/
│   └── main.go                  ← CLI entrypoint: pickle --watch, pickle generate, pickle migrate
├── pkg/
│   ├── generator/
│   │   ├── model_generator.go   ← Reads migrations, writes model files
│   │   ├── route_generator.go   ← Reads routes.go, writes handler registration
│   │   ├── query_generator.go   ← Generates WhereX(), WithX() query methods
│   │   ├── binding_generator.go ← Generates request deserialization + validation
│   │   └── core_generator.go   ← Generates pickle.go (Query[T], Context, Response, etc.)
│   ├── watcher/
│   │   └── watcher.go           ← File system watcher for --watch mode
│   ├── migration/
│   │   └── runner.go            ← Migration runner with state machine, transactions, and locking
│   └── schema/
│       └── table.go             ← Migration DSL (CreateTable, AddColumn, AddIndex, etc.)
├── templates/
│   ├── pickle.go.tmpl           ← Core types: Query[T], Context, Response, Router, Middleware
│   ├── model.go.tmpl            ← Go template for generated models
│   ├── queries.go.tmpl          ← Go template for query scopes + eager loading
│   ├── routes.go.tmpl           ← Go template for route registration
│   └── bindings.go.tmpl         ← Go template for request deserialization
├── testdata/
│   ├── basic-crud/              ← Simple test app: users, posts
│   │   ├── controllers/
│   │   ├── migrations/
│   │   ├── requests/
│   │   ├── middleware/
│   │   ├── routes.go
│   │   └── expected/            ← Expected generated output for test assertions
│   └── fintech/                 ← Complex test app: transfers, KYB, multi-role auth
│       ├── controllers/
│       ├── migrations/
│       ├── requests/
│       ├── middleware/
│       ├── routes.go
│       └── expected/
├── go.mod                       ← module github.com/pickle-framework/pickle
├── go.sum
├── CLAUDE.md
└── README.md
```

Install with `go install github.com/pickle-framework/pickle/cmd/pickle@latest`. One binary, no runtime dependency.

## CLI Commands

```bash
pickle --watch           # Watch for changes, regenerate on save
pickle generate          # One-shot: generate all files
pickle migrate           # Run pending migrations
pickle migrate:rollback  # Rollback last migration batch
pickle migrate:status    # Show migration status
pickle make:controller   # Scaffold a new controller
pickle make:migration    # Scaffold a new migration
pickle make:request      # Scaffold a new request class
pickle make:middleware    # Scaffold a new middleware
```

## What Pickle Is NOT

- **Not a runtime framework** — Pickle is a build tool. The generated code uses Go's stdlib and has no dependency on Pickle. There's no Pickle process running in production.
- **Not opinionated about your database driver** — Uses `database/sql` under the hood. Bring your own driver (`pgx`, `lib/pq`, whatever).
- **Not magic** — All generated code is visible, readable, debuggable Go. No reflection at runtime. No interface{} soup. Just structs and methods.
- **Not trying to replace Go idioms** — The generated OUTPUT is idiomatic Go. The input (your controllers, migrations) is Laravel-flavored. Pickle is the translator between "how you think" and "how Go wants it."

## Security

Pickle makes the secure path the default and the insecure path impossible or visibly wrong. Security comes from three layers:

### By Design — Structural Prevention

- **SQL injection** — impossible. `Query[T]()` generates parameterized queries. There's no API for string interpolation. Developers never write raw SQL.
- **Mass assignment** — request structs define exactly which fields are accepted. POSTing `{"role": "admin"}` does nothing if `CreateUserRequest` doesn't have a `Role` field. The model never sees unvalidated input.
- **Validation bypass** — controllers receive already-validated structs. The generated binding runs validation before your code executes. There's no code path around it.

### By Review — One-File Audit

- **IDOR / broken access control** — open `routes.go`, see every endpoint and its middleware stack. Missing `Auth` or `RequireRole` is immediately visible. No hunting through 15 handlers to check if one forgot an auth check.
- **CORS** — one middleware at the group level. Every route in the group inherits it. No per-handler configuration to forget.
- **Middleware gaps** — the central route file makes it obvious which endpoints are public and which are protected. A security review is a 30-second read.

### By Tooling — Standard Scanner Compatibility

Generated code is plain, idiomatic Go. `go vet`, `gosec`, `staticcheck`, Snyk, Semgrep — they all work on Pickle's output with zero configuration. No framework abstractions to unwrap, no `interface{}` soup, no runtime reflection. Security scanners see exactly what runs in production.

This is the advantage of code generation over runtime frameworks. A scanner can't reason about Goravel's magic method resolution or custom abstractions. It can reason about a struct, a function, and a parameterized query — because that's just Go.

## Design Decisions

### Why a central route file?

Go doesn't have decorators or annotations. The options were:
1. **Comment directives** (`// Route: POST /api/transfers`) — greppable but fragile, hard to express middleware config
2. **`Routes()` method on each controller** — keeps routes near handlers but scatters your API surface across files
3. **Central route file** — one file, entire app surface area visible at a glance

Central route file wins. It's how Laravel does it (`web.php` / `api.php`), it's idiomatic Go (just function calls), and it's the only approach where you can open one file and see every endpoint, its middleware, and its grouping. Pickle reads the AST at generation time — `pickle.Router` is a descriptor, not a runtime router.

### Why migrations in Go instead of SQL?

1. You get type-safe column definitions — `t.String("email").Unique()` vs `VARCHAR(255) UNIQUE`
2. Pickle can read the migration AST to generate models — can't easily do that with raw SQL
3. Rollbacks are co-located with the up migration
4. Conditional logic if needed (`if postgres { ... } else if sqlite { ... }`)

### Generated files: where do they go?

Generated files live in `generated/` with subdirectories mirroring their purpose (`models/`, `queries/`, `routes/`, `bindings/`). Every generated file has a `// Code generated by Pickle. DO NOT EDIT.` header. The output directory is configurable in `pickle.yaml` via `output_dir`. Configure your linter to ignore this directory. The `go:generate` comment convention is respected.

### Query builder: not a full ORM

Pickle generates typed query methods, not a full Eloquent clone. The query builder compiles to prepared statements with parameterized queries. No string interpolation. No SQL injection. But also no lazy loading, no eager loading magic, no N+1 query detection (yet). Keep it simple, ship it, iterate.

## Dependencies (Minimal)

- `github.com/fsnotify/fsnotify` — File watching for `--watch`
- `github.com/go-chi/chi/v5` — Router (or stdlib `net/http` with Go 1.22+ routing)
- `github.com/go-playground/validator/v10` — Struct validation
- `github.com/shopspring/decimal` — Decimal types for financial math
- `github.com/google/uuid` — UUID support
- `github.com/jackc/pgx/v5` — PostgreSQL driver (default, swappable)
- `text/template` — Go stdlib, for code generation templates

## Development

```bash
# Build the CLI
go build -o pickle ./cmd/pickle/

# Run in watch mode (for developing Pickle itself against a test project)
go run ./cmd/pickle/ --watch --project=./testproject/

# Run tests
go test ./...

# Generate from a test project
go run ./cmd/pickle/ generate --project=./testproject/
```

## Linting Generated Code

Go's linter will complain about unused functions in generated files. Solutions:
1. Add `//nolint` directives to generated file headers
2. Configure `golangci-lint` to exclude the `generated/` directory
3. Use `_ = Transfer.Query().WhereMiddleName` if it makes you feel better (don't actually do this)

The unused functions cost nothing at runtime. They're bytes in a binary. Your binary is maybe 2MB bigger. Your development velocity is 10x faster. The math works out.

## LLM Context Efficiency

Pickle is designed to be legible to AI. A Pickle project compresses the information an LLM needs to be effective — what might take 50k tokens of context in a raw Go project becomes 2-3k tokens of actual signal.

### Why It Works

Convention over configuration means the LLM never has to search. Validation is in `requests/`. Business logic is in `controllers/`. Routes are in `routes.go`. Every single time, no exceptions. The LLM doesn't need to *read* the project — it needs to *ask questions* about it.

- **Controllers are pure business logic** — no boilerplate to read past. 20 lines of intent, not 200 lines of wiring.
- **Request structs are self-documenting API contracts** — struct tags tell the LLM exactly what's accepted and how it's validated.
- **`routes.go` is the entire API surface** — one file, every endpoint, every middleware stack.
- **`Query[T]().WhereStatus("pending").All()`** — reads like a sentence. No query builder internals to understand.
- **Generated files never need to be read** — they're an implementation detail. The LLM works at the same abstraction level the developer works at.

### MCP Server Integration

A Pickle MCP server gives LLMs queryable access to the project without consuming context on source files:

- **`pickle schema:show transfers`** — returns the exact table structure. No reading migration files.
- **`pickle routes:list`** — every endpoint, its middleware, its request class. One call.
- **`pickle make:migration`**, **`pickle make:controller`** — the LLM scaffolds via tools, not by writing boilerplate.

LLMs are deeply trained on both Laravel and Go. Pickle sits at the intersection — the LLM already understands the intent (Laravel conventions) and the output (idiomatic Go). The framework is the bridge between two things the LLM already knows.

### Microservices Sweet Spot

Pickle microservices are small enough to fit entirely in context. A typical service — 3-5 controllers, a handful of migrations, one `routes.go` — is maybe 2-3k tokens. The LLM can hold the *complete* service in its head at once, not a summary. Combined with clear service boundaries and the MCP server, the workflow becomes: describe what you want → LLM scaffolds the entire service → review, tweak, deploy a static binary.

## Philosophy

Go's community confused *simplicity* with *tedium*. They're not the same thing. Writing the same HTTP handler boilerplate for the 47th time isn't simple — it's wasteful. Manually syncing struct definitions to database schemas isn't explicit — it's error-prone.

Pickle takes the position that if boilerplate can be generated, it should be. The generated code is visible, readable, and idiomatic Go. You sacrifice nothing. You gain everything.

**Laravel DX. Go binary. No runtime. 🥒**