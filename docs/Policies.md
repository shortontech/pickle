# Policies

Policies are versioned definitions for roles and GraphQL exposure. They use the same timestamp-prefixed, `Up()`/`Down()` pattern as migrations, but operate on RBAC and API surface state instead of database schema.

## Two policy types

**Role policies** define roles, permissions, and lifecycle changes. They embed `Policy` from the schema package.

**GraphQL policies** define which models and operations are exposed over the GraphQL API. They embed `GraphQLPolicy`.

Both types run transactionally by default and track their state in changelog tables.

## Role policy DSL

Role policy files live in `database/policies/`. Each file has a timestamp prefix and contains one struct.

```go
// database/policies/2026_03_23_100000_initial_roles.go
package policies

type InitialRoles_2026_03_23_100000 struct {
    Policy
}

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

func (p *InitialRoles_2026_03_23_100000) Down() {
    p.DropRole("viewer")
    p.DropRole("editor")
    p.DropRole("admin")
}
```

### Operations

| Method | Description |
|--------|-------------|
| `CreateRole(slug)` | Create a new role |
| `AlterRole(slug)` | Modify an existing role |
| `DropRole(slug)` | Remove a role |

### Builder methods

| Method | Applies to | Description |
|--------|-----------|-------------|
| `Name(string)` | create, alter | Set display name |
| `Manages()` | create, alter | Mark as admin-level role |
| `RemoveManages()` | alter | Remove admin-level flag |
| `Default()` | create, alter | Mark as default for new users |
| `RemoveDefault()` | alter | Remove default flag |
| `Can(actions...)` | create, alter | Grant action permissions |
| `RevokeCan(actions...)` | alter | Revoke action permissions |

## GraphQL policy DSL

GraphQL policy files live alongside role policies in `database/policies/`.

```go
// database/policies/2026_03_25_100000_expose_models.go
package policies

type ExposeModels_2026_03_25_100000 struct {
    GraphQLPolicy
}

func (p *ExposeModels_2026_03_25_100000) Up() {
    p.Expose("User", func(e *ExposeBuilder) {
        e.List()
        e.Show()
    })

    p.Expose("Post", func(e *ExposeBuilder) {
        e.All() // list, show, create, update, delete
    })

    p.ControllerAction("publishPost", controllers.PostController{}.Publish)
}

func (p *ExposeModels_2026_03_25_100000) Down() {
    p.RemoveAction("publishPost")
    p.Unexpose("Post")
    p.Unexpose("User")
}
```

### Operations

| Method | Description |
|--------|-------------|
| `Expose(model, fn)` | Expose a model with selected CRUD operations |
| `AlterExpose(model, fn)` | Add or remove operations on an already-exposed model |
| `Unexpose(model)` | Remove a model from the GraphQL schema entirely |
| `ControllerAction(name, handler)` | Register a custom controller action as a GraphQL mutation |
| `RemoveAction(name)` | Remove a previously registered controller action |

### ExposeBuilder methods

| Method | Description |
|--------|-------------|
| `List()` | Expose list/collection queries |
| `Show()` | Expose single-record queries |
| `Create()` | Expose create mutations |
| `Update()` | Expose update mutations |
| `Delete()` | Expose delete mutations |
| `All()` | Shorthand for all five operations |
| `RemoveList()` | Remove list (alter only) |
| `RemoveShow()` | Remove show (alter only) |
| `RemoveCreate()` | Remove create (alter only) |
| `RemoveUpdate()` | Remove update (alter only) |
| `RemoveDelete()` | Remove delete (alter only) |

### Altering exposures

```go
func (p *RestrictUsers_2026_03_26_100000) Up() {
    p.AlterExpose("User", func(e *ExposeBuilder) {
        e.RemoveDelete() // users can no longer be deleted via GraphQL
        e.Create()       // but they can now be created
    })
}
```

## Execution order

The policy runner executes in a fixed order during `pickle migrate`:

1. **Database migrations** -- schema changes first
2. **Role policies** -- roles and permissions
3. **GraphQL policies** -- API surface exposure

This ordering guarantees that roles exist before GraphQL policies reference them, and tables exist before roles reference their actions.

## State tracking

Role policies track state in the `rbac_changelog` table. GraphQL policies track state in the `graphql_changelog` table. Both use the same state machine as migrations:

```
Pending -> Running -> Applied
               |
            Failed
Applied -> Rolling Back -> Rolled Back
               |
            Failed
```

Policies run inside a transaction by default. Override `Transactional()` to return `false` for policies that need non-transactional execution.

## Rollback

Rollback reverses the last batch of policies. The runner calls `Down()` on each policy in the batch in reverse order.

```bash
pickle policies:rollback    # rolls back the last batch of role policies
```

`Down()` must be symmetric with `Up()`. If `Up()` creates a role, `Down()` must drop it. If `Up()` grants permissions, `Down()` must revoke them.

## CLI

```bash
pickle make:policy          # Scaffold a new policy file (prompts for type: role or graphql)
pickle policies:status      # Show applied/pending status of all policies
pickle policies:rollback    # Rollback the last batch of policies
```

## Baked-in tables

Role policies use four tables in `database/migrations/rbac/`:

| Table | Purpose |
|-------|---------|
| `roles` | Role definitions |
| `role_actions` | Permissions per role |
| `role_user` | User-to-role assignments |
| `rbac_changelog` | Policy execution state |

GraphQL policies use three tables in `database/migrations/graphql/`:

| Table | Purpose |
|-------|---------|
| `graphql_changelog` | Policy execution state |
| `graphql_exposures` | Which models/operations are exposed |
| `graphql_actions` | Custom controller actions registered as mutations |

All follow the override pattern.
