# Migrations

The single source of truth for your database schema. You write migrations using the schema DSL; Pickle generates model structs and query scopes from them.

## Writing a migration

```go
// database/migrations/2026_02_21_143052_create_users_table.go
package migrations

type CreateUsersTable_2026_02_21_143052 struct {
    Migration
}

func (m *CreateUsersTable_2026_02_21_143052) Up() {
    m.CreateTable("users", func(t *Table) {
        t.UUID("id").PrimaryKey().Default("uuid_generate_v7()")
        t.String("name").NotNull()
        t.String("email").NotNull().Unique()
        t.String("password").NotNull()
        t.Text("bio").Nullable()
        t.Timestamps()
    })

    m.AddIndex("users", "email")
}

func (m *CreateUsersTable_2026_02_21_143052) Down() {
    m.DropTableIfExists("users")
}
```

## Naming convention

Files use `{timestamp}_{description}.go`. The timestamp prefix determines execution order. The struct name matches: `{PascalDescription}_{timestamp}`.

Use `pickle make:migration create_posts_table` to scaffold one (generates the timestamp automatically).

## Column types

| DSL method | SQL type | Go type |
|-----------|----------|---------|
| `t.UUID(name)` | UUID | `uuid.UUID` |
| `t.String(name)` | VARCHAR(255) | `string` |
| `t.String(name, 100)` | VARCHAR(100) | `string` |
| `t.Text(name)` | TEXT | `string` |
| `t.Integer(name)` | INTEGER | `int` |
| `t.BigInteger(name)` | BIGINT | `int64` |
| `t.Decimal(name, 18, 2)` | DECIMAL(18,2) | `decimal.Decimal` |
| `t.Boolean(name)` | BOOLEAN | `bool` |
| `t.Timestamp(name)` | TIMESTAMP | `time.Time` |
| `t.JSONB(name)` | JSONB | `json.RawMessage` |
| `t.Date(name)` | DATE | `time.Time` |
| `t.Time(name)` | TIME | `time.Time` |
| `t.Binary(name)` | BYTEA | `[]byte` |
| `t.Timestamps()` | — | Adds `created_at` + `updated_at` with NOW() defaults |

## Column modifiers

Chain these on any column:

```go
t.String("email").NotNull().Unique()
t.UUID("id").PrimaryKey().Default("uuid_generate_v7()")
t.UUID("team_id").NotNull().ForeignKey("teams", "id")
t.Text("notes").Nullable()
```

| Modifier | Description |
|----------|-------------|
| `.PrimaryKey()` | Mark as primary key |
| `.NotNull()` | NOT NULL constraint |
| `.Nullable()` | Allow NULL (default for most columns) |
| `.Unique()` | UNIQUE constraint |
| `.Default(value)` | Set default value |
| `.ForeignKey(table, column)` | Add foreign key reference |

## Migration operations

Available in `Up()` and `Down()`:

| Method | Description |
|--------|-------------|
| `m.CreateTable(name, fn)` | Create a table |
| `m.DropTableIfExists(name)` | Drop a table |
| `m.AddColumn(table, name, fn)` | Add a column to an existing table |
| `m.DropColumn(table, name)` | Drop a column |
| `m.RenameColumn(table, old, new)` | Rename a column |
| `m.AddIndex(table, columns...)` | Add an index |
| `m.AddUniqueIndex(table, columns...)` | Add a unique index |
| `m.RenameTable(old, new)` | Rename a table |

## Running migrations

```bash
pickle migrate           # Run pending migrations
pickle migrate:rollback  # Rollback last batch
pickle migrate:fresh     # Drop all tables and re-run
pickle migrate:status    # Show migration status
```

## Transactional migrations

Migrations run inside a transaction by default. Override for operations that can't be transactional:

```go
func (m *AddSearchIndex_2026_03_01_120000) Transactional() bool { return false }
```

## Model generation

From the migration above, Pickle generates:

```go
// models/user.go (GENERATED — DO NOT EDIT)
type User struct {
    ID        uuid.UUID `json:"id" db:"id"`
    Name      string    `json:"name" db:"name"`
    Email     string    `json:"email" db:"email"`
    Password  string    `json:"password" db:"password"`
    Bio       *string   `json:"bio,omitempty" db:"bio"`
    CreatedAt time.Time `json:"created_at" db:"created_at"`
    UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}
```

Nullable columns become pointer types (`*string`, `*time.Time`). The `json` and `db` struct tags are generated automatically.
