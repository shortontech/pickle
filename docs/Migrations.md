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
| `.Public()` | Mark as visible to anyone (ownership system) |
| `.OwnerSees()` | Mark as visible only to the row's owner |
| `.IsOwner()` | Mark as the ownership column for the table |
| `.Encrypted()` | Mark as requiring encryption at rest |
| `.UnsafePublic()` | Acknowledge that a sensitive field is intentionally `.Public()` |

## Ownership & visibility

Declare field visibility tiers and row ownership directly in your migration. Pickle generates `PublicResponse` and `OwnerResponse` structs, a `Serialize` function, and a `WhereOwnedBy` query scope.

```go
m.CreateTable("posts", func(t *Table) {
    t.UUID("id").PrimaryKey().Default("uuid_generate_v7()").Public()
    t.UUID("user_id").NotNull().ForeignKey("users", "id").IsOwner()
    t.String("title").NotNull().Public()
    t.Text("body").NotNull().OwnerSees()
    t.String("draft_notes").Nullable().OwnerSees()
    t.Timestamps()
})
```

| Modifier | Effect |
|----------|--------|
| `.Public()` | Field appears in both `PostPublicResponse` and `PostOwnerResponse` |
| `.OwnerSees()` | Field appears only in `PostOwnerResponse` |
| `.IsOwner()` | Column used to determine ownership — generates `WhereOwnedBy` scope |

Pickle generates:

- `PostPublicResponse` struct — only `.Public()` fields
- `PostOwnerResponse` struct — `.Public()` + `.OwnerSees()` fields
- `SerializePost(record, ownerID)` — returns the appropriate response based on ownership match
- `SerializePosts(records, ownerID)` — slice variant
- `WhereOwnedBy(ownerID)` — query scope filtering by the owner column

Only one column per table may be marked `.IsOwner()`. Tables without any ownership modifiers generate no response structs.

## Encryption at rest

Mark sensitive columns with `.Encrypted()` to declare that they require encryption at rest. Squeeze flags sensitive field names that are missing this annotation.

```go
m.CreateTable("accounts", func(t *Table) {
    t.UUID("id").PrimaryKey().Default("uuid_generate_v7()")
    t.String("api_key", 255).NotNull().Encrypted()
    t.String("email", 255).NotNull().Encrypted()
    t.String("password_hash", 255).NotNull().Encrypted()
    t.Timestamps()
})
```

Pickle knows which field names are sensitive (e.g., `email`, `password`, `api_key`, `*_token`, `*_secret`, `*_hash`). If you mark a sensitive field as `.Public()`, Squeeze flags it as an error. Use `.UnsafePublic()` to explicitly opt out:

```go
// Intentionally public — e.g., a user directory
t.String("email", 255).NotNull().Public().UnsafePublic().Encrypted()
```

See the [Squeeze docs](Squeeze.md) for the full list of sensitive field patterns.

## Immutable tables

Declare `t.Immutable()` for tables where history must never be lost. Every "update" inserts a new version row — the original is preserved forever.

```go
m.CreateTable("transfers", func(t *Table) {
    t.Immutable()

    t.UUID("customer_id").NotNull().ForeignKey("customers", "id").IsOwner()
    t.String("status").NotNull().Default("pending")
    t.Decimal("amount", 18, 2).NotNull()
    t.String("currency", 3).NotNull()
    t.JSONB("metadata").Nullable()

    t.SoftDeletes() // optional — "delete" inserts a version with deleted_at set
})
```

`t.Immutable()` automatically injects:

- `id UUID NOT NULL` — stable identity, same across all versions
- `version_id UUID NOT NULL` — unique per version, UUID v7 (monotonically increasing)
- `row_hash BYTEA NOT NULL` — SHA-256 hash chain linking each row to its predecessor
- `prev_hash BYTEA NOT NULL` — the previous row's `row_hash`
- Composite primary key `(id, version_id)`

**Do not call `t.Timestamps()` on an immutable table.** `CreatedAt()` and `UpdatedAt()` are derived from the UUID v7 timestamps embedded in `id` and `version_id` — they're Go methods, not database columns.

Pickle generates:

```go
type Transfer struct {
    ID         uuid.UUID       `json:"id" db:"id"`
    VersionID  uuid.UUID       `json:"version_id" db:"version_id"`
    RowHash    []byte          `json:"-" db:"row_hash"`
    PrevHash   []byte          `json:"-" db:"prev_hash"`
    CustomerID uuid.UUID       `json:"customer_id" db:"customer_id"`
    Status     string          `json:"status" db:"status"`
    Amount     decimal.Decimal `json:"amount" db:"amount"`
    Currency   string          `json:"currency" db:"currency"`
    // ...
}

func (m *Transfer) CreatedAt() time.Time { return uuidV7Time(m.ID) }
func (m *Transfer) UpdatedAt() time.Time { return uuidV7Time(m.VersionID) }
```

### How immutable CRUD works

Your code looks identical to a mutable table:

```go
// Create — generates id, version_id, and row_hash automatically
transfer := &models.Transfer{CustomerID: id, Amount: amount, Status: "pending"}
models.QueryTransfer().Create(transfer)

// Read — returns the latest version per id, transparently
transfer, _ := models.QueryTransfer().WhereID(id).First()

// Update — inserts a new version with a fresh version_id
transfer.Status = "completed"
models.QueryTransfer().Update(transfer)

// Soft delete — inserts a version with deleted_at set (requires SoftDeletes())
models.QueryTransfer().Delete(transfer)

// Read full history
versions, _ := models.QueryTransfer().WhereID(id).AllVersions().All()
```

Under the hood, all read queries use `DISTINCT ON (id) ... ORDER BY id, version_id DESC` (Postgres) or an equivalent `MAX(version_id)` subquery (MySQL/SQLite) to return only the latest version per `id`.

## Append-only tables

For permanent records that are never modified — audit logs, financial transactions, event streams. No `Update()` or `Delete()` is generated.

```go
m.CreateTable("transactions", func(t *Table) {
    t.AppendOnly()

    t.UUID("account_id").NotNull().ForeignKey("accounts", "id")
    t.String("type", 20).NotNull()
    t.Decimal("amount", 18, 2).NotNull()
    t.String("currency", 3).NotNull()
})
```

`t.AppendOnly()` automatically injects:

- `id UUID NOT NULL PRIMARY KEY` — UUID v7, generated in Go
- `row_hash BYTEA NOT NULL` — SHA-256 hash chain
- `prev_hash BYTEA NOT NULL` — previous row's hash

Only `Create()` and read methods are generated. Corrections are modeled as new records (e.g., a reversal transaction), not mutations.

## Hash chains and Merkle trees

Every immutable and append-only table is automatically hash-chained. Each row's `row_hash` is `SHA-256(prev_hash || canonical row data)`. Tampering with any historical row breaks the chain.

```go
// Verify the entire chain — O(n), run periodically
err := models.QueryTransaction().VerifyChain()

// Verify a single row
err := models.QueryTransaction().VerifyRow(record)

// Create a Merkle tree checkpoint
cp, _ := models.QueryTransaction().Checkpoint()

// Generate an inclusion proof for a specific row
proof, _ := models.QueryTransaction().Proof(record)

// Verify a proof — pure function, no database needed
ok := models.VerifyProof(proof)
```

`RowHash` and `PrevHash` use `json:"-"` tags — they are internal integrity data, not part of the API response.

### Database permissions

Immutable and append-only tables should be granted `SELECT, INSERT` only — no `UPDATE`, no `DELETE`. See the [ledger test app](../testdata/ledger/README.md) for a complete example of database-level permission enforcement.

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
