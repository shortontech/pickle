# 070 — Migration field seeders

## Goal

Make fake-data intent part of Pickle's versioned schema metadata. A column may
declare how Pickle should produce a valid seed value alongside its database
type, constraints, default, encryption, and visibility:

```go
m.CreateTable("contacts", func(t *Table) {
    t.UUID("id").PrimaryKey()
    t.UUID("user_id").NotNull().ForeignKey("users", "id")
    t.String("first_name").NotNull().SeedFirstName()
    t.String("last_name").NotNull().SeedLastName()
    t.String("email").NotNull().Unique().SeedEmail()
    t.String("phone").Nullable().SeedPhoneNumber(UnitedStates)
    t.String("time_zone").NotNull().SeedTimeZone()
    t.String("company_name").Nullable().SeedCompanyName()
    t.String("role").NotNull().Seed(RoleSeeder)
})
```

Field seeders generate values. They do not decide how many rows exist or how
records relate; scenario seeders in spec 071 own that graph.

## Migration metadata

Add a serializable seed declaration to column metadata:

```go
type SeedSpec struct {
    Kind       string
    Arguments  []SeedArgument
    Reference  string
    NullWeight float64
}

type Column struct {
    // existing fields...
    Seed *SeedSpec
}
```

Seed metadata is preserved through migration replay, schema inspection,
generated schema types, MCP, and export. It emits no database DDL.

Seed declarations are changeable through later migrations:

```go
m.AlterTable("contacts", func(t *Table) {
    t.AlterColumn("phone").SeedPhoneNumber(Canada)
    t.AlterColumn("company_name").DropSeeder()
})
```

`Down()` restores the previous declaration through the ordinary migration
state transition. Historical migrations remain immutable.

## Built-in seedable values

The initial registry must be broad enough for application fixtures without
requiring a custom faker for ordinary CRM data. Seeders are grouped by the
database types they can satisfy.

### General and scalar

- `SeedValue(value)` and `SeedValues(values...)`
- `SeedRandomString(length)` and `SeedRandomStringIn(values...)`
- `SeedInteger(min, max)`, `SeedBigInteger(min, max)`
- `SeedDecimal(min, max, scale)`
- `SeedBoolean()` and `SeedBooleanWeighted(trueWeight)`
- `SeedUUID()`
- `SeedBytes(length)`
- `SeedJSON(factory)` for a declared JSON-compatible seeder
- `SeedNull(weight)` as an optional modifier on nullable columns

### Person and organization

- `SeedFirstName(locales...)`, `SeedLastName(locales...)`, `SeedFullName(locales...)`
- `SeedUsername()`, `SeedJobTitle()`, `SeedDepartment()`
- `SeedCompanyName()`, `SeedCompanySuffix()`, `SeedIndustry()`

### Contact and internet

- `SeedEmail()`, `SeedSafeEmail()`, `SeedDomainName()`, `SeedURL()`
- `SeedPhoneNumber(countries...)`
- `SeedIPv4()`, `SeedIPv6()`, `SeedUserAgent()`

### Address and locale

- `SeedStreetAddress(countries...)`, `SeedCity(countries...)`
- `SeedState(countries...)`, `SeedPostalCode(countries...)`
- `SeedCountry()`, `SeedCountryCode()`, `SeedLocale()`, `SeedTimeZone()`
- country and locale markers such as `UnitedStates`, `Canada`, and `EnUS` are
  typed constants, not free-form strings

### Time, text, and commerce

- `SeedDateBetween(start, end)`, `SeedTimeBetween(start, end)`
- `SeedPastTime(maxAge)`, `SeedFutureTime(maxDistance)`
- `SeedSentence(words)`, `SeedParagraph(sentences)`, `SeedWords(count)`
- `SeedProductName()`, `SeedCurrencyCode()`, `SeedMoney(min, max)`

### Password field composites

`SeedPassword` accepts an ordered array of other columns in the same seeded
row:

```go
t.String("password_hash").SeedPassword(
    []string{"first_name", "last_name", "id"},
)
```

Pickle resolves those fields first, converts their logical values to lowercase
text, and joins them with `-`. A row containing `Ada`, `Lovelace`, and `1`
therefore receives the plaintext seed password `ada-lovelace-1`. Generated
seeder execution hashes the final value for storage.

This is intentionally a simple fixture convention. Seeded accounts are not
safe accounts, and `SeedPassword` does not attempt to implement production
password policy. It exists so somebody viewing seeded rows can immediately
derive login credentials.

Pickle rejects unknown columns, self-reference, cycles, and values that cannot
be rendered as scalar text. The resolved plaintext is not written to logs or
MCP output.

The registry is extensible without changing the migration representation:
every built-in lowers to a stable `SeedSpec.Kind` plus typed arguments.

## Custom value seeders

Applications may define a named value seeder:

```go
type RoleSeeder struct {
    Seeder[string]
}

func (RoleSeeder) Seed(ctx *SeedValueContext) string {
    return ctx.StringIn("owner", "manager", "agent")
}
```

Pickle discovers the named seeder and exposes its type token for migration DSL
use:

```go
t.String("role").Seed(RoleSeeder)
```

Tickle resolves the type token while editing; generated migration code stores
the stable qualified seeder name. Pickle reads the seeder's declared return
type and verifies that it can be converted to the column's authoritative Go
and database types. User seeders do not return `any`, and runtime reflection is
not used for ordinary value conversion.

Supported conversions include named aliases of compatible primitives,
nullable wrappers, UUIDs, decimals, timestamps, dates, JSON, enums, encrypted
plaintext inputs, and driver representations. A `ResourceID` is not a database
column value and cannot seed two columns implicitly.

## Precedence and validity

When creating a row, Pickle resolves a column in this order:

1. explicit scenario value;
2. relationship value supplied by the seed graph;
3. migration field seeder;
4. database or generated default;
5. nullable zero choice;
6. deterministic generation for framework-managed keys and timestamps.

Generation fails before insertion when a required column has no value source.
Explicit scenario values still pass through the same schema conversion and
validation path.

Constraints affect generation:

- unique values retry deterministically up to a bounded limit;
- enum and `oneof` domains constrain compatible seeders;
- string lengths cap generated values without corrupt truncation;
- decimal precision and scale are preserved;
- plaintext is passed through generated encryption or sealing behavior;
- password field composites are evaluated after their referenced columns and
  hashed before insertion;
- composite keys are generated as complete tuples;
- foreign-key columns are supplied by scenario relationships, not random IDs.

## Implementation plan

1. Add serializable seed metadata and column DSL methods.
2. Preserve seed state through migration replay, alteration, inspection, and
   generated/embed schema code.
3. Add the built-in registry with typed argument validation.
4. Discover custom value seeders and validate return-type compatibility.
5. Expose resolved field seed metadata to the scenario generator in spec 071.

## Likely files

- `pkg/schema/column.go`
- `pkg/schema/table.go`
- migration operation and inspector code
- generated schema embeds
- Tickle seeder token handling
- `docs/Migrations.md`
- new `docs/Seeders.md`

## Tests

- Every built-in accepts only compatible column types and typed arguments.
- Seed metadata survives create, alter, rollback-state replay, inspection, and
  export in declaration order.
- Custom named and alias return types convert correctly.
- Incompatible return types fail generation with table, column, expected type,
  and actual type.
- Required, nullable, unique, enum, length, decimal, encrypted, sealed, JSON,
  and composite-key columns follow the documented precedence.
- Existing migrations without seed declarations remain byte-for-byte
  compatible where practical.

## Acceptance criteria

1. Seed value intent evolves as versioned migration metadata.
2. Common application fields require no handwritten faker code.
3. Pickle derives safe database conversion from schema and declared return
   types rather than runtime guesses.
4. Field seeders never hide relationship or authorization semantics.

## Non-goals

- Row counts, graph topology, or scenario ordering.
- Treating ResourceID as a database column type.
- Production data migrations or backfills.
- Calling external APIs to generate values.
