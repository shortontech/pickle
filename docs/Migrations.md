# Migrations

## RLS migration interoperability

Portable authorization belongs in `database/policies/`, not duplicated migration SQL. Pickle generates and reconciles its reserved `pickle_` PostgreSQL policies during explicit policy migration. Manual database-only constraints may coexist only through structured `CreateRLSPolicy(...).RestrictiveDefenseInDepth()`, which emits `AS RESTRICTIVE`. Raw migration SQL remains appropriate for roles, grants, and helper functions; Squeeze rejects unprovable policy-affecting SQL beside a Pickle-protected table.

Protecting existing data after adding a non-null ownership column requires an explicit expand/backfill/protect sequence. Pickle never invents owners or silently classifies old rows.

PostgreSQL row-level security has a structured migration DSL for enabling RLS and managing policies. See [PostgreSQL Row-Level Security](RLS.md).

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
| `.Encrypted()` | Mark as requiring encryption at rest — see [Encryption](Encryption.md) |
| `.Sealed()` | Mark as write-only encrypted — can be verified but never retrieved in plaintext. See [Encryption](Encryption.md) |
| `.UnsafePublic()` | Acknowledge that a sensitive field is intentionally `.Public()` |

## Composite keys and foreign keys

Declare a compound primary key after adding its columns, then use a table-level
foreign key to preserve the same boundary in dependent tables:

```go
m.CreateTable("parties", func(t *Table) {
    t.BigInteger("organization_id").NotNull()
    t.BigInteger("party_id").NotNull()
    t.String("name").NotNull()
    t.PrimaryKey("organization_id", "party_id")
})

m.CreateTable("notes", func(t *Table) {
    t.BigInteger("organization_id").NotNull()
    t.BigInteger("party_id").NotNull()
    t.BigInteger("note_id").NotNull()
    t.PrimaryKey("organization_id", "note_id")

    t.ForeignKey(
        []string{"organization_id", "party_id"},
        "parties",
        []string{"organization_id", "party_id"},
    ).OnDelete("CASCADE").OnUpdate("RESTRICT")
})
```

Pickle preserves column order and emits a table-level constraint on PostgreSQL,
MySQL, and SQLite. Source and referenced lists must be nonempty and have equal
lengths. Local columns must already exist and columns may not be repeated.

Supported referential actions are `CASCADE`, `RESTRICT`, `NO ACTION`,
`SET NULL`, and `SET DEFAULT`. The existing single-column
`.ForeignKey(table, column)` modifier remains available and unchanged.

## Ownership & visibility

For scope-local records, keep the real composite integer identity in the
schema even when the HTTP boundary exposes a `ResourceID`:

```go
t.BigInteger("organization_id").NotNull()
t.BigInteger("record_id").NotNull()
t.PrimaryKey("organization_id", "record_id")
```

Related tables must reference the complete tuple in the same declaration
order. Encoding these values as a ResourceID does not replace the composite
foreign key or authorize cross-scope access.

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
| `m.RawSQL(sql)` | Execute explicitly declared SQL through the migration transaction |

`RawSQL` is intended for database-native invariants that the schema DSL cannot
express, such as PostgreSQL row-level-security policies. It executes in both
`Up()` and `Down()` and propagates database errors. Raw SQL remains a manual
review boundary; never construct it from request or other untrusted input.

Field seeders are also versioned migration metadata. They describe fake-data
providers without emitting DDL; see [Seeders](Seeders.md).

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

## Role annotations

Columns can declare which roles are allowed to see them using `RoleSees()`:

```go
m.CreateTable("users", func(t *Table) {
    t.UUID("id").PrimaryKey().Default("uuid_generate_v7()").Public()
    t.String("name").NotNull().Public()
    t.String("email").NotNull().RoleSees("admin", "support")
    t.String("phone").NotNull().RoleSees("admin")
    t.String("internal_score").NotNull().RoleSees("analyst")
    t.Timestamps()
})
```

`RoleSees(roles...)` declares that only users with one of the listed roles can see the column. Pickle generates:

- `XxxSees()` methods on the model (e.g., `EmailSees() []string` returns `["admin", "support"]`)
- A `VisibleTo` map on the model metadata, keyed by column name, used by `SelectFor()` and `SelectForRoles()` in the query builder

Role annotations work alongside `.Public()` and `.OwnerSees()`. A column can have both visibility tiers and role restrictions. `.Public()` columns are always visible regardless of role.

Policy migrations for roles themselves live in `database/policies/`. Squeeze validates that role slugs referenced in `RoleSees()` exist in your roles migration — see the `unknown_role_annotation` and `stale_role_annotation` rules.

Nullable columns become pointer types (`*string`, `*time.Time`). The `json` and `db` struct tags are generated automatically.
