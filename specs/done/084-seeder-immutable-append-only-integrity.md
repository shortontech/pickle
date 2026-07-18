# 084 — Seeder integrity parity for immutable and append-only tables

## Depends on

020 and the implemented core of 070–072.

## Problem

Pickle owns the physical integrity representation of tables declared with
`Immutable()` or `AppendOnly()`. Generated query builders create `id`,
`version_id`, `prev_hash`, and `row_hash` values and use Pickle's canonical row
serialization. Application code is not supposed to calculate or supply those
columns.

The generated seed executor currently inserts planned rows directly. A project
therefore has to provide integrity columns in custom row seeders even though
the same values are framework-owned everywhere else. That creates two hash
implementations, permits fixtures that do not verify under the generated query
builder, and contradicts Squeeze's rule against manually setting integrity
columns.

## Outcome

Make seeded immutable and append-only rows use the exact same identity,
predecessor, canonicalization, and hash behavior as ordinary generated creates.
Projects describe logical fixture values only. Pickle supplies all injected
physical integrity values before validation and insertion.

## Required behavior

For a table whose schema metadata declares `Immutable()` or `AppendOnly()`, the
seed planner/executor must:

- reject scenario and row-seeder overrides for `row_hash` and `prev_hash`;
- generate omitted framework-owned `id` and, for immutable tables,
  `version_id` deterministically from the root seed and graph path;
- preserve an explicit logical `id` when a scenario deliberately creates
  multiple immutable versions of one resource;
- determine the correct predecessor using the same chain semantics as the
  generated query builder;
- include rows planned earlier in the same scenario when resolving a
  predecessor;
- compute `row_hash` from the same canonical fields, order, encodings, and
  `prev_hash` used by generated query-builder writes;
- insert all integrity values inside the root scenario transaction; and
- produce rows accepted by the generated verification APIs without a
  fixture-specific compatibility path.

There must be one shared generated integrity implementation. Do not copy the
hash algorithm into the seed runtime, exporter, or application glue as
independent handwritten variants.

## Existing database state and repeat policies

The executor must handle both an empty chain and a chain with existing rows.
Predecessor reads occur through the active transaction and use deterministic
ordering. PostgreSQL execution must prevent two writers from selecting the same
chain head and silently forking a chain. Introduce one shared transactional
locking seam used by both generated query-builder writes and seed execution;
do not assume the current tail read already provides concurrency exclusion.

`InsertOrIgnore` and `Upsert` must not mutate immutable or append-only history:

- ignoring an already present declared identity is allowed only when the
  existing row is the same logical seeded row and no new chain row is needed;
- `Upsert` remains unavailable for immutable and append-only physical rows;
- an immutable logical update is represented by an explicitly declared new
  version, never an SQL update to an existing version; and
- conflict handling must not consume or publish a hash that was planned for a
  row that was not inserted.

If safe repeat behavior cannot be proven for a combination, generation or
planning fails with an actionable error instead of weakening immutability.

## Planning and dry runs

`db:seed --dry-run` must validate that integrity values are resolvable without
printing raw hashes as application-authored fixture data. It should report that
the columns are framework-derived and whether predecessor resolution requires
database state.

Read-only MCP plans expose the same classification. A plan that needs a live
chain head must say so; the MCP planning surface must not open the database.

## Squeeze

Update seeder analysis so framework-derived integrity columns satisfy required
column checks. Add or extend a hard diagnostic for application-authored
`row_hash` or `prev_hash` values in scenario overrides and custom row-seeder
returns.

The diagnostic must distinguish authored seeder source from generated code and
point to this framework-owned behavior. Existing raw SQL integrity diagnostics
remain unchanged.

## Generated and exported parity

Generated applications and standalone exports must retain identical behavior
without importing Pickle at runtime. Schema inspection must preserve the table
mode metadata needed by the seed planner. Export fixtures must verify through
the exported application's generated integrity verifier.

## Verification

- Unit tests prove byte-identical hashes between a seeded create and the
  generated query-builder create for the same logical row and predecessor.
- SQLite integration tests cover empty chains, multiple rows in one scenario,
  multiple immutable versions, rollback, and repeat execution.
- PostgreSQL integration tests cover existing chain heads, transaction
  rollback, concurrent writers, and normal restricted credentials.
- Negative tests reject authored hashes, incomplete immutable identities,
  unsafe upserts, and ambiguous predecessor state.
- Export tests seed and verify immutable and append-only rows without a Pickle
  runtime dependency.
- `go test ./...` and strict Squeeze pass.

## Acceptance criteria

1. Applications never calculate integrity columns merely to use `db:seed`.
2. Seeded rows verify through the ordinary generated integrity APIs.
3. Seeder execution cannot fork or rewrite an established integrity chain.
4. Dry-run, MCP, generated application, and export behavior agree.
5. Dill can remove its fixture-specific hash helpers after adopting this work.

## Non-goals

- Changing Pickle's canonical hash format or existing table-mode semantics.
- Repairing or importing previously invalid chains.
- Providing application-specific audit, outbox, or domain-event behavior.
- Treating raw seed insertion as a substitute for exercising domain commands.
