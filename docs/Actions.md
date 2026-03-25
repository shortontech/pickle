# Actions

Actions are gated operations on models. You define what the action does and who can perform it. Pickle wires them together so the action is only callable through its gate.

## File structure

Actions live in `database/actions/{model}/`, one file per action:

```
database/actions/
  user/
    ban.go           # BanAction struct with Ban() method
    ban_gate.go      # CanBan() gate function
    suspend.go
    suspend_gate.go
  post/
    publish.go
    publish_gate.go
```

## Defining an action

An action is a struct named `{Name}Action` with a method matching the action name:

```go
// database/actions/user/ban.go
package user

type BanAction struct {
    Reason string
}

func (a BanAction) Ban(ctx *Context, user *User) error {
    user.Status = "banned"
    user.BanReason = a.Reason
    return models.QueryUser().Update(user)
}
```

The method name must match the action name. `BanAction` must have `Ban()`. `PublishAction` must have `Publish()`.

Actions can return a result:

```go
func (a TransferAction) Transfer(ctx *Context, account *Account) (*TransferResult, error) {
    // ...
    return &TransferResult{TransactionID: txID}, nil
}
```

## Gates

Every action requires a gate function. A gate checks whether the current user is allowed to perform the action. No gate, no compilation.

```go
// database/actions/user/ban_gate.go
package user

import "github.com/google/uuid"

func CanBan(ctx *Context, model interface{ OwnerID() string }) *uuid.UUID {
    if ctx.IsAdmin() {
        id := uuid.New()
        return &id
    }
    return nil // denied
}
```

Returns a non-nil `*uuid.UUID` to allow (the authorising role ID), or `nil` to deny.

## How the generator wires it

Pickle generates a method on the model that enforces the gate:

```go
// Generated in app/models/user_actions_gen.go
func (m *User) Ban(ctx *Context, action actions.BanAction) error {
    roleID := actions.CanBan(ctx, m)
    if roleID == nil {
        return ErrUnauthorized
    }
    return action.ban(ctx, m)  // note: lowercase
}
```

The generator renames the action's `Ban()` method to `ban()` (unexported) so it cannot be called directly. The only way to execute the action is through the gated model method.

Usage in a controller:

```go
func (c UserController) Ban(ctx *pickle.Context) pickle.Response {
    user, err := models.QueryUser().WhereID(uuid.MustParse(ctx.Param("id"))).First()
    if err != nil {
        return ctx.NotFound("user not found")
    }

    err = user.Ban(ctx, actions.BanAction{Reason: "spam"})
    if err != nil {
        return ctx.Error(err)
    }

    return ctx.JSON(200, user)
}
```

## RBAC-enriched gates

When a role policy grants an action via `Can()`, Pickle generates a gate automatically. If you have:

```go
p.CreateRole("admin").Manages().Can("ban_user")
p.CreateRole("moderator").Can("ban_user")
```

Pickle generates `ban_user_gate_gen.go`:

```go
func CanBanUser(ctx *Context, model interface{ OwnerID() string }) *uuid.UUID {
    // Manages roles -- full access
    if ctx.HasAnyRole("admin") {
        id := uuid.New()
        return &id
    }
    // Specific roles granted "ban_user"
    if ctx.HasAnyRole("moderator") {
        id := uuid.New()
        return &id
    }
    return nil
}
```

## Gate override pattern

Generated gates end in `_gate_gen.go`. To override, create a `_gate.go` file with the same `Can{Action}` function. The generator skips the `_gen.go` version when a user-written gate exists.

## ScopeBuilder

Scopes restrict queries without exposing terminal operations (`First()`, `All()`, `Create()`, etc.). A scope function receives a `ScopeBuilder` and applies filters.

Scope files live in `database/scopes/{model}/`:

```go
// database/scopes/user/active.go
package user

func Active(sb *UserScopeBuilder) {
    sb.where("status", "active")
    sb.whereOp("banned_at", "IS", nil)
}
```

Scopes with parameters:

```go
func CreatedAfter(sb *UserScopeBuilder, since time.Time) {
    sb.whereOp("created_at", ">", since)
}
```

Pickle wires scopes as methods on the query type:

```go
users, _ := models.QueryUser().Active().CreatedAfter(cutoff).All()
```

`ScopeBuilder` supports `where`, `whereOp`, `whereIn`, `whereNotIn`, `OrderBy`, `Limit`, and `Offset`. It intentionally lacks terminal methods -- scopes define constraints, not execution.

## Audit trail

Actions automatically emit audit events. Three hooks fire at different points:

| Hook | When |
|------|------|
| `AuditPerformed` | Gate passed, action executed successfully |
| `AuditDenied` | Gate returned nil (unauthorized) |
| `AuditFailed` | Gate passed, action returned an error |

By default, audit hooks log to stdout. When audit tables are present, the generator wires `AuditHook` to persist events to the database.

### Audit tables

Three append-only tables in `database/migrations/audit/`:

| Table | Columns | Purpose |
|-------|---------|---------|
| `model_types` | id, name | Registry of model names |
| `action_types` | id, model_type_id, name | Registry of actions per model |
| `user_actions` | id, user_id, action_type_id, resource_id, resource_version_id, role_id, ip_address, request_id, created_at | Append-only audit log |

`user_actions` records who did what, to which resource, under which role, from which IP, in which request. The table is append-only -- rows are never updated or deleted.

## CLI

```bash
pickle make:action {model} {name}   # Scaffold an action and its gate
pickle make:scope {model} {name}    # Scaffold a scope file
```
