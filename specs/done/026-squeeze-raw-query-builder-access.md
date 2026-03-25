# 026 — Squeeze Rule: `raw_query_builder_access`

**Status:** Draft

## Problem

Pickle generates typed query wrappers (`UserQuery`, `TransferQuery`, etc.) that expose safe, per-column methods: `WhereEmail(...)`, `OrderByCreatedAt(...)`, `SelectStatus()`. These methods hardcode column names as string literals, making SQL injection structurally impossible.

But every typed query embeds `*QueryBuilder[T]` as a public field. A user can bypass the typed API:

```go
// Safe — generated method, column name is a hardcoded literal
models.QueryUser().OrderByEmail("ASC")

// Unsafe — reaches through to the base builder
models.QueryUser().QueryBuilder.OrderBy(ctx.Query("sort"), "ASC")
```

`OrderBy` now validates that the column is a valid identifier (no semicolons, no SQL keywords as part of injection), but the typed methods are still the correct API. Reaching through to `QueryBuilder` directly is always a code smell — it means the developer is working around the type-safe layer that exists specifically to prevent mistakes.

This applies to all `QueryBuilder` methods that accept column names:
- `OrderBy(column, direction string)`
- `Where(column string, value any)` (though this is parameterized, column names aren't)
- `WhereIn(column string, values []any)`
- `WhereNotIn(column string, values []any)`

## Goal

A squeeze rule that flags direct access to the embedded `QueryBuilder` or `ImmutableQueryBuilder` field in controller code. Not an error — a warning that says "you're bypassing the typed API, here's the typed method you probably want."

## Design

### Rule: `raw_query_builder_access`

**Severity:** Warning

**Detection:** AST inspection of controller methods. Walk call expressions looking for selector chains where an intermediate selector is `QueryBuilder` or `ImmutableQueryBuilder`:

```go
// Matches:
q.QueryBuilder.OrderBy(...)
q.QueryBuilder.Where(...)
q.ImmutableQueryBuilder.OrderBy(...)
models.QueryUser().QueryBuilder.OrderBy(...)

// Does NOT match:
q.OrderByEmail(...)          // typed method, correct
q.WhereEmail(...)            // typed method, correct
q.QueryBuilder.First()       // terminal method, no column name risk
q.QueryBuilder.All()         // terminal method, no column name risk
```

**Flagged methods:** `OrderBy`, `Where`, `WhereIn`, `WhereNotIn`. These are the methods where the first argument is a column name string. Terminal methods (`First`, `All`, `Count`, `Create`, `Update`, `Delete`) are not flagged — accessing them through the embedded field is harmless.

**Message format:**

```
warning: direct QueryBuilder.OrderBy() access bypasses typed query API
  → use q.OrderByEmail("ASC") instead of q.QueryBuilder.OrderBy("email", "ASC")
  controllers/user_controller.go:42
```

### Implementation

```go
func ruleRawQueryBuilderAccess(ctx *AnalysisContext) []Finding {
    var findings []Finding
    flagged := map[string]bool{
        "OrderBy":    true,
        "Where":      true,
        "WhereIn":    true,
        "WhereNotIn": true,
    }
    builderNames := map[string]bool{
        "QueryBuilder":          true,
        "ImmutableQueryBuilder": true,
    }

    for _, m := range ctx.Methods {
        ast.Inspect(m.Body, func(n ast.Node) bool {
            call, ok := n.(*ast.CallExpr)
            if !ok {
                return true
            }
            sel, ok := call.Fun.(*ast.SelectorExpr)
            if !ok || !flagged[sel.Sel.Name] {
                return true
            }
            // Check if the receiver is .QueryBuilder or .ImmutableQueryBuilder
            inner, ok := sel.X.(*ast.SelectorExpr)
            if !ok || !builderNames[inner.Sel.Name] {
                return true
            }
            findings = append(findings, Finding{
                Rule:     "raw_query_builder_access",
                Severity: SeverityWarning,
                File:     m.File,
                Line:     m.Fset.Position(call.Pos()).Line,
                Message:  "direct " + inner.Sel.Name + "." + sel.Sel.Name + "() bypasses typed query API — use the generated OrderBy{Column}/Where{Column} methods instead",
            })
            return true
        })
    }
    return findings
}
```

### Configuration

Disabled by default in `.squeeze.yml` — this is advisory, not blocking:

```yaml
rules:
  raw_query_builder_access: warn  # or "off" to disable
```

### Edge Cases

- **Generated code** — the search handler calls `q.OrderByEmail(sortDir)` now, not `q.QueryBuilder.OrderBy(...)`. No false positives from generated code.
- **Test files** — tests that exercise `QueryBuilder` directly (like integration tests) should be excluded. The rule only inspects `ctx.Methods` which comes from controller parsing.
- **Legitimate use** — there is no legitimate use in controllers. If the typed method doesn't exist, the migration is missing the column. The fix is to add the column, not to bypass the type system.

## Not In Scope

- Making `QueryBuilder` unexported — this would break the embedding pattern and prevent users from implementing custom query methods. The struct field must stay public.
- Flagging `Limit`, `Offset`, `Lock` etc. through `QueryBuilder` — these don't take column names and aren't security-relevant.
