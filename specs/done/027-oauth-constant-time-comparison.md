# 027 — OAuth Client Credential Timing Attack

**Status:** Draft

## Problem

`pkg/cooked/auth/oauth/driver.go:132` validates client credentials with `!=`:

```go
if clientID != d.clientID || clientSecret != d.clientSecret {
    return ctx.JSON(401, ...)
}
```

Go's `!=` on strings short-circuits on the first mismatched byte. An attacker measuring response times can:

1. Enumerate valid client IDs (the `||` exits early if clientID matches but clientSecret doesn't — different timing than both failing on the first comparison).
2. Progressively guess secrets byte-by-byte through timing analysis.

This is a textbook timing side-channel. The `jwt` driver already uses `hmac.Equal()` for signature verification — the OAuth driver should do the same.

## Fix

Replace the `!=` comparisons with `subtle.ConstantTimeCompare`:

```go
import "crypto/subtle"

idMatch := subtle.ConstantTimeCompare([]byte(clientID), []byte(d.clientID))
secretMatch := subtle.ConstantTimeCompare([]byte(clientSecret), []byte(d.clientSecret))
if idMatch&secretMatch != 1 {
    return ctx.JSON(401, map[string]string{
        "error":             "invalid_client",
        "error_description": "invalid client credentials",
    })
}
```

Both comparisons must run regardless of the first result (no short-circuit `||`). The bitwise `&` ensures constant-time evaluation of both branches.

**Note:** `subtle.ConstantTimeCompare` returns 0 for different-length strings without leaking length through timing, so length mismatches are also safe.

## Scope

One file, one line. `pkg/cooked/auth/oauth/driver.go:132`. Run tickle after to update the embedded template.

## Test

Add a test that validates both valid and invalid credentials still produce the correct responses. Timing is difficult to unit test — the fix is verifiable by code review.
