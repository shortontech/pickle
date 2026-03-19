# Ledger

A financial ledger built with Pickle. Demonstrates immutable tables, append-only tables, and database-level permission enforcement for fintech applications.

## Database Permissions

This app is designed to run with **two database roles**: a privileged migration runner and a restricted application user. The application user should never have the permissions needed to mutate or destroy financial records — that guarantee lives in Postgres, not in application code.

### Migration Role (`ledger_admin`)

Runs migrations, creates tables, manages indexes. Used by `pickle migrate` and CI/CD pipelines. Never used by the running application.

```sql
CREATE ROLE ledger_admin WITH LOGIN PASSWORD '...';
GRANT ALL PRIVILEGES ON DATABASE ledger TO ledger_admin;
```

### Application Role (`ledger_app`)

The running application connects as this role. Permissions are scoped per table based on mutability:

```sql
CREATE ROLE ledger_app WITH LOGIN PASSWORD '...';

-- Mutable tables: full CRUD
GRANT SELECT, INSERT, UPDATE, DELETE ON users TO ledger_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON sessions TO ledger_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON jwt_tokens TO ledger_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON oauth_tokens TO ledger_app;

-- Immutable tables (versioned): SELECT + INSERT only
-- "Updates" are new version rows. "Deletes" are new rows with deleted_at set.
-- No UPDATE or DELETE ever touches these tables.
GRANT SELECT, INSERT ON accounts TO ledger_app;

-- Append-only tables: SELECT + INSERT only
-- Records are permanent. Reversals are new entries, not modifications.
-- No UPDATE or DELETE ever touches this table.
GRANT SELECT, INSERT ON transactions TO ledger_app;

-- Migrations table: read-only for the app
GRANT SELECT ON migrations TO ledger_app;
```

### Why This Works

Pickle's table types map directly to database permissions:

| Table Type | Pickle DSL | Generated Methods | DB Permissions |
|---|---|---|---|
| Mutable | `t.Timestamps()` | Create, Update, Delete | SELECT, INSERT, UPDATE, DELETE |
| Immutable | `t.Immutable()` | Create, Update*, Delete** | SELECT, INSERT |
| Append-Only | `t.AppendOnly()` | Create | SELECT, INSERT |

\* Immutable `Update()` inserts a new version row — it never issues a SQL `UPDATE`.
\** Immutable `Delete()` is only generated when `t.SoftDeletes()` is declared, and it inserts a new version row with `deleted_at` set — it never issues a SQL `DELETE`.

If a bug, exploit, or compromised dependency tries to issue a raw `UPDATE` or `DELETE` against the `transactions` table, Postgres rejects it. The protection isn't in the framework — it's in the database. Application code can't bypass it, even with SQL injection, because the connection itself lacks the privilege.

### The Ledger Guarantee

The `transactions` table uses `t.AppendOnly()`:

- Every transaction is a permanent, immutable record
- There is no `Update()` method — the compiler prevents it
- There is no `Delete()` method — the compiler prevents it
- The database user has no `UPDATE` or `DELETE` grant — Postgres prevents it
- Reversals create new entries that reference the original via `reverses_id`
- The audit trail is the table itself

Three layers of enforcement: **schema DSL** (no methods generated), **Go compiler** (can't call what doesn't exist), **database permissions** (can't execute what isn't granted). Any one of them is sufficient. All three together means you can prove it to an auditor.

## Running

```bash
# Generate all code
pickle generate --project .

# Run migrations (requires ledger_admin credentials)
DATABASE_URL="postgres://ledger_admin:...@localhost:5432/ledger" pickle migrate

# Start the server (uses ledger_app credentials)
DATABASE_URL="postgres://ledger_app:...@localhost:5432/ledger" go run ./cmd/server/
```

## Schema

**users** — Mutable. Standard user accounts with bcrypt passwords.

**accounts** — Immutable with soft deletes. Each "update" is a new version row. Closing an account sets `deleted_at` on a new version. Full version history available via `.AllVersions()`.

**transactions** — Append-only. Write once, read forever. Credits add, debits subtract, reversals offset. Balance is computed from the transaction log, not stored.

## API

```
POST   /api/auth/login              # Get a JWT
GET    /api/accounts                 # List your accounts
POST   /api/accounts                 # Create an account
GET    /api/accounts/:id             # Show an account
GET    /api/accounts/:id/balance     # Computed balance
PUT    /api/accounts/:id             # Update account (new version)
DELETE /api/accounts/:id             # Soft-delete account (new version)
GET    /api/:account_id/transactions       # List transactions
POST   /api/:account_id/transactions       # Create a transaction
GET    /api/:account_id/transactions/:id   # Show a transaction
POST   /api/:account_id/transactions/:id/reverse  # Reverse a transaction
```
