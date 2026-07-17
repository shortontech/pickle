# Policies

Policies are versioned definitions for row authorization, roles, and GraphQL exposure. They use the same timestamp-prefixed, `Up()`/`Down()` pattern as migrations, but operate on authorization and API state instead of database schema.

> Define row authorization once as a Pickle row policy. Pickle enforces it in generated application queries and emits equivalent PostgreSQL RLS for the portable subset. Do not duplicate it as hand-written RLS. Raw application queries are Squeeze errors, not justification for a second policy system.

## Three policy types

**Row policies** decide which existing or proposed rows a subject may select, insert, update, or delete. They are enforced by every generated terminal query on every driver. Portable rules additionally generate PostgreSQL RLS from the identical normalized predicate.

**Role policies** define roles, permissions, and lifecycle changes. They embed `Policy` from the schema package.

**GraphQL policies** define which models and operations are exposed over the GraphQL API. They embed `GraphQLPolicy`.

All three types replay deterministically. In a Pickle project, row and role state share the role-policy transaction. Standalone exports keep a dedicated `row_policy_changelog`, replay pending row-policy versions in order, and roll back only the latest applied batch to its exact preceding generated state. GraphQL retains its later phase and changelog.

## Row policy DSL

Row policies live in `database/policies/` and embed `Policy`. Declare typed identities before protecting a table:

```go
func (p *MessageAccess_2026_07_16_120000) Up() {
    p.IdentityUUID("user_id")
    p.IdentityUUID("workspace_id")

    p.Protect("messages", func(rows *Rows) {
        rows.ExistingRowsAlreadyValid("ownership backfill 2026-07-16 completed")
        rows.Rule("workspace_member").ForAuthenticated().
            Select(Owner("workspace_id", Identity("workspace_id"))).
            Insert(Owner("workspace_id", Identity("workspace_id"))).
            Update(
                Existing(Owner("workspace_id", Identity("workspace_id"))),
                Proposed(Owner("workspace_id", Identity("workspace_id"))),
            ).
            Delete(Owner("workspace_id", Identity("workspace_id")))

        rows.Rule("admin_all").ForRole("admin").All(Allow())
    })
}

func (p *MessageAccess_2026_07_16_120000) Down() {
    p.Unprotect("messages")
}
```

Identity declarations are `IdentityUUID`, `IdentityString`, `IdentityStrings`, `IdentityInt64`, and `IdentityInt64s`. Scalar integer identities compare numerically with `Integer` and `BigInteger` policy columns. `IdentityInt64s` is a bounded JSON array of signed integers used only by `In(PolicyColumn(...), Identity(...))`; Pickle sorts and deduplicates it, binds every value, and treats an empty, missing, malformed, or oversized set as no match. Subjects are `ForPublic`, `ForAuthenticated`, and `ForRole`. `ForAuthenticated` also requires a declared `user_id` identity. Matching subjects use `AnyOfSubjects` by default; call `rows.CombineSubjects(AllOfSubjects)` when every matching subject predicate must pass.

For an organization plus allowed-company boundary:

```go
p.IdentityInt64("user_id")
p.IdentityInt64("organization_id")
p.IdentityInt64s("allowed_company_ids")

rows.Rule("organization_companies").ForAuthenticated().All(And(
    Equal(PolicyColumn("organization_id"), Identity("organization_id")),
    In(PolicyColumn("suborganization_id"), Identity("allowed_company_ids")),
))
```

The left side of `In` must be an `Integer` or `BigInteger` policy column and the right side must be an `IdentityInt64s` identity. Reversed operands and scalar identities fail generation. Use explicit `Existing(...)` and `Proposed(...)` predicates when a tenant column can change; policy source remains statically normalized.

Predicates are a typed tree, not Go or SQL strings:

| Predicate | Meaning |
|---|---|
| `Allow()` / `Deny()` | Constant decision |
| `Identity("name")` | Declared request/job identity |
| `PolicyColumn("column")` | Resolved table column |
| `Owner("column", Identity("name"))` | Equality ownership check |
| `Equal`, `NotEqual` | Typed comparison with SQL null semantics |
| `And`, `Or`, `Not` | Boolean composition |
| `Exists("relationship", predicate)` | Direct, unprotected FK relationship admission for existing rows |

Generation replays every policy, resolves tables, columns, roles, identity types, and operation positions, then feeds one normalized representation to both application and PostgreSQL emitters. Unknown or ambiguous references stop generation.

The first `Protect` transition must also make an explicit decision about rows that already exist. Call `ExistingRowsAlreadyValid(reason)` only after a reviewed backfill or when the table is known to be empty. Pickle records that acknowledgement but does not execute or infer a backfill. Add ownership columns and populate them in schema migrations before the policy transition; later `AlterProtection` transitions operate on an already protected table and do not repeat the acknowledgement.

`UPDATE` requires both `Existing(...)` and `Proposed(...)`. Immutable history tables cannot safely reduce to the globally current version after PostgreSQL has applied per-row RLS. Any row policy on an immutable table therefore requires the explicit `rows.AllowApplicationOnly("non_bijective_physical_plan")` acknowledgement. Generated application enforcement remains active for every operation, but Pickle emits no RLS that could resurrect an older or pre-delete version.

For immutable reads, Pickle first finds the globally newest version and then applies row admission. A denied newest version never reveals an older allowed version.

### Policy context

Protected queries fail before database access when no matching subject or required identity is present. Authentication returns a sealed source that application packages cannot implement; the generated model adapter converts it to `PolicyContext`. Verified custom string claims are available by their claim name, and `user_id` always comes from the verified authentication subject:

```go
policyContext, ok := models.PolicyContextFromHTTP(ctx)
if !ok {
    return ctx.Error(errors.New("policy context boundary is not installed"))
}
```

Ordinary controller code attaches that context with `WithPolicyContext`, or seals it on a transaction. PostgreSQL dual enforcement uses transaction-local settings:

```go
err := models.WithTransaction(func(tx *models.Tx) error {
    if err := tx.WithPostgresPolicyContext(policyContext); err != nil {
        return err
    }
    messages, err := tx.QueryMessage().All()
    _ = messages
    return err
})
```

`pickle.identity.*` is reserved. `Tx.SetLocal` cannot spoof it, a transaction context cannot be overwritten, and nested savepoints inherit but cannot broaden it.

HTTP and GraphQL entry points derive context automatically from the active driver's verified credential; absence produces only the public context, while an invalid presented credential fails. Controllers read the established value with `models.PolicyContextFromHTTP(ctx)`. Background jobs and CLI commands have no ambient request, so they call `models.AuthenticateJobPolicyContext` or `models.AuthenticateCLIPolicyContext` with a driver-native, request-shaped credential. These adapters deliberately do not accept identity maps or role lists. Tests alone receive `models.NewVerifiedPolicyContext` from a generated `_test.go` adapter. Never accept identity values directly from request JSON or flags without verifying them against the entry point's authority.

```go
policyContext, err := models.AuthenticateJobPolicyContext(credentialRequest)
if err != nil {
    return err
}
```

Ownership transfer is an update with different existing and proposed rules: the existing predicate decides who may touch the current row, while the proposed predicate constrains its new owner. For a direct migration-defined foreign-key relationship, `Exists("memberships", Equal(PolicyColumn("workspace_id"), Identity("workspace_id")))` is portable in select, delete, and the existing-row half of update. Pickle resolves the parent/child join and emits the same correlated `EXISTS` in application SQL and PostgreSQL RLS. Proposed-row relationship checks, ambiguous foreign keys, recursion, and privilege-dependent graphs stop generation rather than weakening the rule.

Standalone export carries the normalized registry, application predicate runtime, generated PostgreSQL DDL/fingerprints, policy ledger, `rls:status`, diagnostics, and conformance metadata. It does not import Pickle at runtime. In a multi-service application, exactly one service must own each protected table's generated policy state; consumers use that contract rather than emitting competing policies:

```yaml
services:
  api:
    dir: services/api
    row_policy_owner: true
  worker:
    dir: services/worker
```

### Enforcement classifications

- `application-enforced + generated-rls (live catalog uninspected)` means the normalized portable rule feeds both generators, but Squeeze has not inspected PostgreSQL.
- `application-enforced` means the explicitly acknowledged application-only plan is enforced on all generated query paths.
- `unproven` means reachability, context, raw access, manual RLS composition, or another proof obligation is unresolved. Pickle never upgrades absence of findings into a live `dual-enforced` claim.

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
2. **Role and row policies** -- roles, normalized row admission, and generated PostgreSQL RLS
3. **GraphQL policies** -- API surface exposure

This ordering guarantees that tables and backfills exist before protection is enabled, and that roles exist before GraphQL policies reference them. A schema migration and its later policy transition are separate recorded phases: failure in the policy phase does not pretend to roll back an already applied schema batch.

## State tracking

Role and generated row-policy transitions track state in the `rbac_changelog` table in generated Pickle projects. Standalone exports use a dedicated `row_policy_changelog`. GraphQL policies track state in `graphql_changelog`. They use the same state machine as migrations:

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
pickle policies:rollback    # rolls back the latest role/row-policy batch
```

`Down()` must be symmetric with `Up()`. If `Up()` creates a role, `Down()` must drop it. If `Up()` grants permissions, `Down()` must revoke them.

## CLI

```bash
pickle make:policy          # Scaffold a policy file
pickle policies:status      # Show applied/pending status of all policies
pickle policies:rollback    # Roll back the latest role/row-policy batch
pickle policies:rows        # List normalized row-policy classifications
pickle policies:row users   # Inspect one protected table
pickle policies:explain users select authenticated
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
