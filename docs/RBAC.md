# RBAC

Pickle's role-based access control is defined through policy files, not configuration. Roles, permissions, and column visibility are all code -- versioned, reviewable, and reversible.

## Roles

Roles are defined in policy files using the `Policy` DSL. Each policy has `Up()` and `Down()` methods, just like migrations.

### CreateRole

```go
func (p *InitialRoles_2026_03_23_100000) Up() {
    p.CreateRole("admin").
        Name("Administrator").
        Manages().
        Can("ban_user", "delete_post", "manage_roles")

    p.CreateRole("editor").
        Name("Editor").
        Can("create_post", "edit_post", "delete_post")

    p.CreateRole("viewer").
        Name("Viewer").
        Default()
}
```

- `Name()` sets the display name.
- `Manages()` marks the role as an admin-level role. `ctx.IsAdmin()` returns true for users with any Manages role.
- `Default()` marks the role as the default for new users.
- `Can()` grants action permissions to the role.

### AlterRole

```go
func (p *AddEditorBan_2026_03_24_100000) Up() {
    p.AlterRole("editor").
        Can("ban_user")
}

func (p *AddEditorBan_2026_03_24_100000) Down() {
    p.AlterRole("editor").
        RevokeCan("ban_user")
}
```

`AlterRole` also supports `RemoveManages()` and `RemoveDefault()`.

### DropRole

```go
func (p *RemoveViewer_2026_03_25_100000) Up() {
    p.DropRole("viewer")
}

func (p *RemoveViewer_2026_03_25_100000) Down() {
    p.CreateRole("viewer").
        Name("Viewer").
        Default()
}
```

## Column visibility

Annotate columns in migrations with `RoleSees()` to control which roles can see them:

```go
m.CreateTable("users", func(t *Table) {
    t.UUID("id").PrimaryKey().Default("uuid_generate_v7()")
    t.String("email").NotNull().Unique().Public()
    t.String("ssn", 11).NotNull().RoleSees("compliance")
    t.String("phone", 20).NotNull().RoleSees("support").RoleSees("compliance")
    t.Decimal("balance", 18, 2).NotNull().OwnerSees()
    t.Timestamps()
})
```

Pickle generates convenience methods for each non-manages role. If you have a `compliance` role, you get `ComplianceSees()`:

```go
t.String("ssn", 11).NotNull().ComplianceSees()
// equivalent to: .RoleSees("compliance")
```

## Query scopes

Generated query types include role-aware select methods:

```go
// Select columns visible to a single role
users, _ := models.QueryUser().SelectFor("compliance").All()

// Select union of columns visible to any of the user's roles
users, _ := models.QueryUser().SelectForRoles(ctx.Roles()).All()

// SelectForRoles + OwnerSees columns (for the resource owner)
user, _ := models.QueryUser().SelectForOwner(ctx.Roles()).WhereID(id).First()
```

`SelectFor` switches on role slug. Manages roles see all columns. Unknown roles see public columns only. `SelectForRoles` takes the union across multiple roles. `SelectForOwner` adds `OwnerSees` columns on top.

## Context methods

Available after `LoadRoles` middleware runs:

```go
ctx.Role()                          // primary role slug (first role), "" if none
ctx.Roles()                         // all role slugs
ctx.HasRole("editor")               // true if user has this role
ctx.HasAnyRole("editor", "admin")   // true if user has any of these
ctx.IsAdmin()                       // true if user has any Manages role
```

`SetRoles` is called by the `LoadRoles` middleware -- not by controllers.

## Middleware

Three built-in middleware for RBAC. They must be ordered correctly.

**LoadRoles** -- queries the `role_user` table and populates `ctx.Roles()`:

```go
r.Group("/api", middleware.Auth, middleware.LoadRoles, func(r *pickle.Router) {
    // ctx.Roles() is populated for all routes in this group
})
```

**RequireRole** -- checks for specific roles, returns 403 if none match:

```go
r.Group("/editor", middleware.Auth, middleware.LoadRoles, middleware.RequireRole("editor", "admin"), func(r *pickle.Router) {
    r.Get("/drafts", controllers.PostController{}.Drafts)
})
```

**RequireAdmin** -- checks for any Manages role:

```go
r.Group("/admin", middleware.Auth, middleware.LoadRoles, middleware.RequireAdmin, func(r *pickle.Router) {
    r.Resource("/users", controllers.UserController{})
})
```

Ordering: `Auth` -> `LoadRoles` -> `RequireRole`/`RequireAdmin`. Squeeze's `role_without_load` rule flags routes that use `RequireRole` without `LoadRoles`.

## Baked-in tables

Pickle generates migrations in `database/migrations/rbac/`:

| Table | Purpose |
|-------|---------|
| `roles` | Role definitions (slug, display_name, is_manages, is_default) |
| `role_actions` | Permissions granted to each role (role_slug, action) |
| `role_user` | User-to-role assignments |
| `rbac_changelog` | Policy execution state tracking (same pattern as migrations table) |

All follow the override pattern -- create a non-`_gen.go` version to replace any of them.

## CLI

```bash
pickle make:policy          # Scaffold a new role policy file
pickle policies:status      # Show applied/pending status of all policies
pickle policies:rollback    # Rollback the last batch of policies
```
