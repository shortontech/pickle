# Pickle 🥒

**The world's most secure web framework.**

Pickle is a code generation framework for Go that makes secure applications trivial to build — for humans and AI alike. You write controllers, migrations, request classes, and middleware. Pickle generates plain, idiomatic Go. The output compiles to a single static binary with no runtime dependency on Pickle.

```
You write controllers.     Pickle generates models.
You write migrations.      Pickle generates query builders.
You write migrations.      Pickle generates a GraphQL API.
You write request classes.  Pickle generates validation + deserialization.
You write routes.go.       Pickle wires it all together.
```

The generated code is readable, debuggable, and `grep`-friendly. It's not magic. It's just code you didn't have to type.

### Why "Most Secure"?

Every framework claims to care about security. Django has CSRF tokens. Rails has strong parameters. Spring has Security. These are best practices bolted onto runtime frameworks — they help, but they rely on developers remembering to use them correctly every time.

Pickle is different. It makes entire vulnerability classes structurally impossible, architecturally visible, or caught at build time before code ships.

**Impossible by construction:**

- **SQL injection** — The generated query builder uses parameterized queries exclusively. There is no API for string interpolation. The unsafe path doesn't exist.
- **Mass assignment** — Request structs define exactly which fields are accepted. If `CreateUserRequest` doesn't have a `Role` field, POSTing `{"role": "admin"}` does nothing. The model never sees unvalidated input.
- **Validation bypass** — Controllers receive pre-validated, typed request structs. The generated binding layer runs validation before your code executes. There is no code path around it.
- **Encryption at rest** — Columns marked `.Encrypted()` are transparently encrypted before storage and decrypted on read. Columns marked `.Sealed()` are write-only — they can be verified but never retrieved in plaintext. The query builder enforces both: no range queries on encrypted columns, no WHERE clauses on sealed columns.
- **Data tampering** — Immutable and append-only tables are cryptographically hash-chained. Every row's `row_hash` includes the previous row's hash — tampering with any historical record breaks the chain. Periodic Merkle tree checkpoints give O(log n) inclusion proofs you can hand to an auditor.

**Impossible by construction (RBAC and actions):**

- **Ungated actions** — every action requires a gate function. The generator refuses to produce output if a gate is missing. The action method is renamed to unexported in the compiled output, so it can only be called through the gated model method.
- **Role visibility leaks** — column annotations (`ComplianceSees()`, `SupportSees()`) generate `SelectFor(role)` query scopes. Unknown roles see only `Public()` columns. `Manages()` roles see everything. Squeeze flags controllers that query role-annotated models without calling `SelectFor*`.
- **Audit trail gaps** — every successful action execution writes an append-only audit row in the same database transaction as the action. Both succeed or both roll back. No action persists without its audit record.

**Visible by convention:**

- Every endpoint, its middleware stack, and its grouping are in one file: `routes/web.go`. A missing `Auth`, `LoadRoles`, or `RequireRole` is immediately obvious — to you and to any AI reviewing your code. One file, entire API surface, 30-second security review.

**Caught at build time by Squeeze:**

- **IDOR (Insecure Direct Object Reference)** — The security industry has accepted IDOR as a manual-testing problem. No tool, framework, or scanner claims to detect all IDORs. Squeeze does. It traces route → middleware → controller → query and verifies the chain is scoped by owner. This is possible because Pickle owns all three layers: migrations define ownership columns, the router defines middleware, controllers use generated query scopes. No other framework has this because no other framework was designed to make its own security properties statically analyzable.
- **Data leakage, unbounded queries, missing rate limits, enum validation, UUID panics, missing required fields** — all caught before deployment. See [Squeeze](#squeeze-make-sure-nothings-oozing) below.

**Standard security tooling works out of the box.** Generated code is plain Go — `go vet`, `gosec`, `staticcheck`, Snyk, and Semgrep work with zero configuration. No framework abstractions to unwrap. Security scanners see exactly what runs in production.

### Squeeze: Make Sure Nothing's Oozing

`pickle squeeze` is static security analysis that understands your framework — routes, middleware, migrations, request classes — and catches vulnerabilities that generic linters can't see.

```bash
pickle squeeze              # Run full validation
pickle squeeze --hard       # Strict mode: warnings become failures
```

```
🥒 Squeezing your pickle...
🥒 Your pickle is crunchy.
```

If something's wrong, Squeeze tells you exactly where:

```
🥒 Squeezing your pickle...

  app/http/controllers/post_controller.go
    line 28 [ownership_scoping] PUT /api/posts/:id — query not scoped by owner (IDOR)

🥒 Your pickle is oozing. 1 error(s), 0 warning(s)
```

#### Rules

| Rule | Severity | What it catches |
|------|----------|----------------|
| `ownership_scoping` | error | Write routes (PUT/PATCH/DELETE) behind auth that don't scope queries by owner — IDOR vulnerabilities |
| `read_scoping` | error | Read routes (GET) behind auth that don't scope queries by owner — data leakage |
| `public_projection` | error | Unauthenticated routes returning model data without `.Public()` — leaks sensitive fields |
| `unbounded_query` | error | `.All()` without `.Limit()` — denial-of-service vector |
| `rate_limit_auth` | error | Auth endpoints (login, register) without rate limiting middleware |
| `enum_validation` | error | Status/role/type fields without `oneof=` validation — accepts arbitrary values |
| `uuid_error_handling` | error | `uuid.MustParse()` on user input — panics crash the server |
| `required_fields` | error | `Create()` calls missing NOT NULL fields — database rejects the insert |
| `no_printf` | warning | `fmt.Print*` in controllers — use structured logging |
| `param_mismatch` | error | Route parameters (`:id`) with no corresponding `ctx.Param()` call, or vice versa |
| `auth_without_middleware` | error | `ctx.Auth()` called in a controller without auth middleware on the route |
| `immutable_raw_update` | error | Raw `UPDATE` on an immutable or append-only table — use the query builder |
| `immutable_raw_delete` | error | Raw `DELETE` on an immutable table without `SoftDeletes()` |
| `immutable_timestamps` | error | `t.Immutable()` + `t.Timestamps()` on the same table — timestamps are derived from UUID v7 |
| `integrity_hash_override` | error | Raw SQL setting `row_hash` or `prev_hash` — these are computed by the query builder |
| `encrypted_column_range` | error | Range/comparison scopes (GT, LT, Between) on `.Encrypted()` columns — ciphertext ordering is meaningless |
| `sealed_column_where` | error | Any WHERE clause on a `.Sealed()` column — sealed data cannot be queried |
| `encrypted_column_order_by` | error | ORDER BY on an `.Encrypted()` column — ciphertext sort order is random |
| `encrypted_sealed_conflict` | error | Column marked both `.Encrypted()` and `.Sealed()` — pick one |
| `encrypted_missing_key_config` | error | `.Encrypted()` columns exist but no encryption key is configured |
| `stale_role_annotation` | warning | Migration uses `XxxSees()` for a role removed via policy |
| `unknown_role_annotation` | error | Migration uses `XxxSees()` for a role that has never been defined |
| `role_without_load` | error | `RequireRole()` used but `LoadRoles` not in middleware chain |
| `default_role_missing` | error | Policies exist but no role has `.Default()`, or multiple do |
| `ungated_action` | error | Action exists with no corresponding gate |
| `direct_execute_call` | error | Action method called directly instead of through the gated model method |
| `scope_builder_leak` | error | `ScopeBuilder` referenced outside `database/scopes/` |
| `query_builder_in_scope` | error | `XxxQuery` referenced inside `database/scopes/` |

No pickle ships without being squeezed first.

```yaml
# .github/workflows/squeeze.yml
- name: Squeeze the pickle
  run: pickle squeeze --hard
```

### Built for AI

Pickle isn't just secure — it's the most AI-friendly backend framework you can use. Every convention serves two audiences: the developer who needs to ship, and the AI model that needs to help.

A functioning Pickle app is ~2,000 tokens of source. Controllers are pure business logic — no boilerplate to read past. Request structs are self-documenting API contracts. Migrations are the single source of truth for schema. An AI model doesn't need to parse framework wiring to understand what your endpoint does.

Pickle ships an MCP server that gives AI models queryable access to your project's structure — without dumping source files into context.

```
pickle schema:show transfers    → exact table structure with visibility annotations
pickle routes:list              → every endpoint, middleware, request class
pickle roles:list               → all RBAC roles with permissions
pickle roles:show admin         → single role with column visibility and action grants
pickle graphql:list             → exposed GraphQL models with operations
pickle make:controller          → scaffold via tooling, not by writing boilerplate
```

The model doesn't read your code. It queries your constraints. It discovers what fields exist, what's validated, what middleware protects each route, what relationships are defined — all through structured tool calls. Even lightweight models produce code that respects your schema, validation rules, and security boundaries.

This is why Pickle can do in 5 minutes what takes other frameworks hours. It's not faster typing. It's less context required to make correct decisions.

---

## Getting Started: Unboxing Your First Pickle

See the [Getting Started guide](docs/GettingStarted.md) to create your first Pickle project.

## Documentation

| Topic | Description |
|-------|-------------|
| [Getting Started](docs/GettingStarted.md) | Create your first Pickle project |
| [Controllers](docs/Controller.md) | Handling requests and returning responses |
| [Middleware](docs/Middleware.md) | Auth, rate limiting, and request pipeline |
| [Requests](docs/Requests.md) | Validation and deserialization |
| [Migrations](docs/Migrations.md) | Database schema as code |
| [Views](docs/Views.md) | Database views with computed columns |
| [Router](docs/Router.md) | Route definitions and groups |
| [Context](docs/Context.md) | The request context object |
| [Response](docs/Response.md) | Building HTTP responses |
| [QueryBuilder](docs/QueryBuilder.md) | Typed database queries |
| [Config](docs/Config.md) | Application configuration |
| [Commands](docs/Commands.md) | CLI commands reference |
| [Tickle](docs/Tickle.md) | The preprocessor pipeline |
| [Squeeze](docs/Squeeze.md) | Static security analysis |
| [GraphQL](docs/GraphQL.md) | Auto-generated GraphQL API from migrations |
| [Cron Jobs](docs/CronJobs.md) | Scheduled background tasks |
| [Encryption](docs/Encryption.md) | Encryption at rest and sealed columns |
| [RBAC](docs/RBAC.md) | Role-based access control, column visibility, role-aware queries |
| [Policies](docs/Policies.md) | Role policies and GraphQL exposure policies |
| [Actions](docs/Actions.md) | Gated actions, scopes, and audit trails |
| [Ledger Example](testdata/ledger/README.md) | Immutable tables, append-only tables, DB permissions |

### Immutable Tables & Cryptographic Integrity

Financial records, audit logs, compliance data — anything where history matters. Declare `t.Immutable()` or `t.AppendOnly()` in your migration and Pickle enforces it at every layer.

```go
m.CreateTable("transactions", func(t *Table) {
    t.AppendOnly()
    t.UUID("account_id").NotNull().ForeignKey("accounts", "id")
    t.String("type", 20).NotNull()
    t.Decimal("amount", 18, 2).NotNull()
    t.String("currency", 3).NotNull()
})
```

**What you get:**

| | Mutable | Immutable | Append-Only |
|---|---|---|---|
| DSL | `t.Timestamps()` | `t.Immutable()` | `t.AppendOnly()` |
| `Create()` | INSERT | INSERT | INSERT |
| `Update()` | UPDATE | INSERT new version | Not generated |
| `Delete()` | DELETE | INSERT with `deleted_at`* | Not generated |
| Hash chain | No | Yes | Yes |
| Merkle proofs | No | Yes | Yes |
| DB permissions needed | SELECT, INSERT, UPDATE, DELETE | SELECT, INSERT | SELECT, INSERT |

\* Only with `t.SoftDeletes()`. Without it, `Delete()` is not generated — immutable tables without soft deletes have no deletion concept.

**Developer code is identical to mutable tables:**

```go
// Create — hash chain extended automatically
transfer := &models.Transfer{CustomerID: id, Amount: amount, Status: "pending"}
models.QueryTransfer().Create(transfer)

// Read — always returns the latest version, transparently
transfer, _ := models.QueryTransfer().WhereID(id).First()

// Update — inserts a new version, old version preserved forever
transfer.Status = "completed"
models.QueryTransfer().Update(transfer)

// Full history — opt-in only
versions, _ := models.QueryTransfer().WhereID(id).AllVersions().All()
```

**Cryptographic verification:**

```go
// Verify the full hash chain — O(n), run as a periodic audit
err := models.QueryTransaction().VerifyChain()

// Create a Merkle checkpoint — O(n) within the checkpoint window
cp, _ := models.QueryTransaction().Checkpoint()

// Generate an inclusion proof for an auditor — O(log n)
proof, _ := models.QueryTransaction().Proof(transaction)
ok := models.VerifyProof(proof) // pure function, no DB needed
```

Every row is chained to its predecessor via SHA-256. Merkle tree checkpoints roll the chain into a binary tree for efficient verification. Tampering with any historical row breaks the chain — detectable by `VerifyChain()` and provable via `VerifyProof()`.

Three layers of enforcement: **schema DSL** (no unsafe methods generated), **Go compiler** (can't call what doesn't exist), **database permissions** (SELECT + INSERT only). Any one is sufficient. All three together means you can prove it to an auditor.

### Cron Jobs

Schedule recurring tasks with `pickle make:job`. Jobs run inside your compiled binary — no external cron daemon needed. Define the schedule, write the logic, and Pickle wires it into the app lifecycle. See the [Cron Jobs docs](docs/CronJobs.md) for details.

## The Stack

```
Migrations → Models → Query Builders → Controllers → Routes
Policies   → Roles  → Gates          → Actions     → Audit Trail
     ↑ single source of truth              ↑ pure intent
```

Everything flows from migrations. Everything is queryable via MCP. Everything is verifiable via Squeeze. The generated output is plain Go with zero dependency on Pickle.

## Contributing

Pickle is open to contributions. Here's how to get started:

```bash
git clone https://github.com/shortontech/pickle.git
cd pickle
go run ./pkg/tickle/cmd/                                        # tickle your pickle
go build ./...                                                   # build
go run ./cmd/pickle/ generate --project ./testdata/basic-crud/   # pickle the test app
go run ./cmd/pickle/ squeeze --project ./testdata/basic-crud/    # squeeze it
go test ./...                                                    # test
```

Tickle-generated embeds and testdata output are gitignored. You generate them locally.

**Before submitting a PR:**

1. Run `go run ./pkg/tickle/cmd/` — always, not just if you think you changed something
2. Run `go run ./cmd/pickle/ generate --project ./testdata/basic-crud/`
3. Run `go run ./cmd/pickle/ squeeze --project ./testdata/basic-crud/` — must pass clean
4. Run `go test ./...` — all tests must pass

**Guidelines:**

- Generated files (`*_gen.go`) are never edited by hand. Change the source in `pkg/cooked/`, `pkg/schema/`, or the generator, then regenerate.
- Squeeze rules should have zero false positives. If a rule fires, it should be a real problem. Noisy rules get disabled by users and stop providing value.
- Security is the priority. If a change weakens any security guarantee — even for convenience — it won't be merged.
- Keep the dependency list minimal. Pickle's output has zero dependency on Pickle. New runtime dependencies need strong justification.

**Expressive DX. Go binary. No runtime. 🥒**
