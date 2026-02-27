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
