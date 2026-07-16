# PostgreSQL Row-Level Security

Pickle normally generates PostgreSQL row-level security from the portable subset of [row policies](Policies.md). Define authorization once: generated query builders enforce the normalized rule on every driver, while PostgreSQL receives an equivalent `ENABLE` + `FORCE ROW LEVEL SECURITY` policy generated from that same rule.

Do not copy a portable Pickle row policy into hand-written RLS. PostgreSQL permissive policies combine with `OR`, so an independent manual policy can silently broaden access.

## Generated RLS

For each protected table and physical command, Pickle emits at most one aggregate permissive policy. Subject dispatch stays inside its predicate. Stable `pickle_` names are reserved, limited to PostgreSQL's identifier length, and carry a normalized fingerprint in policy comments.

| Logical position | PostgreSQL position |
|---|---|
| select/delete existing row | `USING` |
| insert proposed row | `WITH CHECK` |
| update existing row | `USING` |
| update proposed row | `WITH CHECK` |

Missing, empty, malformed, or wrongly typed identity settings evaluate false. Generated helpers read `current_setting(name, true)` without throwing. Roles are application role slugs encoded as JSON identity data; Pickle does not create one PostgreSQL login per application role.

Protected PostgreSQL work must use a transaction carrying verified context:

```go
err := models.WithTransaction(func(tx *models.Tx) error {
    if err := tx.WithPostgresPolicyContext(policyContext); err != nil {
        return err
    }
    _, err := tx.QueryMessage().All()
    return err
})
```

Settings use parameterized `set_config(..., true)`, last only for the transaction, and cannot leak through a pooled connection. `pickle.identity.*` is reserved from `SetLocal`; the context is write-once and nested savepoints inherit it.

The runtime PostgreSQL role must not be superuser or have `BYPASSRLS`. Generated tables are forced by default, which also protects against table-owner bypass. Keep migration/owner credentials separate from runtime credentials.

Run explicit, read-only drift inspection with:

```bash
pickle rls:status
```

It compares managed names, commands, permissiveness, fingerprints, enabled/forced state, unexpected permissive policies, orphaned generated policies, ownership, superuser, and `BYPASSRLS`. MCP `rls_status` shows desired state without unexpectedly connecting to a live database.

## Manual restrictive defense in depth

`CreateRLSPolicy` remains an advanced migration-only escape hatch for database-only constraints. On a Pickle-protected table, a manual companion must be genuinely narrowing and explicitly registered:

```go
m.CreateRLSPolicy("messages", "messages_not_archived", func(p *RLSPolicy) {
    p.For(RLSSelect).
        To("dill_app").
        UsingExpression(SQLPredicate("archived_at IS NULL")).
        RestrictiveDefenseInDepth()
})
```

This emits `AS RESTRICTIVE`, so PostgreSQL combines it with generated permissive admission using `AND`. It remains database-only and is never assumed by application enforcement. Squeeze rejects manual permissive policies, the reserved `pickle_` namespace, weakening `DISABLE`/`NO FORCE` operations, and raw policy-affecting SQL beside a protected table.

Raw SQL is still acceptable in migrations for roles, grants, and helper functions. RLS operations are PostgreSQL-only and error on other drivers rather than being ignored.
