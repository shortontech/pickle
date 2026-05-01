# GraphQL

Pickle generates a GraphQL API from your migrations. No controllers, no request structs, no routes file. Write a migration, run `pickle generate`, get queries, mutations, pagination, auth, dataloaders, and input validation generated from the same schema source of truth as the rest of the app.

## What Gets Generated

```
app/graphql/
  pickle_gen.go           ← executor, auth context, dataloaders, error types
  schema_gen.go           ← SDL string constant, parsed at init
  resolver_gen.go         ← query dispatch, field resolvers, filter/sort
  crud_resolver_gen.go    ← create/update/delete mutations with ownership scoping
  dataloader_gen.go       ← batched relationship loaders
  handler_gen.go          ← net/http handler
```

All generated files end in `_gen.go` and get overwritten on every run.

## Schema Generation

Every table becomes a GraphQL type. Every column becomes a field. Relationships become nested fields.

### Type Mapping

| Pickle Column | GraphQL Type |
|--------------|-------------|
| `UUID` | `ID` |
| `String` | `String` |
| `Text` | `String` |
| `Integer` | `Int` |
| `BigInteger` | `Int` |
| `Decimal` | `String` (precision-safe) |
| `Boolean` | `Boolean` |
| `Timestamp` | `DateTime` (custom scalar) |
| `JSONB` | `JSON` (custom scalar) |
| `Binary` | excluded |

### Nullability

- `.NotNull()` → `String!`
- `.Nullable()` → `String`
- Primary keys → always `!`

### Relationships

Foreign keys and `HasMany`/`HasOne` become object and list fields:

```graphql
type User {
  id: ID!
  name: String!
  posts: [Post!]!        # HasMany
  profile: Profile       # HasOne
}

type Post {
  id: ID!
  title: String!
  user: User!            # BelongsTo (from FK)
  comments: [Comment!]!  # HasMany
}
```

### Queries and Mutations

For every table, Pickle generates five operations:

```graphql
type Query {
  users(filter: UserFilter, sort: UserSort, page: PageInput): UserConnection!
  user(id: ID!): User
}

type Mutation {
  createUser(input: CreateUserInput!): User! @auth
  updateUser(id: ID!, input: UpdateUserInput!): User! @auth
  deleteUser(id: ID!): Boolean! @auth
}
```

Pagination is Relay-style with `edges`, `node`, `cursor`, and `pageInfo`.

### Filter and Sort Types

Generated from columns:

```graphql
input UserFilter {
  id: IDFilter
  name: StringFilter
  email: StringFilter
  createdAt: DateTimeFilter
}

input StringFilter {
  eq: String
  like: String
  in: [String!]
}

enum UserSort {
  NAME_ASC
  NAME_DESC
  CREATED_AT_ASC
  CREATED_AT_DESC
}
```

## Auth Directives

Visibility annotations on columns map to GraphQL directives:

| Column Annotation | Directive | Behavior |
|------------------|-----------|----------|
| `.Public()` | `@public` | Visible to everyone, no auth required |
| (default) | `@auth` | Requires authentication |
| `.IsOwner()` / owner-sees | `@ownerOnly` | Only the resource owner (or admin) can see |

Primary keys are always `@public`.

```graphql
type User {
  id: ID! @public
  name: String! @public
  email: String! @ownerOnly
  createdAt: DateTime! @auth
}
```

At resolve time, field resolvers check the directive:

- `@public` fields resolve for everyone.
- `@auth` fields return `nil` for unauthenticated requests.
- `@ownerOnly` fields return `nil` unless `ctx.CanSeeOwnerFields(ownerID)` is true (caller is the owner or an admin).

The `ResolveContext` determines visibility tier automatically:

```go
// Unauthenticated → VisibilityPublic  (only @public fields)
// Authenticated   → VisibilityOwner   (@public + @auth + @ownerOnly for own records)
// Admin           → VisibilityAll     (everything)
```

## Input Types

Input types are derived from column definitions. No request structs needed.

**Create input:** all `NOT NULL` columns without defaults are required (`!`). Everything else is optional.

**Update input:** all fields are optional (partial update).

**Excluded from input:** primary keys, timestamps (`created_at`, `updated_at`, `deleted_at`), `password_hash`, `row_hash`, `prev_hash`, `version_id`, binary columns.

```go
// Migration
t.String("title").NotNull()           // → title: String!  (required on create)
t.Text("body").NotNull()              // → body: String!
t.String("status").Default("draft")   // → status: String  (optional, has default)
t.Timestamps()                        // → excluded
```

Produces:

```graphql
input CreatePostInput {
  title: String!
  body: String!
  status: String
}

input UpdatePostInput {
  title: String
  body: String
  status: String
}
```

### Constraint Validation

Column constraints become validation rules at mutation time:

| Constraint | Validation |
|-----------|-----------|
| `.NotNull()` | Required on create |
| `String(name, 255)` | Max length 255 |
| `UUID` type | UUID format validation |
| `.ForeignKey(table, col)` | UUID format + existence check |

## Ownership Scoping

Tables with an `.IsOwner()` column get automatic ownership enforcement on mutations.

**Create:** owner column is set from `ctx.Auth().UserID`. The caller cannot set it via input.

**Update/Delete:** query is scoped by owner — `WhereOwnedBy(auth)` ensures users can only modify their own records.

**Read:** field-level visibility directives control what each user sees.

```go
// Generated create mutation (simplified)
func (r *RootResolver) crudCreatePost(ctx *ResolveContext, field Field) (any, error) {
    if !ctx.IsAuthenticated() {
        return nil, Unauthenticated("createPost: authentication required")
    }
    // ...
    record.UserID = ownerID  // auto-set from auth, not from input
    // ...
}

// Generated update mutation (simplified)
func (r *RootResolver) crudUpdatePost(ctx *ResolveContext, field Field) (any, error) {
    q := models.QueryPost().WhereID(id)
    q.WhereUserId(ownerID)  // ownership scope — can only update own posts
    record, err := q.First()
    // ...
}
```

Tables without `.IsOwner()` require `@auth` but have no ownership scoping — admin-style resources.

## Nested Resources

For `HasMany`/`HasOne` relationships, child mutations are scoped to the parent. The parent ID is an argument, not part of the input. The FK is set automatically.

```graphql
type Mutation {
  # Standard create
  createPost(input: CreatePostInput!): Post! @auth

  # Nested create — parent-scoped
  createNestedComment(postId: ID!, input: CreateCommentInput!): Comment! @auth
}
```

The generated resolver verifies the parent exists, sets the FK, and applies ownership if applicable:

```go
func (r *RootResolver) crudCreateNestedComment(ctx *ResolveContext, field Field) (any, error) {
    // 1. Auth check
    // 2. Parse and validate parent ID
    // 3. Verify parent exists: models.QueryPost().WhereID(parentID).First()
    // 4. Set FK: record.PostID = parentID
    // 5. Set owner from auth (if IsOwner column exists)
    // 6. Validate input constraints
    // 7. Create record
}
```

## Dataloaders

Relationship fields use batched dataloaders to prevent N+1 queries. Pickle generates a `batchLoader` per relationship that collects keys across a request and issues a single query.

```go
// Generated: loads all posts for a batch of user IDs in one query
loader := newBatchLoader(func(keys []uuid.UUID) []batchResult[[]models.Post] {
    // SELECT * FROM posts WHERE user_id IN ($1, $2, ...) ORDER BY user_id
})
```

Dataloaders are created per-request (stored on `ResolveContext`). No cross-request caching, no stale data.

When a resolver hits a relationship field like `user.posts`, it calls `loader.load(userID)` instead of issuing an immediate query. The loader batches all calls within the same tick and dispatches once.

## Override Pattern

Same as all Pickle generation:

- `app/graphql/resolver_gen.go` — generated, overwritten every run
- `app/graphql/resolver.go` — user-written, never touched by generator

**If `resolver.go` exists, `resolver_gen.go` is not written.** The user's version takes precedence.

For per-resource overrides, the same pattern applies at the resource level:

- `app/graphql/resolvers/user_resolvers_gen.go` — generated CRUD
- `app/graphql/resolvers/user_resolvers.go` — user-written, skips generation

Use overrides when business logic diverges from CRUD — send an email on user creation, charge a payment on order creation, enforce complex authorization rules. Everything else stays generated.

## Server Wiring

Mount the GraphQL handler in your `main.go`:

```go
package main

import (
    "net/http"
    "myapp/app/graphql"
)

func main() {
    mux := http.NewServeMux()

    // GraphQL endpoint
    mux.Handle("/graphql", graphql.Handler())

    // Optional: GraphQL Playground (development only)
    mux.Handle("/playground", graphql.PlaygroundHandler("/graphql"))

    // Disable introspection in production
    // graphql.SetIntrospection(false)

    http.ListenAndServe(":8080", mux)
}
```

The handler:

1. Reads the JSON request body (`query`, `variables`, `operationName`)
2. Parses the query against the embedded SDL using `gqlparser`
3. Checks query depth (default max: 10)
4. Extracts auth from the request (override `extractAuth` for your auth strategy)
5. Dispatches to generated resolvers
6. Returns a standard GraphQL JSON response (`data` + `errors`)

All errors follow GraphQL conventions with structured `extensions`:

```json
{
  "errors": [{
    "message": "createPost: authentication required",
    "path": ["createPost"],
    "extensions": { "code": "UNAUTHENTICATED" }
  }]
}
```

Error codes: `BAD_USER_INPUT`, `UNAUTHENTICATED`, `FORBIDDEN`, `NOT_FOUND`, `INTERNAL_SERVER_ERROR`.

## Full Example

This migration:

```go
m.CreateTable("users", func(t *Table) {
    t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
    t.String("name").NotNull().Public()
    t.String("email").NotNull().Unique().Encrypted()
    t.String("password_hash").NotNull().Encrypted()
    t.Timestamps()

    t.HasMany("posts", func(t *Table) {
        t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
        t.String("title").NotNull().Public()
        t.Text("body").NotNull().Public()
        t.String("status").NotNull().Default("draft")
        t.Timestamps()
    }).Collection()
})
```

Produces generated GraphQL support with:

- `users` / `user(id)` queries with Relay pagination
- `posts` / `post(id)` queries with filtering and sorting
- `createUser`, `updateUser`, `deleteUser` mutations with auth
- `createPost`, `updatePost`, `deletePost` mutations with ownership scoping
- `createNestedPost(userId, input)` parent-scoped mutation
- Dataloaders for `user.posts` (no N+1)
- Field-level auth (`name` is public, `email` is owner-only, `createdAt` requires auth)
- Input validation from column constraints

One migration file. `go build`. Static binary.

## GraphQL Exposure Policies

By default, no models are exposed via GraphQL. Exposure is opt-in: if your project has no `database/policies/` directory, no GraphQL schema is generated.

### Enabling exposure

Create policy files in `database/policies/` to control which models and fields are exposed:

```go
// database/policies/user_policy.go
package policies

type UserPolicy struct {
    Policy
}

func (p *UserPolicy) Configure() {
    // Expose the User model with all public fields
    p.Expose("users")

    // Modify exposure — hide specific fields or add constraints
    p.AlterExpose("users", func(e *Exposure) {
        e.Hide("internal_notes")
        e.ReadOnly("email")
    })
}
```

### Policy methods

| Method | Description |
|--------|-------------|
| `Expose(table)` | Expose a table as a GraphQL type with standard CRUD |
| `AlterExpose(table, fn)` | Modify an existing exposure — hide fields, set read-only, add constraints |
| `Unexpose(table)` | Remove a previously exposed table from the GraphQL schema |
| `ControllerAction(action)` | Wrap an existing controller action as a GraphQL mutation or query |

### ControllerAction adapter

Reuse existing REST controller logic in GraphQL without duplication:

```go
func (p *TransferPolicy) Configure() {
    p.Expose("transfers")
    p.ControllerAction(controllers.TransferController{}.Approve)
}
```

The adapter handles argument mapping, auth context forwarding, and response serialization.

### Incremental exposure

Add tables to your GraphQL API one at a time. Each `Expose()` call adds that table; everything else stays hidden:

```go
// Sprint 1 — expose users only
p.Expose("users")

// Sprint 2 — add posts
p.Expose("posts")

// Sprint 3 — remove a field that shouldn't have been exposed
p.AlterExpose("users", func(e *Exposure) {
    e.Hide("phone_number")
})
```

### Changelog tracking

Pickle tracks GraphQL schema changes in a `graphql_changelog` state file (generated, lives alongside other `_gen.go` files). Each generation run diffs the current schema against the previous one and records additions, removals, and type changes. This makes it easy to review schema evolution in pull requests and catch accidental exposure changes.
