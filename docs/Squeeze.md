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
  rules:
    ownership_scoping: true
    read_scoping: true
    public_projection: true
    unbounded_query: true
    rate_limit_auth: true
    enum_validation: true
    uuid_error_handling: true
    required_fields: true
    no_printf: true
```

All rules default to enabled. Set a rule to `false` to disable it.

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

**Severity:** warning

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

This is a warning (not an error) because some read endpoints intentionally serve data to any authenticated user — e.g., a user directory. To suppress the warning for a specific query, call `.AnyOwner()`:

```go
// User directory — intentionally shows all users
users, err := models.QueryUser().AnyOwner().All()
```

You can also disable the rule globally with `read_scoping: false` in `pickle.yaml`.

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

### no_printf

**Severity:** warning

**What it catches:** `fmt.Printf`, `fmt.Println`, `fmt.Sprintf`, and similar calls in controllers. These indicate debug logging that should use structured logging instead.

**How to fix:** Replace `fmt.Print*` calls with your structured logger, or remove debug output before shipping.
