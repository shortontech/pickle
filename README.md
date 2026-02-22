# Pickle 🥒

**Laravel's developer experience. Go's deployment story. Security by construction.**

Pickle is a code generation framework for Go. You write controllers, migrations, request classes, and middleware using a Laravel-inspired syntax. Pickle watches your project and generates all the boilerplate — models, route bindings, query scopes, handler wiring, request deserialization — as plain, idiomatic Go. The output compiles to a single static binary with no runtime dependency on Pickle.

```
You write controllers.     Pickle generates models.
You write migrations.      Pickle generates query builders.
You write request classes.  Pickle generates validation + deserialization.
You write routes.go.       Pickle generates handler registration.
```

The generated code is readable, debuggable, and `grep` friendly. It's not magical. It's just code you didn't have to type.

### Why Pickle Exists

Go makes you write 200 lines to do what Laravel does in 3. The GoLang community thinks this is a feature. I think it's a security nightmare not. It's boilerplate, and boilerplate is where bugs and exploits hide.

Pickle takes the position that if code can be generated from your intent, it should be. You keep the control. You lose the tedium.

### Security by Design, Not by Discipline

Most frameworks treat security as a best practice.

Pickle treats it as a structural property.

**SQL injection is impossible.** The generated query builder uses parameterized queries exclusively. There is no API for string interpolation. No developer discipline required — the unsafe path doesn't exist.

**Mass assignment is impossible.** Request structs define exactly which fields are accepted. If `CreateUserRequest` doesn't have a `Role` field, POSTing `{"role": "admin"}` does nothing. The model never sees unvalidated input.

**Validation cannot be bypassed.** Controllers receive pre-validated, typed request structs. The generated binding layer runs validation before your code executes. There is no code path around it.

**Authorization gaps are visible.** Every endpoint, its middleware stack, and its grouping are defined in a single `routes.go` file. A missing `Auth` or `RequireRole` middleware is immediately obvious. Security review is a 30-second read, not a codebase-wide audit.

**Standard security tooling works out of the box.** Generated code is plain Go — `go vet`, `gosec`, `staticcheck`, Snyk, and Semgrep work with zero configuration. No framework abstractions to unwrap, no `interface{}` soup, no runtime reflection. Security scanners see exactly what runs in production.

This is the main advantage of code generation over using runtime frameworks. A scanner can't reason about magic method resolution or custom middleware abstractions. It can reason about a struct, a function, and a parameterized query — because that's just Go.

---

## Getting Started: Unboxing Your First Pickle

## Middleware Stack

**Authentication:** "Wrap It Before They Hack It"

### Wrap it before they hack it.

Pickle middleware forms a protective layer around your 
application. Each middleware wraps the next, creating 
a thick, secure barrier between the internet and your 
business logic.

## A properly wrapped Pickle

Request → RateLimit → CORS → Auth → RBAC → Validation → Controller

An unwrapped pickle (DO NOT DO THIS):

Request → Controller

> ⚠️ **WARNING:** Never deploy an unwrapped pickle. An unwrapped 
> Pickle exposed to the open internet is a liability.

## Database (What's Inside Your Pickle):


## Deployment (Putting Your Pickle In Production):


## Scaling (Your Pickle Grows As You Scale):


## Monitoring (Keeping An Eye On Your Pickle):


## Testing (Poking Your Pickle):

It's not complicated.

Compile → Tickle → Compile → Squeeze → Pickle.

* **Compile the tickler** — a generator for generating generators used within Pickle, so that pickle's generators are plain idiomatic .go files.
* **Tickle your pickle** — this processes the idiomatic Go to store as templates in .go files.
* **Then you compile your pickle.**
* **Squeeze your pickle** — run the test suite. Make sure nothing's oozing.
* **Whip out your pickle** — pickle one of our test apps.

It's so easy. Anyone can play with their pickle.

### Squeeze: Automated Testing 🥒

`pickle squeeze` is Pickle's built-in test runner. It validates your entire project — generated code, migrations, route wiring, request validation — in one command.

```bash
pickle squeeze              # Run full test suite
pickle squeeze --hard       # Strict mode: warnings become failures
pickle squeeze --dry        # Dry run: show what would be tested without executing
pickle squeeze --only=routes # Target a specific layer
```

#### What Gets Squeezed

**Schema integrity** — Every migration is parsed forward and backward. If your `Up()` creates something your `Down()` doesn't clean up, the squeezer catches it. Migrations are tested in sequence *and* in reverse to verify rollback safety.

**Model correctness** — Generated models are diffed against their source migrations. If a migration adds a column and the model doesn't reflect it, your pickle is oozing. If struct tags don't match column types, your pickle is oozing.

**Route completeness** — Every controller method referenced in `routes.go` must exist. Every request class referenced in a controller must exist. Every middleware referenced in a route group must exist. Dead references are oozing. Unreachable handlers are oozing.

**Request validation** — Squeeze generates mock requests for each endpoint: valid payloads that should pass, malformed payloads that should fail, and boundary payloads that test edge cases. If a request with `{"role": "admin"}` gets through a struct that doesn't define `Role`, something is very wrong with your pickle.

**Middleware chain verification** — Protected routes are tested without auth tokens to verify they actually reject. RBAC routes are tested with wrong roles. Rate limit middleware is tested with burst traffic. If any middleware is a no-op, the squeezer finds it.

#### Squeeze Output

A healthy pickle:
```
🥒 Squeezing your pickle...
   Schemas:    ✅ 12 migrations (forward + rollback)
   Models:     ✅ 8 models in sync
   Routes:     ✅ 23 endpoints wired
   Requests:   ✅ 14 request classes validated
   Middleware:  ✅ 6 middleware chains verified
🥒 Your pickle is crunchy.
```

A problematic pickle:
```
🥒 Squeezing your pickle...
   Schemas:    ✅ 12 migrations
   Models:     ❌ Transfer model missing 'currency' field
   Routes:     ⚠️  UserController.Destroy referenced but not implemented
   Requests:   ❌ CreateTransferRequest allows undeclared field 'processor_id'
   Middleware:  ❌ POST /api/transfers missing Auth middleware
⚠️  Your pickle is oozing. Check squeeze logs.
```

#### CI/CD

Always squeeze before you ship.

```yaml
# .github/workflows/squeeze.yml
- name: Squeeze the pickle
  run: pickle squeeze --hard
```

No pickle gets deployed without being squeezed first. That's just good hygiene.


## Contributing (Pickle Enhancement):