# 072 — Seeder execution, safety, and tooling

## Depends on

070 and 071.

## Goal

Execute generated seed graphs reproducibly and safely through the compiled
Pickle application, with CLI, Squeeze, MCP, and export parity.

## Commands

Generate built-in commands:

```text
db:seed                         run the default root scenario
db:seed CRMSeeder               run one named root scenario
db:seed --seed 8675309          reproduce an exact data set
db:seed --list                  list runnable scenarios
db:seed --dry-run               validate and print the resolved graph
migrate:fresh --seed            recreate schema and run the default scenario
```

`pickle db:seed ...` forwards to `go run ./cmd/server/ db:seed ...` using the
same compiled-command mechanism as migrations. `pickle make:seeder` belongs to
the Pickle development CLI.

There is no implicit seed during ordinary `migrate`. A project may opt into a
named scenario after `migrate:fresh`, in tests, or through an explicit command.
Internal Pickle catalog migrations remain migration-owned and do not silently
invoke application scenarios.

## Determinism

Every seed execution has one explicit 64-bit root seed. When omitted, Pickle
generates and prints it before mutation begins. The same schema, seeder source,
driver semantics, and root seed must produce the same application-supplied
values and graph shape.

Randomness is derived into stable substreams by scenario identity, node path,
row ordinal, column, and retry count. Adding an unrelated field or sibling
scenario must not reshuffle every existing value.

Database-generated timestamps, sequences, and engine-specific defaults are
reported as nondeterministic in `--dry-run`. Tests requiring exact output use
Pickle-side deterministic providers.

Locale and country providers are versioned with Pickle. Provider changes are
treated as observable generation changes and documented in release notes.

## Transactions and failure behavior

The default execution unit is one transaction per root scenario. Validation,
graph resolution, type conversion, and static uniqueness planning occur before
the first insert. Any insertion failure rolls back the scenario.

Scenario seeders may opt into documented chunking for very large data sets,
but chunking must be explicit because it permits partial completion. Generated
inserts are parameterized and use the configured `database/sql` driver.

Errors identify the scenario, graph path, row ordinal, table, column, and safe
reason. Values for encrypted, sealed, password, token, secret, credential, or
other sensitive columns are never included in errors or dry-run output.

## Repeat execution policies

Every root scenario declares one policy:

```go
func (CRMSeeder) Policy() SeedPolicy { return InsertOnly }
```

Initial policies:

- `InsertOnly` — fail on conflicting unique or primary keys;
- `InsertOrIgnore` — skip rows only on a declared stable identity;
- `Upsert` — update an explicit allowlist of columns using a declared stable
  identity;
- `ReplaceScenario` — delete only rows previously tagged to this scenario,
  when project metadata explicitly enables seed provenance.

`InsertOrIgnore` and `Upsert` require `UniqueBy(...)` or an authoritative
primary key. Pickle never guesses a conflict target. Destructive replacement
is disabled by default and cannot target tables without provenance.

## Environment safety

`db:seed` is allowed by default only when `APP_ENV` is `local`, `development`,
or `test`. Other environments require both:

```text
db:seed --force --confirm-environment production
```

Projects may prohibit production seeding entirely. Non-interactive execution
must use flags; commands never hang waiting for a prompt. `--dry-run` remains
available everywhere because it does not mutate data.

## Squeeze

Add framework-aware rules:

- `seeder_missing_value` — a required column has no resolvable value source;
- `seeder_type_mismatch` — a value seeder return type cannot satisfy a column;
- `seeder_ambiguous_relationship` — `For` matches multiple foreign keys;
- `seeder_incomplete_composite_key` — only part of a composite identity flows;
- `seeder_unstable_identity` — repeat policy lacks a declared unique identity;
- `seeder_sensitive_literal` — likely secrets or production credentials appear
  as literals in seed source;
- `seeder_nondeterministic` — direct global randomness, wall-clock time, or
  unversioned external data is used outside an explicit unsafe escape hatch;
- `seeder_production_unsafe` — project configuration permits unconfirmed
  production mutation.

Rules rely on schema and typed seeder metadata. Names alone do not establish a
secret, relationship, identity, or compatible type when stronger metadata is
available.

## MCP visibility

Add read-only tools:

```text
seeders:list
seeders:show {name}
seeders:plan {name, seed?}
```

They report root scenarios, dependencies, estimated row counts, tables,
relationships, repeat policies, required environment confirmation, and field
seeder kinds. Plans redact sensitive values and never execute inserts.

Mutation remains available through the existing explicit command execution
surface rather than an ambient MCP read tool.

## Export and generated-code boundary

Generated seeder execution has zero runtime dependency on Pickle. `pickle
export` preserves:

- field seed metadata required by exported scenarios;
- generated deterministic providers;
- scenario graph and relationship ordering;
- `db:seed` command behavior and safety checks;
- parameterized driver-specific insert and upsert behavior.

Unsupported provider or driver behavior blocks export with an actionable
report rather than silently changing generated values.

## Documentation

Create `docs/Seeders.md` covering:

1. migration field seeders;
2. custom value and row seeders;
3. scenario graphs and relationships;
4. precedence and overrides;
5. deterministic root seeds;
6. repeat policies and environment safety;
7. composite scope identities;
8. testing and CI usage.

Update migrations, commands, Squeeze, MCP, export, and getting-started docs.

## Implementation plan

1. Generate the `db:seed` command and deterministic execution context.
2. Implement validation, transaction boundaries, repeat policies, and guards.
3. Add Squeeze rules and sensitive-value redaction.
4. Add MCP discovery and dry-run plan rendering.
5. Preserve behavior through export and add end-to-end fixtures.

## Likely files

- `pkg/generator/command_generator.go`
- new generated seeder runtime and provider packages
- `pkg/cooked/command.go`
- `cmd/pickle/main.go`
- `pkg/squeeze/*`
- `pkg/mcp/server.go`
- `pkg/exporter/exporter.go`
- `docs/Seeders.md` and related docs

## Tests

- Identical root seeds reproduce values and graph shape.
- Stable substreams prevent unrelated fields from reshuffling existing values.
- Scenario failures roll back all inserts by default.
- Unique retry exhaustion is deterministic and actionable.
- Insert-only, ignore, upsert, and provenance replacement obey explicit
  identities and driver semantics.
- Sensitive data is redacted from errors, logs, MCP, and dry runs.
- Password composites concatenate their declared fields in order, are
  reconstructible from the seeded row, and are hashed before insertion.
- Production execution requires the documented confirmation flags.
- Squeeze catches each proven unsafe condition without name-only false
  positives.
- Exported seeders produce equivalent rows without importing Pickle.
- `go test ./...` and `pickle squeeze --hard` pass.

## Acceptance criteria

1. Seeder execution is reproducible, transactional, and explicit.
2. Repeat behavior never guesses conflict identity or destructive scope.
3. Tooling exposes plans without leaking secrets or mutating data.
4. Generated and exported applications retain zero Pickle runtime dependency.

## Non-goals

- Production backfills, migrations, or data anonymization.
- Distributed seeding across independently committed databases.
- Fetching live third-party data as a built-in provider.
- Hiding nondeterministic database defaults as deterministic output.
