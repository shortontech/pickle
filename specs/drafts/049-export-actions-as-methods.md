# 049 - Export Actions as Plain Go Methods

**Status:** Draft

## Problem

Pickle actions currently rely on generated model wiring. During `pickle export`, generated action wrappers are omitted and reported for manual review, which means exported applications can lose important business operations unless the developer rewrites them by hand.

Actions are not a runtime framework concept after export. They are ordinary business operations attached to a model or service. Export should make that explicit in plain Go.

## Goal

Lower Pickle actions from `database/actions/{model}/` into ordinary Go methods or functions in the exported application.

The exported code should:

- Preserve the user-authored action body.
- Preserve action input structs and result structs.
- Remove any dependency on Pickle-generated action wrappers.
- Make authorization through gates explicit.
- Preserve straightforward audit calls where possible.
- Report any action that cannot be safely lowered.

## Non-Goals

- Do not introduce a third-party action framework.
- Do not preserve Pickle's generated wrapper architecture.
- Do not silently drop audit behavior.
- Do not attempt to infer complex authorization if the gate cannot be lowered.

## Input Shape

User source:

```text
database/actions/user/
  ban.go
  ban_gate.go
```

Typical action:

```go
type BanAction struct {
    Reason string
}

func (a BanAction) Execute(ctx *pickle.Context, user *models.User) error {
    user.Status = "banned"
    user.BanReason = a.Reason
    return models.QueryUser().Update(user)
}
```

Generated Pickle model method today:

```go
func (u *User) Ban(ctx *pickle.Context, action BanAction) error
```

## Export Shape

Preferred output:

```go
func (u *User) Ban(ctx *httpx.Context, action user.BanAction) error {
    auditID, err := user.AuthorizeBan(ctx, u)
    if err != nil {
        audit.Denied(ctx, "Ban", "User", u.ID, err.Error())
        return err
    }

    if err := action.Execute(ctx, u); err != nil {
        audit.Failed(ctx, "Ban", "User", u.ID, err)
        return err
    }

    audit.Performed(ctx, auditID, "Ban", "User", u.ID)
    return nil
}
```

The action body itself remains in an exported package, with imports rewritten from Pickle packages to exported packages:

```go
func (a BanAction) Execute(ctx *httpx.Context, user *models.User) error {
    user.Status = "banned"
    user.BanReason = a.Reason
    return models.DB.Save(user).Error
}
```

If placing methods on generated GORM model files would create import cycles, generate companion files in `app/models` or `app/actions/{model}`. Prefer the smallest shape that compiles and reads naturally.

## Translation Rules

### Action Packages

Copy user-authored action files from `database/actions/{model}/` into an exported application package:

```text
app/actions/{model}/
```

Rewrite imports and signatures:

| Pickle Source | Exported Source |
|---------------|-----------------|
| `*pickle.Context` | `*httpx.Context` |
| `pickle.Response` | `httpx.Response` |
| `models.QueryX()` | GORM-backed model operations |
| Pickle HTTP import | `internal/httpx` |

### Model/Service Method

For each action with a matching gate, generate a plain method that:

1. Calls the exported gate helper.
2. Returns an authorization error if denied.
3. Executes the action body.
4. Emits audit calls when audit data can be preserved.

Action result structs are preserved:

```go
func (a TransferAction) Execute(ctx *httpx.Context, account *models.Account) (*TransferResult, error)
```

The generated method must mirror the result shape:

```go
func (a *Account) Transfer(ctx *httpx.Context, action transfer.TransferAction) (*transfer.TransferResult, error)
```

### Query Lowering

Action bodies use the same query lowering as controllers:

- `models.QueryX().Create(v)` -> `models.DB.Create(v).Error`
- `models.QueryX().Update(v)` -> `models.DB.Save(v).Error`
- `models.QueryX().Delete(v)` -> `models.DB.Delete(v).Error`
- supported `WhereX`, `First`, `All`, `Count`, `SumX`, and `AvgX` chains lower to GORM

Unsupported query chains fail export with file and line information.

### Audit

If generated action audit calls are available in source or can be reconstructed from action metadata, lower them into a small standalone audit package:

```go
func Denied(ctx *httpx.Context, action, model string, resourceID any, reason string)
func Failed(ctx *httpx.Context, action, model string, resourceID any, err error)
func Performed(ctx *httpx.Context, auditID *uuid.UUID, action, model string, resourceID any)
```

If audit semantics cannot be preserved, export should still compile but add an `actions_audit` manual-review finding.

## Error Handling

Use an exported authorization error:

```go
var ErrUnauthorized = errors.New("unauthorized")
```

Generated wrappers should return `ErrUnauthorized` or wrap it with context. Do not return Pickle errors.

## Export Report

Remove the broad `generated_actions` omission when all actions lower successfully.

Add specific findings instead:

| Rule | Section | Meaning |
|------|---------|---------|
| `action_export_unsupported_signature` | Manual Review | Action signature cannot be lowered |
| `action_export_unsupported_query` | Manual Review | Action query chain cannot be lowered |
| `action_export_import_cycle` | Manual Review | Natural method placement would create an import cycle |
| `actions_audit` | Manual Review | Audit behavior needs review |

## Tests

- Action with gate exports as a plain method and compiles.
- Action body imports are rewritten to exported packages.
- Action body query chains lower to GORM.
- Action returning `(*Result, error)` preserves result type.
- Action without lowerable gate fails or reports clearly.
- Audit calls are generated for a straightforward action.
- Unsupported action signature produces file/line error or report entry.
- Exported app has no Pickle imports or generated action runtime references.

## Dependencies

- Spec 050 - Export Gates as Authorization Helpers.
- Existing query lowering in `pkg/exporter`.
- Existing action scanner/generator metadata.
