# Response

The return type from every controller and middleware. A simple value type with a status code, body, and headers.

You almost never construct `Response` directly — use `ctx.JSON()`, `ctx.Error()`, etc. But you can add headers to any response using the `.Header()` method.

## Adding headers

`.Header()` returns a copy with the header set, so you can chain it:

```go
return ctx.JSON(200, data).
    Header("X-Request-ID", requestID).
    Header("Cache-Control", "no-cache")
```

## Setting cookies

`.WithCookie()` returns a copy with a cookie to set on the response:

```go
return ctx.JSON(200, data).
    WithCookie(&http.Cookie{
        Name:     "session_id",
        Value:    sessionID,
        Path:     "/",
        HttpOnly: true,
        Secure:   true,
        SameSite: http.SameSiteStrictMode,
    })
```

Multiple cookies can be chained:

```go
return ctx.JSON(200, data).
    WithCookie(sessionCookie).
    WithCookie(csrfCookie)
```

## Structure

```go
type Response struct {
    StatusCode int
    Body       any
    Headers    map[string]string
    Cookies    []*http.Cookie
}
```

- `Body` is JSON-marshaled when written. If `nil`, no body is written.
- `StatusCode` defaults to 200 if body is present, 204 if nil.
- `Content-Type` defaults to `application/json` if not explicitly set.
- `Cookies` are written via `http.SetCookie()` before headers.

## Writing

The router calls `resp.Write(w)` automatically — you never call it yourself. It marshals the body to JSON, sets headers, and writes the status code.

## Method reference

| Method | Returns | Description |
|--------|---------|-------------|
| `Header(key, value)` | `Response` | Returns a copy with the header added |
| `WithCookie(c)` | `Response` | Returns a copy with a cookie to set |
| `Write(w)` | — | Serializes to an `http.ResponseWriter` (called by router) |
