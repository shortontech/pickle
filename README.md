# Pickle 🥒

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