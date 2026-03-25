# 030 — JWT Generic Error Messages

**Status:** Draft

## Problem

`pkg/cooked/auth/jwt/driver.go:169` includes the expected algorithm in the error message:

```go
if header.Alg != alg {
    return nil, errors.New("jwt: algorithm mismatch (header=" + header.Alg + ", expected=" + alg + ")")
}
```

This confirms the server's expected algorithm to an attacker. The practical risk is negligible — the algorithm is typically in the JWT header of any previously-issued token, and HMAC algorithm selection isn't a meaningful attack surface when the key is strong. But it's unnecessary information disclosure, and fixing it costs nothing.

Similarly, other error messages in `ValidateToken` are highly specific:

```go
"jwt: token expired"
"jwt: invalid issuer"
"jwt: token not found (revoked or never issued)"
"jwt: token revoked"
```

These tell an attacker exactly *why* their token was rejected. For a login flow this is fine (the user needs to know). For a bearer token on an API, the client doesn't need to distinguish between "expired" and "revoked" — both mean "get a new token."

## Fix

### External errors (returned to callers / HTTP responses): generic

Replace all `ValidateToken` error returns with a single generic error:

```go
var ErrInvalidToken = errors.New("jwt: invalid token")
```

All validation failures — malformed, bad algorithm, bad signature, expired, wrong issuer, revoked, not found — return `ErrInvalidToken`. The caller (auth middleware) maps this to 401.

### Internal logging: detailed

Log the specific reason before returning the generic error, so operators can debug token issues:

```go
if header.Alg != alg {
    log.Printf("jwt: rejected token jti=%s reason=algorithm_mismatch header=%s expected=%s", claims.JTI, header.Alg, alg)
    return nil, ErrInvalidToken
}
```

The JTI (if available at that point in validation) provides correlation without leaking it to the client.

### SignToken errors: keep specific

Errors from `SignToken` (secret not configured, DB insert failed) are programming/infrastructure errors, not attacker-facing. These should remain specific for debugging.

## Scope

- `pkg/cooked/auth/jwt/driver.go` — `ValidateToken` method only
- Add `var ErrInvalidToken` sentinel error
- Update tests that assert on specific error messages to check for `ErrInvalidToken`
- Run tickle after

## Severity

Low. This is a hardening measure, not a vulnerability fix. The information disclosed doesn't enable an attack that wasn't already possible.
