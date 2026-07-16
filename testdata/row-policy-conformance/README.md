# Row-policy conformance corpus

`cases.json` is the versioned portable-policy matrix shared by generator,
application-runtime, PostgreSQL fixture, and export tests. Every case must have
the same decision in three lanes: generated application enforcement with RLS
absent, direct SQL as a constrained PostgreSQL runtime role, and generated
application SQL with RLS active.

The PostgreSQL lane uses separate schema-owner, migration, and runtime roles.
Set `PICKLE_POSTGRES_TEST_DSN` when running the integration fixture; ordinary
unit runs retain the corpus and skip only the external database execution.
Application-only immutable physical plans are recorded separately and must not
emit an RLS or dual-enforcement claim.

