# 029 — Config Reload Credential Scrubbing

**Status:** Draft

## Problem

`pkg/cooked/reload.go:82-89` logs and returns config reload errors verbatim:

```go
if err != nil {
    log.Printf("pickle: config reload failed error=%q", err)
    // ...
    json.NewEncoder(w).Encode(map[string]any{
        "status":  "error",
        "message": err.Error(),
    })
    return
}
```

If `ConfigReloadFunc` validates a database DSN and the DSN contains credentials (`postgres://user:password@host/db`), the error message may include the full DSN. This leaks credentials to:

1. **Application logs** — which may be shipped to a centralized logging system (Datadog, CloudWatch, etc.) accessible to a wider team
2. **The HTTP response** — which goes to whoever can reach `/pickle/config/reload` (internal network, but still)

The endpoint is documented as internal-only, but defense in depth says: don't log secrets even on internal endpoints.

## Fix

### 1. Scrub the HTTP response

Never return the raw error to the caller. Return a generic message and a correlation ID:

```go
reloadID := uuid.New().String()[:8]
log.Printf("pickle: config reload failed reload_id=%s error=%q", reloadID, scrubDSN(err.Error()))
json.NewEncoder(w).Encode(map[string]any{
    "status":    "error",
    "message":   "config reload failed — check application logs",
    "reload_id": reloadID,
})
```

### 2. Scrub DSNs in log output

Add a `scrubDSN` helper that replaces credentials in common DSN formats:

```go
// scrubDSN replaces password components in DSN strings with "***".
// Handles: postgres://user:pass@host, mysql://user:pass@host,
// and key=value formats (password=secret).
func scrubDSN(s string) string
```

Patterns to scrub:
- `://user:password@` → `://user:***@`
- `password=secret` → `password=***` (case-insensitive, handles `PASSWORD`, `passwd`)
- `secret=abc123` → `secret=***`

This is best-effort — it won't catch every possible format, but it catches the common ones (libpq, pgx, MySQL DSN, Redis URL).

### 3. Scrub the `changes` list

The reload response includes `"changes": ["DB_PASSWORD", "JWT_SECRET"]` — the list of env vars that changed. The var *names* are safe to return (they're not secrets), but double-check that `ConfigReloadFunc` never puts values in this list.

## Scope

- `pkg/cooked/reload.go` — scrub error in log and response, add `scrubDSN`
- `pkg/cooked/reload_test.go` — test `scrubDSN` with postgres, mysql, redis, key=value formats
- No tickle needed — `reload.go` is in `pkg/cooked/` and gets embedded via the normal tickle flow

## Not in Scope

- Scrubbing log output globally — that's a bigger project. This spec only covers the config reload endpoint.
- Encrypting logs — that's infrastructure, not framework.
