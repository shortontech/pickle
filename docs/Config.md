# Config

Application configuration using environment variables with typed config structs. Follows the Laravel pattern of config files that return typed structs.

## Writing config

```go
// config/app.go
package config

type AppConfig struct {
    Name  string
    Env   string
    Debug bool
    Port  string
    URL   string
}

func app() AppConfig {
    return AppConfig{
        Name:  Env("APP_NAME", "myapp"),
        Env:   Env("APP_ENV", "local"),
        Debug: Env("APP_DEBUG", "true") == "true",
        Port:  Env("APP_PORT", "8080"),
        URL:   Env("APP_URL", "http://localhost:8080"),
    }
}
```

```go
// config/database.go
package config

type DatabaseConfig struct {
    Default     string
    Connections map[string]ConnectionConfig
}

func database() DatabaseConfig {
    return DatabaseConfig{
        Default: Env("DB_CONNECTION", "pgsql"),
        Connections: map[string]ConnectionConfig{
            "pgsql": {
                Driver:   "pgsql",
                Host:     Env("DB_HOST", "127.0.0.1"),
                Port:     Env("DB_PORT", "5432"),
                Name:     Env("DB_DATABASE", "myapp"),
                User:     Env("DB_USERNAME", "postgres"),
                Password: Env("DB_PASSWORD", ""),
            },
        },
    }
}
```

## Conventions

- Config files live in `config/`.
- Each file defines a config struct and an unexported function that returns it.
- The function name determines the config key: `app()` → `config.App()`, `database()` → `config.Database()`.
- Pickle generates `config/pickle_gen.go` with exported accessor functions.

## Env helper

`Env(key, fallback)` reads environment variables with a default fallback. On first call, it loads `.env` from the project root. Environment variables take precedence over `.env` values.

```go
port := Env("APP_PORT", "8080")
```

## ConnectionConfig

The built-in `ConnectionConfig` type handles database connections:

```go
type ConnectionConfig struct {
    Driver   string  // "pgsql", "mysql", "sqlite"
    Host     string
    Port     string
    Name     string  // database name (or file path for sqlite)
    User     string
    Password string
}
```

It has a `DSN()` method that returns the driver-specific connection string, and is used by `OpenDB()` to establish the database connection at startup.

## Auth configuration

Auth drivers are configured via environment variables. Set `AUTH_DRIVER` to choose the active driver.

### JWT (default)

```
AUTH_DRIVER=jwt
JWT_SECRET=your-secret-key
JWT_ISSUER=myapp
JWT_EXPIRY=3600
JWT_ALGORITHM=HS256
```

### Sessions

```
AUTH_DRIVER=session
SESSION_COOKIE=session_id
SESSION_TTL=86400
SESSION_SECRET=your-csrf-secret
CSRF_COOKIE=csrf_token
```

`SESSION_SECRET` is required when using CSRF middleware. `SESSION_COOKIE` and `CSRF_COOKIE` have sensible defaults.

## .env file

The `.env` file at your project root sets defaults for local development:

```
APP_NAME=myapp
APP_ENV=local
APP_PORT=8080
DB_HOST=127.0.0.1
DB_PORT=5432
DB_DATABASE=myapp
DB_USERNAME=postgres
DB_PASSWORD=secret
AUTH_DRIVER=jwt
JWT_SECRET=change-me
```

Lines starting with `#` are comments. Values can be quoted with single or double quotes.

## Encryption configuration

If your migrations use `.Encrypted()` or `.Sealed()` columns, configure the encryption key:

```
ENCRYPTION_KEY=base64-encoded-32-byte-key
ENCRYPTION_KEY_PREVIOUS=base64-encoded-32-byte-key
```

The `EncryptionConfig` struct is generated alongside your other config:

```go
type EncryptionConfig struct {
    Key         string // active encryption key (base64-encoded)
    PreviousKey string // previous key, used during key rotation
}
```

`ENCRYPTION_KEY` is required when any table has encrypted or sealed columns. `ENCRYPTION_KEY_PREVIOUS` is only needed during key rotation (see `key:rotate` in [Commands](Commands.md)).

## RuntimeConfig and hot reload

Pickle wraps your config in a `RuntimeConfig` that uses Go's `atomic.Pointer` for lock-free reads. Config values are read millions of times per second in a busy server — an atomic pointer swap means zero contention.

To reload config without restarting the server, hit the built-in reload endpoint:

```
POST /pickle/config/reload
```

This re-reads `.env` and environment variables, builds new config structs, and atomically swaps the pointer. In-flight requests see either the old or new config — never a torn read. The endpoint is only available when `APP_ENV` is not `production` (or when explicitly enabled).
