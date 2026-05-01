# 047 - `pickle export`

**Status:** Draft

## Problem

Pickle is a code generation framework: developers write migrations, controllers, request structs, routes, middleware, policies, actions, and scopes; Pickle generates the repetitive Go glue around them.

That is good during development, but a mature Pickle application should not be locked into Pickle forever. A team should be able to export the project into an idiomatic standalone Go application whose source code no longer imports Pickle-generated packages or requires the `pickle` CLI.

This matters for three reasons:

- **Trust** - teams can adopt Pickle knowing there is an exit path.
- **Auditability** - generated abstractions can be lowered into ordinary Go source for security review, compliance review, or long-term maintenance.
- **Agentic development** - AI agents can work against either the high-level Pickle source of truth or the exported conventional Go application, depending on the task.

## Goal

Add `pickle export`, a command that exports a Pickle project into an idiomatic Go application with no visible Pickle dependency.

The exported app should:

- Compile without importing `github.com/shortontech/pickle` or generated `app/http` Pickle runtime packages.
- Preserve application behavior as much as possible.
- Use standard Go packages and the dominant Go ORM target, **GORM**, for model persistence.
- Keep output readable and maintainable, not merely machine-generated.
- Make irreversible or ambiguous transformations explicit in an export report.

## Non-Goals

- Do not make the exported app byte-for-byte equivalent to the Pickle app.
- Do not support every Pickle feature in the first version.
- Do not export back into Pickle source.
- Do not mutate the source project.
- Do not hide unsupported constructs. If a construct cannot be translated safely, emit a clear TODO and report entry.

## Command

```bash
pickle export --out ./dist/myapp
pickle export --project ./myapp --out ./dist/myapp
pickle export --project ./myapp --out ./dist/myapp --orm gorm
pickle export --project ./myapp --out ./dist/myapp --report export-report.md
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--project <dir>` | `.` | Pickle project to export |
| `--out <dir>` | required | Destination directory |
| `--orm <name>` | `gorm` | ORM target. Only `gorm` is supported initially |
| `--force` | false | Allow export into a non-empty output directory |
| `--report <path>` | `<out>/EXPORT_REPORT.md` | Write translation report |
| `--dry-run` | false | Analyze and report without writing files |

## Export Pipeline

### 1. Generate First

`pickle export` runs the normal generator before export unless `--no-generate` is added later. Export should operate on a fully generated project so request bindings, route wiring, config, models, and schema-derived data are available.

### 2. Build Export IR

Create an internal export representation from the Pickle project:

```go
type ExportProject struct {
    ModulePath string
    Tables     []ExportModel
    Routes     []ExportRoute
    Requests   []ExportRequest
    Middleware []ExportMiddleware
    Controllers []ExportController
    Findings   []ExportFinding
}
```

The IR should be populated from existing generator/squeeze parsers where possible:

- `generator.DetectProject`
- migration/schema inspector output
- route parser
- controller parser
- request scanner
- policy/action scanners

### 3. Copy User-Owned Source

Copy user-written source into the destination:

- `cmd/server/`
- `routes/`
- `app/http/controllers/`
- `app/http/middleware/`
- `app/http/requests/`
- `config/`
- `database/migrations/`
- `database/policies/`
- `database/actions/`
- `database/scopes/`

Generated Pickle files should not be copied directly unless they are being lowered into ordinary application code.

### 4. Generate Standalone Runtime

Generate a small local runtime package for HTTP primitives, or rewrite controllers to standard `net/http` handlers.

First version should prefer a local runtime package because it minimizes behavior drift:

```text
internal/httpx/
  context.go
  response.go
  router.go
  middleware.go
```

The package must not mention Pickle in package names, comments, generated headers, or import paths.

Controller signatures can be rewritten from:

```go
func (c UserController) Show(ctx *pickle.Context) pickle.Response
```

to:

```go
func (c UserController) Show(ctx *httpx.Context) httpx.Response
```

This preserves the controller structure while removing Pickle.

### 5. Export Models to GORM

For each migration table, generate a GORM model:

```go
type User struct {
    ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
    Email     string    `gorm:"size:255;not null;uniqueIndex"`
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

Column mapping rules:

| Pickle DSL | GORM Output |
|------------|-------------|
| `UUID().PrimaryKey()` | `uuid.UUID` with `primaryKey` |
| `String(length)` | `string` with `size:<length>` |
| `Text()` | `string` with `type:text` |
| `Integer()` | `int` |
| `BigInteger()` | `int64` |
| `Decimal(p, s)` | `string` or decimal-compatible type; report precision choice |
| `Boolean()` | `bool` |
| `Timestamp()` | `time.Time` |
| nullable columns | pointer type or `sql.Null*`, choose pointer initially |
| foreign keys | field plus GORM association when relationship is known |
| `.Encrypted()` | custom serializer/hook or TODO with report entry in v1 |
| `.Sealed()` | custom write-only field handling or TODO with report entry in v1 |
| immutable/append-only | hooks or TODO with report entry in v1 |

The exported model package should include a database handle:

```go
var DB *gorm.DB

func SetDB(db *gorm.DB) {
    DB = db
}
```

## Query Translation

Pickle query chains should be rewritten with Go AST, not string replacement.

### Supported v1 Patterns

Translate common controller query chains:

```go
models.QueryUser().WhereID(id).First()
```

to:

```go
var user models.User
err := models.DB.Where("id = ?", id).First(&user).Error
```

```go
models.QueryPost().WhereUserID(userID).Limit(50).All()
```

to:

```go
var posts []models.Post
err := models.DB.Where("user_id = ?", userID).Limit(50).Find(&posts).Error
```

```go
models.QueryUser().Create(user)
```

to:

```go
err := models.DB.Create(user).Error
```

```go
models.QueryUser().Update(user)
```

to:

```go
err := models.DB.Save(user).Error
```

```go
models.QueryUser().Delete(user)
```

to:

```go
err := models.DB.Delete(user).Error
```

### Chain Method Mapping

| Pickle Query Method | GORM Translation |
|---------------------|------------------|
| `WhereX(value)` | `Where("x = ?", value)` |
| `WhereXNot(value)` | `Where("x <> ?", value)` |
| `WhereXIn(values)` | `Where("x IN ?", values)` |
| `WhereXNotIn(values)` | `Where("x NOT IN ?", values)` |
| `WhereXGT(value)` | `Where("x > ?", value)` |
| `WhereXGTE(value)` | `Where("x >= ?", value)` |
| `WhereXLT(value)` | `Where("x < ?", value)` |
| `WhereXLTE(value)` | `Where("x <= ?", value)` |
| `Limit(n)` | `Limit(n)` |
| `Offset(n)` | `Offset(n)` |
| `OrderByX(dir)` | `Order("x " + dir)` only if dir is validated; otherwise report |
| `First()` | `First(&model).Error` |
| `All()` | `Find(&slice).Error` |
| `Count()` | `Count(&count).Error` |
| `Create(model)` | `Create(model).Error` |
| `Update(model)` | `Save(model).Error` |
| `Delete(model)` | `Delete(model).Error` |

### Unsupported v1 Patterns

Emit a TODO and export finding for:

- custom raw `Where(column, value)` unless column is a string literal known to be safe
- encrypted/sealed column operations
- immutable history methods: `AllVersions`, `VerifyChain`, `Checkpoint`, `Proof`
- action gate wiring
- GraphQL generation
- MCP server behavior
- custom scopes that cannot be lowered to GORM safely

Example generated TODO:

```go
// TODO(export): manual translation required for immutable query method VerifyChain.
```

Report entry:

```text
app/http/controllers/ledger_controller.go:42
  unsupported_query_method: VerifyChain cannot be translated to GORM automatically
```

## AST Rewriting Requirements

Use Go AST for import and type rewrites:

- Replace imports of generated Pickle HTTP packages with `internal/httpx`.
- Replace selector expressions `pickle.Context` and `pickle.Response` with `httpx.Context` and `httpx.Response`.
- Rewrite generated binding imports only if the package path changes.
- Rewrite query chains by inspecting `ast.CallExpr` and `ast.SelectorExpr` chains.
- Preserve formatting with `go/format`.

Avoid regex-based source rewriting except for generated report text.

## Output Layout

```text
exported-app/
  cmd/server/main.go
  internal/httpx/
    context.go
    response.go
    router.go
    middleware.go
  app/
    http/controllers/
    http/middleware/
    http/requests/
    models/
  config/
  database/migrations/
  routes/
  go.mod
  EXPORT_REPORT.md
```

No output file should contain:

- `github.com/shortontech/pickle`
- `pickle.Controller`
- `pickle.Context`
- `pickle.Response`
- generated file headers mentioning Pickle
- `pickle_gen.go` filenames

## Export Report

`EXPORT_REPORT.md` should include:

- source project path
- export timestamp
- target ORM
- files written
- imports rewritten
- query chains translated
- unsupported features requiring manual work
- whether `go test ./...` or `go build ./...` succeeded in the exported app

## Testing

### Unit Tests

- query chain parser extracts model, filters, terminal operation, and arguments
- `WhereUserID` maps to `user_id`
- import rewrite replaces Pickle HTTP package with `internal/httpx`
- type rewrite replaces `pickle.Context` and `pickle.Response`
- unsupported query method emits finding

### Integration Tests

Use `testdata/basic-crud`:

```bash
pickle export --project ./testdata/basic-crud --out <tmpdir>
```

Assert:

- exported directory exists
- no exported `.go` file contains `github.com/shortontech/pickle`
- no exported `.go` file contains `pickle.`
- exported app has `gorm.io/gorm` in `go.mod`
- `go test ./...` or `go build ./...` succeeds when v1 supports enough runtime lowering

If build is not achievable in the first implementation, the integration test should assert the report clearly says build was skipped or failed with explicit unsupported findings.

## Initial Implementation Plan

1. Add `cmdExport()` and usage text.
2. Add `pkg/exporter` with `Export(projectDir, outDir, options)`.
3. Generate first, then inspect schema.
4. Copy user source to output.
5. Generate GORM models from schema.
6. Generate `internal/httpx` compatibility runtime.
7. AST-rewrite controller and middleware imports/types.
8. AST-rewrite common query chains.
9. Write `EXPORT_REPORT.md`.
10. Add unit tests and a basic-crud integration test.

## Open Questions

- Should exported code use GORM directly in controllers, or generate repository methods to keep controllers cleaner?
- Should `pickle export` include GraphQL in v1, or explicitly require REST-only export initially?
- Should encrypted and sealed fields map to GORM serializers/hooks in v1, or require manual follow-up?
- Should exported routes target `net/http`, `chi`, or a tiny local router? The first version should use a local `internal/httpx` router for least behavior drift.
