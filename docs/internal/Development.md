# Development

## Building and Running

```bash
# Build the CLI
go build -o pickle ./cmd/pickle/

# Run tickle to regenerate embedded templates (after changing pkg/cooked/ or pkg/schema/)
go run ./pkg/tickle/cmd/

# Run tests
go test ./...

# Generate from a test project
go run ./cmd/pickle/ generate --project ./testdata/basic-crud/

# Run in watch mode
go run ./cmd/pickle/ --watch --project ./testdata/basic-crud/
```

## Go Version

Requires Go **1.25.7** or later.

## Dependencies

Pickle keeps its dependency footprint minimal. Only `fsnotify` is a direct dependency; the rest are indirect (used by cooked runtime code tickled into user projects).

**Pickle CLI:**
- `github.com/fsnotify/fsnotify` — File watching for `--watch`

**Generated/cooked runtime:**
- `github.com/go-playground/validator/v10` — Struct validation (in generated bindings)
- `github.com/shopspring/decimal` — Decimal types for financial math (in generated models)
- `github.com/google/uuid` — UUID support (in generated models)
- `github.com/robfig/cron/v3` — Cron/scheduled job execution
- `github.com/vektah/gqlparser/v2` — GraphQL schema parsing
- `golang.org/x/crypto` — AES-SIV/AES-GCM encryption for encrypted columns
- `golang.org/x/oauth2` — OAuth2 auth driver
- `github.com/modelcontextprotocol/go-sdk` — MCP server protocol
- `database/sql` + `net/http` — Go stdlib

## Linting Generated Code

Go's linter will complain about unused functions in generated files. Solutions:

1. Add `//nolint` directives to generated file headers
2. Configure `golangci-lint` to exclude generated `_gen.go` files and `models/`

The unused functions cost nothing at runtime. They're bytes in a binary. Your binary is maybe 2MB bigger. Your development velocity is 10x faster.
