# Security Architecture

Pickle makes the secure path the default and the insecure path impossible or visibly wrong.

## By Design — Structural Prevention

- **SQL injection** — impossible. `QueryBuilder[T]` generates parameterized queries. There's no API for string interpolation.
- **Mass assignment** — request structs define exactly which fields are accepted. POSTing `{"role": "admin"}` does nothing if `CreateUserRequest` doesn't have a `Role` field.
- **Validation bypass** — controllers call generated `Bind` functions that validate before returning the typed struct.
- **CSRF** — the session auth driver ships HMAC double-submit cookie CSRF middleware (`session.CSRF`). Tokens are generated from a random nonce HMAC-signed with the session ID using `SESSION_SECRET`. Safe methods (GET, HEAD, OPTIONS) pass through; state-changing methods require a valid `X-CSRF-TOKEN` header. Bearer-token API requests skip CSRF automatically. Cookies are set with `Secure`, `SameSite=Strict`, and `HttpOnly=false` (JS must read the token).
- **Rate limiting** — built into the router, not just middleware. Every request hits a per-IP token bucket *before* middleware or handlers execute — same level as panic recovery. Configured via `RATE_LIMIT_RPS` (default: 10) and `RATE_LIMIT_BURST` (default: 20) in `.env`. Returns 429 with `Retry-After` header. Disabled with `RATE_LIMIT=false`. For per-route overrides, `pickle.RateLimit(rps, burst)` returns a `MiddlewareFunc` that runs its own independent limiter. Proxy-aware: reads `X-Forwarded-For` and `X-Real-IP` before falling back to `RemoteAddr`. Stale buckets are cleaned up automatically.
- **Panic recovery** — the router catches panics in handlers and returns a 500 response instead of crashing the process. Recovered panics are forwarded to the `OnError` reporter for external error tracking (Sentry, Datadog, etc.).
- **Secrets** — `pickle new` scaffolds a `.gitignore` that excludes `.env` and `.env.local`. Secrets never end up in version control by default.

## By Design — RBAC and Gates

- **Role-based access control** — roles are defined in policy files, not config. `CreateRole("admin").Manages().Can("users.ban")` is code, not a database record. The policy runner applies them transactionally with full rollback support.
- **Gate enforcement** — every action requires a gate. Generate fails if a gate is missing. The generator renames the action method to unexported (`Ban()` -> `ban()`) so it can only be called through the gated model method. Same-package bypass is caught by squeeze.
- **Action audit trail** — every successful action execution is recorded in an append-only `user_actions` table in the same transaction as the action. Gate denials and failures don't produce audit rows — nothing changed, nothing to audit.
- **Column visibility** — role-specific column annotations (`ComplianceSees()`, `SupportSees()`) generate `SelectFor(role)` query scopes that restrict SELECT clauses by role. Unknown roles see only `Public()` columns. `Manages()` roles see everything.

## By Design — Auth Drivers

Pickle ships opinionated auth drivers that eliminate common JWT and session pitfalls:

- **JWT driver** — pure Go HMAC implementation (HS256/HS384/HS512), no third-party JWT library. Algorithm is pinned server-side — tokens with a mismatched `alg` header are rejected, preventing alg=none and key confusion attacks. Expiry is enforced. Issuer is validated when configured. Tokens are tracked in a `jwt_tokens` allowlist table — a token must exist in the table *and* not be revoked to be valid. Revocation is instant: `RevokeToken(jti)` for single logout, `RevokeAllForUser(userID)` for password changes or account compromise.
- **Session driver** — server-side sessions with CSRF protection built in. The `CSRF` middleware is part of the session package and works automatically when the session driver is active.

## By Review — One-File Audit

- **IDOR / broken access control** — open `routes/web.go`, see every endpoint and its middleware stack. Missing `Auth` or `RequireRole` is immediately visible.
- **Middleware gaps** — the central route file makes it obvious which endpoints are public and which are protected. A security review is a 30-second read.

## By Tooling — Standard Scanner Compatibility

Generated code is plain, idiomatic Go. `go vet`, `gosec`, `staticcheck`, Snyk, Semgrep — they all work on Pickle's output with zero configuration. No framework abstractions to unwrap, no `interface{}` soup, no runtime reflection. Security scanners see exactly what runs in production.

This is the advantage of code generation over runtime frameworks. A scanner can't reason about Goravel's magic method resolution or custom abstractions. It can reason about a struct, a function, and a parameterized query — because that's just Go.
