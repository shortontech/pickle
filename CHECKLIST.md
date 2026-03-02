# Pickle Error Handling Checklist

Work through these items and check them off as fixed.

## Critical / High

- [x] **context.go:118** — `ctx.Resource()` converts all errors to 404 (DB outages, timeouts). Should match `ctx.Resources()` which uses `ctx.Error(err)`
- [x] **auth/jwt/driver.go:134** — Token header `alg` never read during validation. No algorithm mismatch detection or confusion defense
- [x] **config.go:87** — DSN credentials not URL-encoded. Passwords with `@`/`/`/`?` produce malformed connection strings
- [x] **cmd/pickle/main.go:94** — Project name used as path without traversal validation
- [x] **pkg/mcp/server.go:268** — Same path traversal issue in MCP `projectCreate` (higher risk, remote caller)
- [x] **pkg/squeeze/rules.go:441** — `rate_limit_auth` only checks controllers with "Auth" in name. Login endpoints on other controllers invisible

## Medium — Generator

- [x] **generate.go:237** — `os.MkdirAll` error silently dropped
- [x] **generate.go:574** — `ScanRouteVars` error blank-identified, parse errors swallowed
- [x] **generate.go:262** — Unknown column type maps to zero-value (UUID) silently via map lookup
- [x] **registry_generator.go:79** — Migration parse errors treated same as "no structs found"
- [x] **helpers.go:28** — `toLowerFirst` uses byte arithmetic `s[0]+32`, corrupts non-ASCII / already-lowercase
- [x] **schema_inspector.go:385** — Invalid timestamp suffix returns full name as sort key
- [x] **command_generator.go:36** — Parse errors in command files silently skip the file
- [x] **command_generator.go:125** — Parse errors in routes files silently skip the file
- [x] **command_generator.go:297** — `format.Source` error returned without context

## Medium — Runtime (cooked)

- [ ] **context.go:84** — `SetAuth` silently wraps non-`*AuthInfo` claims, UserID/Role empty with no warning
- [x] **router.go:127** — No duplicate route detection before ServeMux panic
- [x] **config_generator.go:157** — Unknown DB connection panics instead of returning startup error

## Medium — Schema DSL

- [x] **schema/table.go:44** — `Decimal()` accepts scale > precision, negatives, zero
- [x] **schema/table.go:22** — `String()` accepts zero/negative length
- [x] **schema/migration.go:97** — `AddIndex` with zero columns accepted
- [x] **schema/migration.go** — Empty strings accepted everywhere: table names, column names, rename targets, FK refs
- [x] **schema/view.go:155** — `parseColumnRef` without dot gives empty alias
- [x] **schema/view.go:40** — `View.From`/`Join` accept empty table, alias, or ON condition
- [x] **schema/view.go:86** — `SelectRaw` accepts empty name or expression

## Medium — Tickle

- [x] **tickle/scopes.go:90** — Unclosed `pickle:scope` block silently dropped
- [ ] **tickle/scopes.go:112** — `GenerateScopes` returns empty string with no error when zero blocks match

## Medium — Squeeze

- [x] **pkg/squeeze/rules.go:157** — `enum_validation` findings have empty file/line
- [x] **pkg/squeeze/rules.go:266** — Naïve `+"s"` pluralization breaks `required_fields` rule
- [ ] **pkg/squeeze/rules.go:273** — `required_fields` checks for any `Create` call in method, not the specific literal
- [ ] **pkg/squeeze/controller_parser.go:137** — Missing controllers dir silently treated as empty

## Medium — Watcher

- [x] **watcher/watcher.go:71** — `addRecursive` error silently ignored on dir create
- [x] **watcher/watcher.go:40** — Zero watched directories blocks forever with no warning

## Low

- [x] **response.go:45,53** — `w.Write` return values discarded
- [x] **auth/jwt/driver.go:189** — `hmacSign` error discarded in `hmacVerify`
- [x] **router.go:184** — Dead `trimTrailingSlash` function
- [ ] **context.go:71** — `BearerToken` can't distinguish absent header vs wrong scheme (by design — callers don't need this)
- [x] **cmd/pickle/main.go:321** — Unknown flags silently ignored in `parseMakeArgs`
- [x] **cmd/pickle/main.go:476** — `findPicklePkgDir` returns fabricated non-existent path as fallback
- [x] **scaffold/scaffold.go:117** — `sanitizeName` allows invalid Go identifiers (`123Foo`, `my-foo`)
- [x] **scaffold/scaffold.go:135** — TOCTOU race between stat and write in `writeScaffold`
- [x] **tickle/cmd/main.go:78** — `defer os.RemoveAll` inside loop, skipped by `os.Exit`
- [x] **watcher/watcher.go:68** — `timer.Reset` without draining channel
- [x] **schema/types.go:36** — `ColumnType.String()` returns `""` for in-range unmapped types
- [x] **tickle/tickle.go:53** — `Merge` returns empty-body file without error when srcDir has no .go files
