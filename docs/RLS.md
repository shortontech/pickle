# PostgreSQL Row-Level Security

Pickle migrations have first-party operations for PostgreSQL row-level security (RLS). Pickle owns the policy lifecycle and preserves it through schema inspection and standalone export; the predicate remains explicit SQL because PostgreSQL policy expressions can use application-specific functions, joins, and settings.

RLS operations are PostgreSQL-only. Applying a migration containing them with another driver returns an error instead of silently ignoring the policy.

## Creating a policy

```go
func (m *CreateMessageIsolation_2026_07_16_120000) Up() {
    m.EnableRLS("messages")
    m.ForceRLS("messages")

    m.CreateRLSPolicy("messages", "message_scope", func(p *RLSPolicy) {
        p.For(RLSAll).
            To("dill_app").
            UsingExpression(SQLPredicate(
                "workspace_id = current_setting('dill.workspace_id')::uuid",
            )).
            WithSameCheck()
    })
}

func (m *CreateMessageIsolation_2026_07_16_120000) Down() {
    m.DropRLSPolicy("messages", "message_scope")
    m.NoForceRLS("messages")
    m.DisableRLS("messages")
}
```

Commands are `RLSAll`, `RLSSelect`, `RLSInsert`, `RLSUpdate`, and `RLSDelete`. `To` accepts one or more PostgreSQL roles. If omitted, PostgreSQL uses `PUBLIC`.

Use `UsingExpression` for row visibility and `WithCheckExpression` for rows created or changed by a command. `WithSameCheck` copies the `USING` expression to `WITH CHECK`. Pickle rejects invalid PostgreSQL combinations such as `WITH CHECK` on a `SELECT` policy or `USING` on an `INSERT` policy.

The expression passed as `SQLPredicate` is raw migration SQL. Keep request data out of it. Dynamic request or job identity belongs in a transaction-local setting.

## Supplying request context

Generated query code exposes `SetLocal` on a transaction:

```go
err := models.WithTransaction(func(tx *models.Tx) error {
    if err := tx.SetLocal("dill.workspace_id", workspaceID.String()); err != nil {
        return err
    }

    messages, err := tx.QueryMessage().All()
    // Every statement in this transaction sees the RLS setting.
    return err
})
```

`SetLocal` calls PostgreSQL `set_config(name, value, true)` with bound parameters. The value lasts only for the transaction, so pooled connections cannot leak one request's identity into another. RLS-protected queries should not run outside that transaction.

## Roles, grants, and helper functions

Role creation, grants, and application-specific SQL helper functions remain ordinary `RawSQL` migration statements. Those objects are broader PostgreSQL administration primitives rather than policy definitions. Keeping them in migrations makes their lifecycle reviewable and prevents raw policy SQL from leaking into regular application code.
