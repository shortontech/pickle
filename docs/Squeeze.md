# Squeeze

Static security analysis for Pickle projects. Squeeze understands your framework — routes, middleware, migrations, request classes — and catches vulnerabilities that generic Go linters can't see.

## Running squeeze

```bash
pickle squeeze                        # analyze the current project
pickle squeeze --project ./myapp/     # analyze a specific project
```

Squeeze reads `pickle.yaml` for middleware classification and rule toggles.

## Configuration

```yaml
# pickle.yaml
squeeze:
  middleware:
    auth: [Auth]
    admin: [RequireRole]
    rate_limit: [RateLimit]
    csrf: [CSRF]
  rules:
    ownership_scoping: true
    read_scoping: true
    public_projection: true
    unbounded_query: true
    rate_limit_auth: true
    enum_validation: true
    uuid_error_handling: true
    required_fields: true
    auth_without_middleware: true
    param_mismatch: true
    csrf_missing: true
    no_printf: true
    immutable_raw_update: true
    immutable_raw_insert_missing_version_id: true
    immutable_timestamps: true
    immutable_direct_delete: true
    integrity_hash_override: true
    integrity_column_in_request: true
    sensitive_field_encryption: true
    public_sensitive_conflict: true
    encrypted_column_range: true
    sealed_column_where: true
    encrypted_column_order_by: true
    encrypted_sealed_conflict: true
    encrypted_missing_key_config: true
```

All rules default to enabled. Set a rule to `false` to disable it.

Add RBAC and action/scope rules to the config as needed:

```yaml
    stale_role_annotation: true
    unknown_role_annotation: true
    role_without_load: true
    default_role_missing: true
    ungated_action: true
    direct_execute_call: true
    scope_builder_leak: true
    query_builder_in_scope: true
```

## Rules

### ownership_scoping

**Severity:** error

**What it catches:** PUT, PATCH, and DELETE routes behind auth that query a model without scoping by the authenticated user. This is an IDOR (Insecure Direct Object Reference) vulnerability — any logged-in user can modify or delete another user's data.

**How to fix:** Add `WhereOwnedBy()` to your query chain, passing the authenticated user's ID:

```go
// BEFORE — any user can update any post
post, err := models.QueryPost().WhereID(id).First()

// AFTER — only the post owner can update it
authID, err := uuid.Parse(ctx.Auth().UserID)
if err != nil {
    return ctx.Unauthorized("invalid auth")
}
post, err := models.QueryPost().
    WhereID(id).
    WhereOwnedBy(authID).
    First()
```

`WhereOwnedBy()` is generated for any model with a foreign key to the users table. It filters by the ownership column (typically `user_id`).

For the User model itself (where the resource IS the authenticated user), scope by `WhereID` with the auth user's ID:

```go
user, err := models.QueryUser().
    WhereID(authID).
    First()
```

If the endpoint intentionally allows access to any user's data (e.g., an admin panel), call `.AnyOwner()` to signal the opt-out:

```go
// Explicit: this endpoint serves all resources regardless of owner
posts, err := models.QueryPost().AnyOwner().WhereStatus("published").All()
```

### read_scoping

**Severity:** error

**What it catches:** GET routes behind auth that query models without scoping by the authenticated user. This is a read-side IDOR — a user can view another user's private data.

**How to fix:** Same as `ownership_scoping` — add `WhereOwnedBy()` to your query:

```go
// BEFORE — any user can read any user's posts
posts, err := models.QueryPost().All()

// AFTER — only see your own posts
authID, err := uuid.Parse(ctx.Auth().UserID)
if err != nil {
    return ctx.Unauthorized("invalid auth")
}
posts, err := models.QueryPost().
    WhereOwnedBy(authID).
    All()
```

If the endpoint intentionally serves data to any authenticated user (e.g., a user directory), call `.AnyOwner()` to opt out:

```go
// User directory — intentionally shows all users
users, err := models.QueryUser().AnyOwner().All()
```

### public_projection

**Severity:** error

**What it catches:** Unauthenticated routes that return model data without calling `.Public()`. This can leak sensitive fields like `password_hash`, `token`, or `secret` to anonymous users.

**How to fix:** Call `.Public()` on model instances or `PublicXxx()` on slices before returning:

```go
// BEFORE — leaks password_hash, internal fields
return ctx.JSON(200, user)

// AFTER — returns only public-safe fields
return ctx.JSON(200, user.Public())

// For slices:
return ctx.JSON(200, models.PublicUsers(users))
```

`.Public()` is generated for every model. It returns a struct with sensitive fields stripped. Which fields are considered sensitive is determined by your migration schema.

### unbounded_query

**Severity:** error

**What it catches:** Routes that call `.All()` without `.Limit()`. On unauthenticated routes, anyone on the internet can dump your entire table in one request — a denial-of-service vector. On authenticated routes, a single request can still return megabytes of data.

**How to fix:** Add `.Limit()` or `.Paginate()` to any query that calls `.All()`:

```go
// BEFORE — returns every row in the table
users, err := models.QueryUser().All()

// AFTER — bounded response
users, err := models.QueryUser().
    Limit(50).
    All()
```

For paginated endpoints, use `.Limit()` and `.Offset()` together:

```go
users, err := models.QueryUser().
    OrderBy("created_at", "DESC").
    Limit(20).
    Offset(page * 20).
    All()
```

### rate_limit_auth

**Severity:** error

**What it catches:** Authentication endpoints (login, register) without rate limiting middleware. Without rate limiting, attackers can brute-force credentials.

**How to fix:** Add your rate limiting middleware to auth routes:

```go
// routes/web.go
r.Post("/login", controllers.AuthController{}.Login, middleware.RateLimit)
r.Post("/register", controllers.AuthController{}.Register, middleware.RateLimit)
```

Then classify it in `pickle.yaml`:

```yaml
squeeze:
  middleware:
    rate_limit: [RateLimit]
```

### csrf_missing

**Severity:** error

**What it catches:** POST, PUT, PATCH, and DELETE routes without CSRF middleware. Only fires when your project uses sessions (i.e., any controller or helper calls `session.Create`). Without CSRF protection, an attacker's website can submit forms to your app using the victim's session cookie.

**How to fix:** Add `session.CSRF` to your route groups:

```go
r.Group("/app", session.CSRF, func(r *pickle.Router) {
    r.Post("/register", controllers.AuthController{}.Register)
    r.Post("/transfers", controllers.TransferController{}.Store, middleware.Auth)
})
```

If you have a custom CSRF middleware, classify it in `pickle.yaml`:

```yaml
squeeze:
  middleware:
    csrf: [MyCSRF]
```

The default middleware name is `CSRF`. Requires `SESSION_SECRET` in your `.env`.

### param_mismatch

**Severity:** error

**What it catches:** `ctx.Param()` or `ctx.ParamUUID()` calls where the parameter name doesn't match any parameter in the route definition. This is usually a typo — `ctx.Param("idd")` instead of `ctx.Param("id")` — and will panic at runtime.

**How to fix:** Match the param name to your route definition:

```go
// Route: r.Get("/users/:id", ...)

// BEFORE — typo, will panic
id := ctx.Param("idd")

// AFTER — matches route parameter
id := ctx.Param("id")
```

### auth_without_middleware

**Severity:** error

**What it catches:** Controllers on unauthenticated routes that call `ctx.Auth()`. Without auth middleware, `ctx.Auth()` panics — the auth info was never set.

**How to fix:** Either add auth middleware to the route, or remove the `ctx.Auth()` call:

```go
// Option 1 — add auth middleware to the route
r.Get("/profile", controllers.UserController{}.Show, middleware.Auth)

// Option 2 — remove ctx.Auth() from the controller if it doesn't need auth
```

This is always a bug. If a controller needs auth info, the route must have auth middleware.

### enum_validation

**Severity:** error

**What it catches:** Request struct fields named `status`, `role`, `type`, `state`, `kind`, or `category` without `oneof=` validation. Without it, users can submit arbitrary values like `"god_mode"` for a role field.

**How to fix:** Add `oneof=` to the validate tag:

```go
// BEFORE — accepts any string
Status string `json:"status" validate:"required"`

// AFTER — only accepts known values
Status string `json:"status" validate:"required,oneof=draft published archived"`
```

### uuid_error_handling

**Severity:** error (for `ctx.Param`), warning (for `ctx.Auth`)

**What it catches:** `uuid.MustParse()` calls with user-controlled input. `MustParse` panics on invalid input, which crashes your server.

**How to fix:** Use `uuid.Parse()` with error handling:

```go
// BEFORE — panics on malformed UUID
id := uuid.MustParse(ctx.Param("id"))

// AFTER — returns a 400 on bad input
id, err := uuid.Parse(ctx.Param("id"))
if err != nil {
    return ctx.JSON(400, map[string]string{"error": "invalid id"})
}
```

For `ctx.Auth().UserID`, this is a warning rather than an error because the auth middleware has already validated the token. Using `uuid.Parse` is defense in depth:

```go
authID, err := uuid.Parse(ctx.Auth().UserID)
if err != nil {
    return ctx.Unauthorized("invalid auth")
}
```

### required_fields

**Severity:** error

**What it catches:** `Create()` calls where the model struct literal is missing fields that are `NOT NULL` with no default value in the migration. The database will reject the insert.

**How to fix:** Set all required fields in the struct literal:

```go
// BEFORE — missing Currency, which is NOT NULL with no default
transfer := &models.Transfer{
    Amount: amount,
    Status: "pending",
}

// AFTER — all required fields present
transfer := &models.Transfer{
    Amount:   amount,
    Status:   "pending",
    Currency: req.Currency,
}
```

Check your migration to see which columns are `NOT NULL` without `.Default()` or `.Nullable()`.

### sensitive_field_encryption

**Severity:** warning

**What it catches:** Columns with sensitive names that are not marked `.Encrypted()` in the migration. Pickle has a built-in dictionary of sensitive field patterns — exact names like `email`, `password`, `api_key`, `ssn`, `cvv`, and suffix patterns like `*_token`, `*_secret`, `*_key`, `*_hash`, `*_credential`.

**How to fix:** Mark sensitive columns with `.Encrypted()` in your migration:

```go
// BEFORE — sensitive data stored unencrypted
t.String("api_key", 255).NotNull()
t.String("email", 255).NotNull()

// AFTER — declared as encrypted at rest
t.String("api_key", 255).NotNull().Encrypted()
t.String("email", 255).NotNull().Encrypted()
```

The full list of sensitive patterns:

**Exact names:** `password`, `email`, `ssn`, `access_token`, `api_key`, `session_key`, `refresh_token`, `secret`, `private_key`, `credit_card`, `card_number`, `cvv`, `pin`, `date_of_birth`, `phone`, `phone_number`

**Suffixes:** `_secret`, `_token`, `_key`, `_hash`, `_password`, `_ssn`, `_credential`

### public_sensitive_conflict

**Severity:** error

**What it catches:** Columns marked `.Public()` whose names match sensitive field patterns. Exposing a field like `email` to unauthenticated users is almost always a mistake.

**How to fix:** Either remove `.Public()` from the sensitive column, or add `.UnsafePublic()` to explicitly acknowledge the exposure:

```go
// BEFORE — Squeeze flags this as an error
t.String("email", 255).NotNull().Public()

// AFTER — explicit acknowledgment that this is intentional
t.String("email", 255).NotNull().Public().UnsafePublic().Encrypted()
```

`.UnsafePublic()` is the escape hatch — same pattern as `.AnyOwner()` for ownership scoping. It makes the intent visible in code review and grep-able in audits.

### immutable_raw_update

**Severity:** error

**What it catches:** Raw SQL containing `UPDATE <table>` where `<table>` is an immutable or append-only table. On these tables, the query builder's `Update()` inserts a new version row — it never issues a SQL `UPDATE`. A raw `UPDATE` bypasses immutability and destroys history.

**How to fix:** Use the generated query builder:

```go
// BEFORE — destroys the previous state
db.Exec("UPDATE transfers SET status = 'completed' WHERE id = $1", id)

// AFTER — inserts a new version, previous state preserved
transfer.Status = "completed"
models.QueryTransfer().Update(transfer)
```

### immutable_raw_insert_missing_version_id

**Severity:** error

**What it catches:** Raw SQL `INSERT INTO <table>` on an immutable table that names explicit columns but omits `version_id`. The row will fail the NOT NULL constraint.

**How to fix:** Use the generated query builder, which handles `version_id` and `row_hash` automatically.

### immutable_timestamps

**Severity:** error

**What it catches:** `t.Immutable()` and `t.Timestamps()` called on the same table. Immutable tables derive `CreatedAt()` and `UpdatedAt()` from the UUID v7 timestamps in `id` and `version_id` — separate timestamp columns are redundant and would drift.

**How to fix:** Remove `t.Timestamps()` from the migration:

```go
// BEFORE — Squeeze flags this
m.CreateTable("transfers", func(t *Table) {
    t.Immutable()
    t.Timestamps() // ← remove this
})

// AFTER
m.CreateTable("transfers", func(t *Table) {
    t.Immutable()
})
```

### immutable_direct_delete

**Severity:** error

**What it catches:** Raw `DELETE FROM <table>` where the table is immutable and has no `SoftDeletes()`.

**How to fix:** If you need soft deletes, add `t.SoftDeletes()` to the migration and use the generated `Delete()` method. If you need hard deletes for data erasure (e.g., GDPR), that's a deliberate raw SQL operation — document why.

### integrity_hash_override

**Severity:** error

**What it catches:** Raw SQL that sets `row_hash` or `prev_hash` on an immutable or append-only table. These are computed by the query builder from the row's canonical serialization and must not be set manually.

**How to fix:** Use the generated `Create()` or `Update()` methods. They compute the hash chain automatically.

### integrity_column_in_request

**Severity:** error

**What it catches:** A request struct with a field tagged `json:"row_hash"` or `json:"prev_hash"`. Integrity columns are internal — they must not be accepted from external input.

**How to fix:** Remove the field from the request struct. If you need to expose integrity data to clients (e.g., for proof verification), return it in a response — never accept it in a request.

### encrypted_column_range

**Severity:** error

**What it catches:** Range or comparison scopes (`WhereXxxGT`, `WhereXxxLT`, `WhereXxxBetween`) on columns marked `.Encrypted()`. Encrypted values are ciphertext — ordering and range comparisons are meaningless.

**How to fix:** Remove range queries on encrypted columns. If you need to filter by range, the column should not be encrypted, or you should use a separate plaintext index column.

### sealed_column_where

**Severity:** error

**What it catches:** Any `WHERE` clause on a column marked `.Sealed()`. Sealed columns are write-only — the plaintext is never retrievable, so filtering by value is impossible.

**How to fix:** Remove the WHERE clause. Sealed columns can only be written and verified (e.g., password hashing), not queried.

### encrypted_column_order_by

**Severity:** error

**What it catches:** `ORDER BY` on a column marked `.Encrypted()`. Ciphertext sort order is effectively random and does not reflect the plaintext ordering.

**How to fix:** Remove the ORDER BY on the encrypted column, or sort by a different column.

### encrypted_sealed_conflict

**Severity:** error

**What it catches:** A column marked with both `.Encrypted()` and `.Sealed()`. These are mutually exclusive — `.Encrypted()` means the value is retrievable (decrypt on read), while `.Sealed()` means it is write-only (verify only, never decrypt).

**How to fix:** Pick one. Use `.Encrypted()` if you need to read the plaintext back. Use `.Sealed()` if the column should never be retrieved (e.g., password hashes).

### encrypted_missing_key_config

**Severity:** error

**What it catches:** Migrations that declare `.Encrypted()` or `.Sealed()` columns, but no `ENCRYPTION_KEY` is configured. Without a key, encryption cannot function.

**How to fix:** Set `ENCRYPTION_KEY` in your `.env` or environment variables. See [Config](Config.md) for details.

### stale_role_annotation

**Severity:** warning

**What it catches:** A `RoleSees()` annotation in a migration referencing a role slug that no longer exists in the `roles` table migration. The role was probably renamed or removed.

**How to fix:** Update the `RoleSees()` call to use the current role slug, or remove it if the role no longer exists.

### unknown_role_annotation

**Severity:** error

**What it catches:** A `RoleSees()` annotation referencing a role slug that has never been defined in any roles migration. This is a typo or a reference to a role that hasn't been created yet.

**How to fix:** Check the slug against your roles migration. Create the role first, or fix the typo.

### role_without_load

**Severity:** error

**What it catches:** A route that uses `RequireRole` middleware without `LoadRoles` in the middleware chain. Without `LoadRoles`, the context has no role data and `RequireRole` will always deny access.

**How to fix:** Add `LoadRoles` to the middleware chain before `RequireRole`:

```go
// BEFORE — RequireRole has no role data to check
r.Group("/admin", middleware.Auth, middleware.RequireRole("admin"), func(r *pickle.Router) { ... })

// AFTER — LoadRoles populates roles before the check
r.Group("/admin", middleware.Auth, middleware.LoadRoles, middleware.RequireRole("admin"), func(r *pickle.Router) { ... })
```

### default_role_missing

**Severity:** error

**What it catches:** A roles migration that defines roles but none of them is marked as the default role. New users need a default role assignment.

**How to fix:** Mark one role as the default in your roles seed migration.

### ungated_action

**Severity:** error

**What it catches:** A controller action that performs a state-changing operation (Create, Update, Delete) without any authorization gate — no ownership scoping, no role check, no explicit opt-out. Any authenticated user can execute the action.

**How to fix:** Add ownership scoping (`WhereOwnedBy`), role middleware (`RequireRole`), or call `.AnyOwner()` to explicitly acknowledge the lack of gating.

### direct_execute_call

**Severity:** error

**What it catches:** Direct calls to `db.Exec()` or `db.Query()` with raw SQL in controller code. Raw SQL bypasses the query builder's safety guarantees (parameterization, ownership scoping, immutability).

**How to fix:** Use the generated query builder methods. If raw SQL is genuinely needed, move it to a model method or repository function where it can be audited.

### scope_builder_leak

**Severity:** error

**What it catches:** A scope builder (`XxxScopeBuilder`) that is returned from a function or assigned to a variable outside of its intended scope function. Scope builders are restricted types meant to stay within scope definitions.

**How to fix:** Use scope builders only within scope functions. Return the final query result, not the builder.

### query_builder_in_scope

**Severity:** error

**What it catches:** A scope function that uses the full `QueryBuilder` or `XxxQuery` type instead of the restricted `XxxScopeBuilder`. Scopes must use the scope builder to prevent unrestricted query access.

**How to fix:** Accept and return `*XxxScopeBuilder` in scope functions, not `*XxxQuery`.

### no_printf

**Severity:** warning

**What it catches:** `fmt.Printf`, `fmt.Println`, `fmt.Sprintf`, and similar calls in controllers. These indicate debug logging that should use structured logging instead.

**How to fix:** Replace `fmt.Print*` calls with your structured logger, or remove debug output before shipping.
