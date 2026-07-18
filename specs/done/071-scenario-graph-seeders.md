# 071 — Scenario and relationship seeders

## Depends on

070.

## Goal

Let application seeders describe populations and relationships without
repeating column-level fake-data mechanics:

```go
type CRMSeeder struct {
    Seeder
}

func (CRMSeeder) Seed(s *SeedGraph) {
    user := s.Create(UserSeeder)

    contacts := s.CreateN(ContactSeeder, 25).For(user)
    s.ForEach(contacts, func(contact SeedRecord) {
        s.CreateN(NoteSeeder, s.Between(1, 8)).For(contact)
    })
}
```

The scenario says “one user, 25 contacts for that user, and one to eight notes
per contact.” Pickle derives values, casts, inserts, foreign keys, and ordering
from migrations and the seeder return types.

## Files and discovery

User seeders live in `database/seeders/`. `pickle make:seeder CRM` creates a
root scenario; `pickle make:seeder Contact --value` creates a reusable row or
value seeder.

Pickle discovers exported types embedding `Seeder`, `Seeder[T]`, or another
documented generated seeder primitive. Files ending `_gen.go` are ignored as
inputs. Seeder names are stable identifiers used by commands and dependency
metadata.

## Row seeders and return types

A row seeder returns a typed declaration:

```go
type ContactSeed struct {
    UserID   SeedRef[User]
    Status   ContactStatus
    Source   string
}

type ContactSeeder struct {
    Seeder[ContactSeed]
}

func (ContactSeeder) Seed(ctx *SeedValueContext) ContactSeed {
    return ContactSeed{
        Status: ctx.StringIn(ContactLead, ContactActive),
        Source: ctx.StringIn("web", "referral", "import"),
    }
}
```

By convention `ContactSeeder` targets `contacts`; an explicit
`Table() string` method overrides the convention. Pickle reads the declared
return type, matches its fields to schema columns, and generates the required
casts and insert code. Omitted fields fall through to migration field seeders
from spec 070.

Unknown fields, ambiguous table names, incompatible return types, and missing
required columns fail generation. Seeder struct tags may resolve deliberate
name differences:

```go
ExternalOwner SeedRef[User] `seed:"user_id"`
```

## Graph primitives

The initial declarative graph supports:

```go
s.Create(UserSeeder)
s.CreateN(ContactSeeder, 25)
s.ForEach(records, fn)
s.Between(1, 8)
s.For(parent)
s.With("status", ContactActive)
s.WithFactory("email", WorkEmailSeeder)
s.DependsOn(RoleCatalogSeeder)
```

`Create` returns one typed seed handle. `CreateN` returns a typed collection of
handles. Tickle supplies editor-safe source forms and lowers these declarations
to generated concrete functions; the generated application does not use
reflection or depend on Pickle at runtime.

`For(parent)` resolves a unique declared relationship between the child and
parent tables. If there are zero or multiple possible relationships, generation
fails and requires an explicit relationship selector:

```go
s.CreateN(ContactSeeder, 25).For(user, Through("user_id"))
```

Composite foreign keys are filled as complete ordered tuples. A relationship
may never copy only the local record half of a scoped identity.

## Overrides and composition

Scenario values override row-seeder and migration field seeders:

```go
s.CreateN(ContactSeeder, 10).
    For(user).
    With("status", ContactActive).
    WithFactory("email", SupportEmailSeeder)
```

Scenarios may invoke reusable sub-scenarios and share returned handles. Cyclic
scenario dependencies fail generation with the full cycle. Record creation is
topologically ordered from declared foreign keys and explicit dependencies.

Optional relationships require an explicit probability or count; Pickle does
not randomly leave required graph edges disconnected.

## Identity and references

`SeedRecord` and typed seed handles are build-time graph references, not model
instances. Generated execution captures inserted or generated keys and makes
them available to dependent nodes.

- framework-generated UUIDs and integer sequences are captured automatically;
- composite identities remain tuples;
- ResourceIDs may be computed for scenario logic or output but are never
  persisted in place of their integer components;
- database-generated values that cannot be returned by a driver require a
  deterministic Pickle-side generator or an explicit post-insert lookup.

References are valid only within one seed execution unless a scenario
explicitly resolves an existing row by a stable unique key.

## Existing-row and catalog seeders

Stable catalogs such as roles may declare natural-key identity:

```go
type RoleCatalogSeeder struct {
    Seeder[RoleSeed]
}

func (RoleCatalogSeeder) Identity() SeedIdentity {
    return UniqueBy("slug")
}
```

This allows other seeders to use `Seed(RoleCatalogSeeder)` for a compatible
column or graph relationship. Resolution uses the declared unique key and an
explicit execution policy from spec 072; it never selects an arbitrary row.

## Implementation plan

1. Add seeder discovery and typed return-structure inspection.
2. Define graph nodes, handles, counts, overrides, and relationship selection.
3. Resolve the graph against schema metadata and field seeders.
4. Generate topologically ordered, parameterized insert code.
5. Add scaffolding and a CRM fixture covering nested and composite relations.

## Likely files

- new `pkg/generator/seeder_*` files
- new cooked seeder primitives and generated embeds
- `pkg/tickle` seeder lowering
- `pkg/scaffold/scaffold.go`
- `cmd/pickle/main.go`
- `database/seeders/` fixture applications
- `docs/Seeders.md`

## Tests

- One-to-many and nested one-to-many scenarios create the requested counts.
- Overrides follow the documented precedence.
- Missing, ambiguous, and cyclic relationships fail before insertion.
- Composite foreign keys are copied as complete tuples.
- Cross-scope relationships are rejected by the database and caught before
  execution when graph metadata proves the mismatch.
- Seeder return aliases, enums, nullable fields, and struct tags cast correctly.
- Required fields without any value source fail generation.
- Generated applications compile without a Pickle runtime dependency.

## Acceptance criteria

1. Scenario code describes cardinality and graph shape rather than SQL wiring.
2. Seeder return types drive generated casting and schema validation.
3. Relationship order and foreign-key propagation are deterministic.
4. Complete composite identities are preserved throughout the graph.

## Non-goals

- Randomly inferring ambiguous relationships.
- A general object-relational mapper.
- Long-lived references between independent seed runs.
- Production data migration or anonymization.
