# 050 - Export Gates as Authorization Helpers

**Status:** Draft

## Problem

Pickle gates authorize actions and standalone model operations. They currently depend on Pickle context types and generated action wiring. During export, gates should not remain hidden behind a framework abstraction.

In standalone Go, gates are just authorization helpers.

## Goal

Lower gates from `database/actions/{model}/` into explicit Go functions in the exported application.

The exported code should:

- Preserve user-authored gate logic.
- Preserve generated RBAC-enriched gate behavior when it can be represented directly.
- Return explicit allow/deny information.
- Integrate with exported action methods.
- Avoid introducing a policy/action framework for gates.

## Non-Goals

- Do not introduce Casbin, OPA, or another policy engine for action gates.
- Do not preserve Pickle's generated gate wrapper names if clearer exported names are needed.
- Do not silently convert an unknown gate into allow-all behavior.

## Input Shape

Gate source:

```go
func CanBan(ctx *pickle.Context, user *models.User) *uuid.UUID {
    if ctx.Auth().Role == "admin" {
        id := uuid.New()
        return &id
    }
    return nil
}
```

Pickle convention:

- non-nil `*uuid.UUID` means authorized and identifies the authorizing role/policy/audit source
- nil means denied

## Export Shape

Preferred output:

```go
func AuthorizeBan(ctx *httpx.Context, user *models.User) (*uuid.UUID, error) {
    if ctx.Auth().Role == "admin" {
        id := uuid.New()
        return &id, nil
    }
    return nil, ErrUnauthorized
}
```

For simple boolean call sites, also allow generated convenience helpers:

```go
func CanBan(ctx *httpx.Context, user *models.User) bool {
    _, err := AuthorizeBan(ctx, user)
    return err == nil
}
```

The canonical helper for exported action wrappers should be `AuthorizeX`, because it preserves both denial reason and optional audit ID.

## Translation Rules

### User-Written Gates

Copy user-written gate files into the exported action package:

```text
app/actions/{model}/
```

Rewrite:

| Pickle Source | Exported Source |
|---------------|-----------------|
| `*pickle.Context` | `*httpx.Context` |
| `*models.X` | `*models.X` |
| `return nil` denial | `return nil, ErrUnauthorized` in `AuthorizeX` |
| `return roleID` allow | `return roleID, nil` |

If the original `CanX` function is copied as-is for compatibility, generate `AuthorizeX` beside it rather than changing its signature in place.

### Generated RBAC Gates

Generated gates derived from RBAC policies should lower to plain code that checks exported context auth/roles and model ownership.

Example:

```go
func AuthorizeUpdatePost(ctx *httpx.Context, post *models.Post) (*uuid.UUID, error) {
    if ctx.Auth().Role == "admin" {
        return roleID("admin"), nil
    }
    if ctx.Auth().UserID == post.UserID.String() {
        return roleID("owner"), nil
    }
    return nil, ErrUnauthorized
}
```

If the generated gate depends on policy state that export does not lower yet, emit a manual-review finding instead of generating an unsafe approximation.

### Standalone Gates

Standalone gates without a matching action are still exported. They become ordinary authorization helpers callable from controllers or middleware.

Call sites should be rewritten when possible:

```go
if actions.CanBan(ctx, user) { ... }
```

or:

```go
if _, err := actions.AuthorizeBan(ctx, user); err != nil { ... }
```

## Package Layout

Preferred layout:

```text
app/actions/user/
  ban.go
  ban_gate.go
  errors.go
```

`errors.go` contains shared authorization errors for that action package, unless a common `app/actions` package is needed to avoid duplication.

## Export Report

Remove broad `generated_actions` findings when all gates/actions lower successfully.

Add specific gate findings instead:

| Rule | Section | Meaning |
|------|---------|---------|
| `gate_export_unsupported_signature` | Manual Review | Gate signature cannot be lowered |
| `gate_export_policy_dependency` | Manual Review | Gate depends on policy state not lowered yet |
| `gate_export_dynamic_role` | Manual Review | Dynamic role lookup cannot be represented safely |
| `gate_export_callsite` | Manual Review | A call site could not be rewritten confidently |

## Safety Rules

- Never export an unknown gate as allow-all.
- A failed gate translation should either fail export or produce a report entry with file/line detail.
- Prefer deny-by-default stubs over permissive stubs if the user asks for partial output.
- Preserve audit IDs when available.

## Tests

- User-written `CanX(ctx, model) *uuid.UUID` exports to `AuthorizeX(ctx, model) (*uuid.UUID, error)`.
- Nil denial becomes `ErrUnauthorized`.
- Allowed role ID is preserved.
- Standalone gate without action exports and compiles.
- Exported action method calls `AuthorizeX`.
- Unsupported gate signature reports file/line detail.
- Generated RBAC gate lowers when all required role data is available.
- Export never produces allow-all code for unsupported gates.

## Dependencies

- Existing action/gate scanner metadata.
- Exported `httpx.Context` auth and role APIs.
- Optional future policy lowering for generated RBAC gates.
