# Auth

Pickle ships three auth drivers: **JWT** (default), **Sessions**, and **OAuth2 Client Credentials**. All implement the same `AuthDriver` interface. Set `AUTH_DRIVER` in your `.env` to choose.

## JWT

Stateless authentication with HMAC-signed tokens. Tokens are tracked in a `jwt_tokens` table for revocation support — a token must exist in the table and not be revoked to be valid.

### Issuing tokens

```go
// In your login controller
func (c AuthController) Login(ctx *pickle.Context) pickle.Response {
    req, bindErr := requests.BindLoginRequest(ctx.Request())
    if bindErr != nil {
        return ctx.JSON(bindErr.Status, bindErr)
    }

    user, err := models.QueryUser().WhereEmail(req.Email).First()
    if err != nil || !checkPassword(req.Password, user.PasswordHash) {
        return ctx.Unauthorized("invalid credentials")
    }

    driver := auth.Driver("jwt").(*jwt.Driver)
    token, err := driver.SignToken(jwt.Claims{
        Subject: user.ID.String(),
        Role:    user.Role,
    })
    if err != nil {
        return ctx.Error(err)
    }

    return ctx.JSON(200, map[string]string{"token": token})
}
```

`SignToken` generates a JTI (UUID), signs the token, and inserts it into the `jwt_tokens` table. The token is not valid unless it's in the table.

### Revocation

Revoke a single token by JTI (logout):

```go
driver := auth.Driver("jwt").(*jwt.Driver)
claims := ctx.Auth().Claims.(jwt.Claims)
driver.RevokeToken(claims.JTI)
```

Revoke all tokens for a user (password change, account compromise):

```go
driver := auth.Driver("jwt").(*jwt.Driver)
driver.RevokeAllForUser(ctx.Auth().UserID)
```

If the JWT secret rotates, old tokens fail signature validation before the DB is ever hit. Dead rows in `jwt_tokens` can be pruned by `expires_at`.

### Claims

```go
type Claims struct {
    JTI       string `json:"jti"`  // auto-generated UUID
    Subject   string `json:"sub"`  // user ID
    Issuer    string `json:"iss"`  // from JWT_ISSUER
    ExpiresAt int64  `json:"exp"`  // from JWT_EXPIRY
    IssuedAt  int64  `json:"iat"`  // auto-set
    Role      string `json:"role"` // user role
}
```

### Configuration

```
AUTH_DRIVER=jwt
JWT_SECRET=your-secret-key
JWT_ISSUER=myapp
JWT_EXPIRY=3600
JWT_ALGORITHM=HS256
```

### Migration

Pickle generates `database/migrations/jwt/2026_03_03_100000_create_jwt_tokens_table_gen.go`:

| Column | Type | Description |
|--------|------|-------------|
| `jti` | `string(255)` | Primary key, the JWT ID |
| `user_id` | `uuid` | Foreign key for bulk revocation |
| `expires_at` | `timestamp` | For pruning expired rows |
| `revoked_at` | `timestamp?` | Null = active, set = revoked |
| `created_at` | `timestamp` | When the token was issued |

## Sessions

Database-backed session authentication using cookies. See the CSRF section in [Middleware](Middleware.md) — session auth requires CSRF protection on all state-changing routes.

### Creating sessions

```go
func (c AuthController) Login(ctx *pickle.Context) pickle.Response {
    // ... validate credentials ...

    cookies, err := session.Create(ctx, user.ID.String(), user.Role)
    if err != nil {
        return ctx.Error(err)
    }

    return cookies.Apply(ctx.JSON(200, map[string]string{"ok": "true"}))
}
```

`Create` inserts a session row, returns cookies for both the session and CSRF token. `Apply` chains them onto the response.

### Destroying sessions

```go
func (c AuthController) Logout(ctx *pickle.Context) pickle.Response {
    resp, err := session.Destroy(ctx)
    if err != nil {
        return ctx.Error(err)
    }
    return resp
}
```

### Session data

Read and write arbitrary key-value data in the session's `payload` JSONB:

```go
// Store a value
session.Put(ctx, "onboarding_step", "3")

// Read it back
step, err := session.Get(ctx, "onboarding_step")
```

### Configuration

```
AUTH_DRIVER=session
SESSION_COOKIE=session_id
SESSION_TTL=86400
SESSION_SECRET=your-csrf-secret
CSRF_COOKIE=csrf_token
```

`SESSION_SECRET` is required for CSRF protection.

## OAuth2 Client Credentials

Machine-to-machine authentication using the OAuth2 client credentials grant. The driver acts as an OAuth2 provider — issuing opaque tokens at a token endpoint and validating Bearer tokens on protected routes.

### Token endpoint

Wire the token endpoint into your routes:

```go
// routes/web.go
driver := auth.Driver("oauth").(*oauth.Driver)
r.Post("/oauth2/token", driver.TokenEndpoint)
```

Clients authenticate with HTTP Basic auth and request a token:

```
POST /oauth2/token
Authorization: Basic base64(client_id:client_secret)
Content-Type: application/x-www-form-urlencoded

grant_type=client_credentials
```

Response:

```json
{"access_token": "...", "token_type": "bearer", "expires_in": 3600}
```

### Validating tokens

When `AUTH_DRIVER=oauth`, the auth middleware validates Bearer tokens against the `oauth_tokens` table and checks expiry. `ctx.Auth()` returns `UserID` set to the client ID and `Role` set to `"client"`.

### Configuration

```
AUTH_DRIVER=oauth
OAUTH_CLIENT_ID=my-client-id
OAUTH_CLIENT_SECRET=my-client-secret
OAUTH_TOKEN_EXPIRY=3600
```

### Migration

Pickle generates `database/migrations/2026_03_03_200000_create_oauth_tokens_table_gen.go`:

| Column | Type | Description |
|--------|------|-------------|
| `token` | `string(255)` | Primary key, the opaque token |
| `client_id` | `string(255)` | Which client owns this token |
| `expires_at` | `timestamp` | Token expiry |
| `created_at` | `timestamp` | When the token was issued |

## Auth middleware

All drivers work with the same middleware. Use `auth.DefaultAuthMiddleware` or write your own:

```go
// routes/web.go
r.Group("/api", auth.DefaultAuthMiddleware, func(r *pickle.Router) {
    r.Get("/me", controllers.UserController{}.Show)
})
```

The active driver is determined by `AUTH_DRIVER` at runtime. Controllers don't need to know which driver is in use — `ctx.Auth()` works the same either way.
