# Commands

The CLI command system for your compiled binary. The generated `App` handles initialization, command dispatch, and HTTP serving.

## How it works

Your `cmd/server/main.go` creates an `App` and calls `Run()`:

```go
func main() {
    commands.NewApp().Run(os.Args[1:])
}
```

With no arguments, the app starts the HTTP server. With a command name, it dispatches to that command instead.

## Built-in commands

Pickle generates these commands automatically:

| Command | Description |
|---------|-------------|
| `migrate` | Run pending database migrations |
| `migrate:rollback` | Roll back the last migration batch |
| `migrate:fresh` | Drop all tables and re-run migrations |
| `migrate:status` | Show migration status |

Run them via: `go run ./cmd/server/ migrate`

## App lifecycle

The generated `NewApp()` function builds an `App` with:

1. **Init function** — loads config, opens database connection, sets `models.DB`
2. **Serve function** — registers routes on a `ServeMux` and starts HTTP
3. **Commands** — migrate, rollback, etc.

`App.Run(args)` calls init first, then either dispatches a command or starts serving.

## Custom commands

Implement the `Command` interface:

```go
type Command interface {
    Name() string
    Description() string
    Run(args []string) error
}
```

Register commands in the generated `NewApp()` by placing them in `app/commands/`.

## Scaffold commands

The Pickle CLI includes scaffolding commands (run from your project root):

| Command | Description |
|---------|-------------|
| `pickle make:controller` | Scaffold a new controller |
| `pickle make:migration` | Scaffold a new migration with timestamp |
| `pickle make:request` | Scaffold a new request class |
| `pickle make:middleware` | Scaffold a new middleware |
| `pickle make:job` | Scaffold a new cron job (creates a job struct in `app/jobs/`) |

## Export

`pickle export` converts a Pickle project into a standalone Go application. Use it when you want to leave the Pickle workflow and continue with plain Go.

```bash
pickle export --out ./dist/myapp
cd ./dist/myapp
go test ./...
```

The exported project includes:

- GORM models generated from migrations and views
- SQL migration files as `.up.sql` and `.down.sql` pairs
- copied controllers, routes, requests, middleware, and config
- standalone HTTP, auth, request binding, and server support code
- `EXPORT_REPORT.md` describing unsupported generated subsystems

Generated Pickle imports are removed. The exported app has no runtime dependency on Pickle.

Use `--force` to export into a non-empty output directory:

```bash
pickle export --out ./dist/myapp --force
```

## Encryption key management

These commands manage encryption keys for columns marked `.Encrypted()` or `.Sealed()`. They are planned and may not be fully implemented yet.

| Command | Description |
|---------|-------------|
| `key:rotate` | Re-encrypt all encrypted columns with a new key. Set `ENCRYPTION_KEY` to the new key and `ENCRYPTION_KEY_PREVIOUS` to the old one, then run this command. |
| `key:swap` | Swap the active and previous keys. Useful for completing a rotation or rolling back. |
| `key:cleanup` | Remove the previous key after rotation is verified complete. Clears `ENCRYPTION_KEY_PREVIOUS`. |
