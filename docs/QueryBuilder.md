# QueryBuilder

The generic typed query builder for all models. Pickle generates a model-specific wrapper (e.g. `UserQuery`) with typed `Where*` scope methods, but the underlying CRUD and query building is handled by `QueryBuilder[T]`.

## Generated query types

For each table, Pickle generates:

```go
// models/user_query.go (GENERATED)
type UserQuery struct {
    *QueryBuilder[User]
}

func QueryUser() *UserQuery { ... }

// Type-safe scopes
func (q *UserQuery) WhereID(id uuid.UUID) *UserQuery { ... }
func (q *UserQuery) WhereEmail(email string) *UserQuery { ... }
func (q *UserQuery) WhereEmailLike(pattern string) *UserQuery { ... }
func (q *UserQuery) WithPosts() *UserQuery { ... }
```

## Querying

```go
// Find one record
user, err := models.QueryUser().WhereID(id).First()

// Find all matching
users, err := models.QueryUser().WhereRole("admin").All()

// Count
n, err := models.QueryUser().WhereRole("admin").Count()

// Ordering and pagination
users, err := models.QueryUser().
    OrderBy("created_at", "DESC").
    Limit(20).
    Offset(40).
    All()

// Eager load relationships
user, err := models.QueryUser().
    WhereEmail(email).
    WithPosts().
    First()
```

## CRUD

```go
// Create — inserts and scans back DB-generated values (UUID, timestamps)
user := &models.User{Name: "Alice", Email: "alice@example.com"}
err := models.QueryUser().Create(user)
// user.ID and user.CreatedAt are now populated

// Update — updates all fields, uses ID for WHERE by default
user.Name = "Bob"
err := models.QueryUser().Update(user)

// Update with explicit conditions
err := models.QueryUser().WhereID(id).Update(user)

// Delete
err := models.QueryUser().WhereID(id).Delete(&models.User{})
```

## Generic methods (from QueryBuilder[T])

These are inherited by all model query types:

| Method | Returns | Description |
|--------|---------|-------------|
| `Where(column, value)` | `*QueryBuilder[T]` | Add `column = value` condition |
| `WhereOp(column, op, value)` | `*QueryBuilder[T]` | Add `column op value` condition |
| `WhereIn(column, values)` | `*QueryBuilder[T]` | Add `column IN (...)` condition |
| `WhereNotIn(column, values)` | `*QueryBuilder[T]` | Add `column NOT IN (...)` condition |
| `OrderBy(column, direction)` | `*QueryBuilder[T]` | Add ORDER BY clause |
| `Limit(n)` | `*QueryBuilder[T]` | Set LIMIT |
| `Offset(n)` | `*QueryBuilder[T]` | Set OFFSET |
| `EagerLoad(relation)` | `*QueryBuilder[T]` | Mark relationship for eager loading |
| `First()` | `(*T, error)` | Return first matching record |
| `All()` | `([]T, error)` | Return all matching records |
| `Count()` | `(int64, error)` | Count matching records |
| `Create(record)` | `error` | INSERT with RETURNING (populates DB defaults) |
| `Update(record)` | `error` | UPDATE by conditions or by ID |
| `Delete(record)` | `error` | DELETE matching records |

## Generated scope methods

For each column, Pickle generates type-safe scopes:

**All types:**
- `Where{Column}(val)` — exact match
- `Where{Column}Not(val)` — not equal
- `Where{Column}In(vals)` — IN list
- `Where{Column}NotIn(vals)` — NOT IN list

**String columns:**
- `Where{Column}Like(pattern)` — SQL LIKE
- `Where{Column}NotLike(pattern)` — SQL NOT LIKE

**Numeric columns (Integer, BigInteger, Decimal):**
- `Where{Column}GT(val)`, `GTE`, `LT`, `LTE` — comparisons

**Timestamp columns:**
- `Where{Column}Before(time)`, `After(time)`, `Between(start, end)`

**Foreign key columns:**
- `With{Relation}()` — eager load the related model

## Database connection

The query builder uses the package-level `models.DB` variable (a `*sql.DB`). This is set during app initialization by the generated commands package. All queries use parameterized `$1, $2, ...` placeholders — no string interpolation, no SQL injection.
