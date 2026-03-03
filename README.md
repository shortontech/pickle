# Pickle 🥒

**The world's most secure web framework.**

Pickle is a code generation framework for Go that makes secure applications trivial to build — for humans and AI alike. You write controllers, migrations, request classes, and middleware. Pickle generates plain, idiomatic Go. The output compiles to a single static binary with no runtime dependency on Pickle.

```
You write controllers.     Pickle generates models.
You write migrations.      Pickle generates query builders.
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

**Visible by convention:**

- Every endpoint, its middleware stack, and its grouping are in one file: `routes/web.go`. A missing `Auth` or `RequireRole` is immediately obvious — to you and to any AI reviewing your code. One file, entire API surface, 30-second security review.

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
pickle schema:show transfers    → exact table structure, no reading migrations
pickle routes:list              → every endpoint, middleware, request class
pickle requests:list            → all validation rules at a glance
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

## The Stack

```
Migrations → Models → Query Builders → Controllers → Routes
     ↑ single source of truth              ↑ pure intent
```

Everything flows from migrations. Everything is queryable via MCP. Everything is verifiable via Squeeze. The generated output is plain Go with zero dependency on Pickle.

## Contributing

I don't want anything the user apps do to expose their pickle. That's public indecency.

**Laravel DX. Go binary. No runtime. 🥒**
