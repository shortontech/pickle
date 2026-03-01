# Pickle 🥒

**Minimal context. Maximum security. Ship a Go binary.**

Pickle is a code generation framework for Go that makes secure applications trivial to build — for humans and AI alike. You write controllers, migrations, request classes, and middleware using a Laravel-inspired syntax. Pickle generates plain, idiomatic Go. The output compiles to a single static binary with no runtime dependency on Pickle.

Using Anthropic's cheapest model (Haiku) and Pickle's MCP server, I built a working app with zero vulnerabilities in under 5 minutes. Not a demo. Not a toy. Production-grade code with parameterized queries, request validation, ownership scoping, and middleware enforcement — all generated from a handful of intent files.

That's not a speed trick. That's context management.

### Why Pickle Exists

Code reviews are fifteen files deep, full of hard-to-detect security vulnerabilities and duplicated features. Go makes this worse, not better. Go libraries are built to be flexible rather than opinionated — which means there's no way to do a sniff test. No conventions to check against. No structure that tells you something is wrong just by looking at it.

Pickle fixes this by reducing the code you actually write — and review — to pure intent. A functioning app is ~2,000 tokens of source. Even with a large feature set, a Pickle project has far less code than the same thing built with existing Go libraries. And because Pickle is opinionated, every file follows the same conventions. You can sniff-test a PR in seconds.

```
You write controllers.     Pickle generates models.
You write migrations.      Pickle generates query builders.
You write request classes.  Pickle generates validation + deserialization.
You write routes.go.       Pickle wires it all together.
```

The generated code is readable, debuggable, and `grep`-friendly. It's not magic. It's just code you didn't have to type.

### Context Management, Not Just Code Generation

Pickle isn't opinionated for the sake of it. Every convention serves two audiences: the developer who needs to ship, and the AI model that needs to help.

**Migrations are the single source of truth.** You write the schema once. Pickle derives models, query builders, typed scopes, and struct tags from it. There's exactly one place to look for what a table contains.

**Controllers are pure intent.** No boilerplate to read past. A 20-line controller is 20 lines of business logic. An AI model doesn't need to parse framework wiring to understand what your endpoint does.

**Routes are one file.** Open `routes/web.go` and see every endpoint, its middleware stack, and its grouping. One file, entire API surface. For a human, that's a 30-second security review. For a model, that's a few hundred tokens.

**Request classes are self-documenting contracts.** Struct fields + validation tags = the complete API contract. No documentation drift. No guessing what a request accepts.

### MCP Server: AI Discovers Your Constraints

Pickle ships an MCP server that gives AI models queryable access to your project's structure — without dumping source files into context.

```
pickle schema:show transfers    → exact table structure, no reading migrations
pickle routes:list              → every endpoint, middleware, request class
pickle requests:list            → all validation rules at a glance
pickle make:controller          → scaffold via tooling, not by writing boilerplate
```

The model doesn't read your code. It queries your constraints. It discovers what fields exist, what's validated, what middleware protects each route, what relationships are defined — all through structured tool calls. The result: even lightweight models produce code that respects your schema, validation rules, and security boundaries.

This is why Pickle can do in 5 minutes what takes other frameworks hours. It's not faster typing. It's less context required to make correct decisions.

### Security by Construction: Wrap It Before They Hack It

Most frameworks treat security as a best practice. Pickle treats it as a structural property.

A properly wrapped pickle:

```
Request → RateLimit → CORS → Auth → RBAC → Validation → Controller
```

An unwrapped pickle (DO NOT DO THIS):

```
Request → Controller
```

> Never deploy an unwrapped pickle. An unwrapped pickle exposed to the open internet is a liability.

**SQL injection is impossible.** The generated query builder uses parameterized queries exclusively. There is no API for string interpolation. The unsafe path doesn't exist.

**Mass assignment is impossible.** Request structs define exactly which fields are accepted. If `CreateUserRequest` doesn't have a `Role` field, POSTing `{"role": "admin"}` does nothing. The model never sees unvalidated input.

**Validation cannot be bypassed.** Controllers receive pre-validated, typed request structs. The generated binding layer runs validation before your code executes. There is no code path around it.

**Authorization gaps are visible.** Every endpoint and its middleware stack are in `routes.go`. A missing `Auth` or `RequireRole` is immediately obvious — to you and to any AI reviewing your code.

**Standard security tooling works out of the box.** Generated code is plain Go — `go vet`, `gosec`, `staticcheck`, Snyk, and Semgrep work with zero configuration. No framework abstractions to unwrap. Security scanners see exactly what runs in production.

This is the advantage of code generation over runtime frameworks. A scanner can't reason about magic method resolution. It can reason about a struct, a function, and a parameterized query — because that's just Go.

### Squeeze: Make Sure Nothing's Oozing

`pickle squeeze` validates your entire project in one command — schema integrity, model correctness, route wiring, request validation, and middleware enforcement.

```bash
pickle squeeze              # Run full validation
pickle squeeze --hard       # Strict mode: warnings become failures
```

```
🥒 Squeezing your pickle...
   Schemas:    ✅ 12 migrations (forward + rollback)
   Models:     ✅ 8 models in sync
   Routes:     ✅ 23 endpoints wired
   Requests:   ✅ 14 request classes validated
   Middleware:  ✅ 6 middleware chains verified
🥒 Your pickle is crunchy.
```

If a migration adds a column the model doesn't reflect — your pickle is oozing. If a route references a missing controller method — oozing. If a protected endpoint is missing auth middleware — definitely oozing. If a request struct allows an undeclared field — something is very wrong with your pickle.

No pickle ships without being squeezed first. That's just good hygiene.

```yaml
# .github/workflows/squeeze.yml
- name: Squeeze the pickle
  run: pickle squeeze --hard
```

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

## The Stack

```
Migrations → Models → Query Builders → Controllers → Routes
     ↑ single source of truth              ↑ pure intent
```

Everything flows from migrations. Everything is queryable via MCP. Everything is verifiable via squeeze. The generated output is plain Go with zero dependency on Pickle.

## Contributing

I don't want anything the user apps do to expose their pickle. That's public indecency.

**Laravel DX. Go binary. No runtime. 🥒**
