# Seeders

Pickle declares fake-data behavior in migrations, next to the schema rules that
determine whether a value is valid. Field seeders describe values; scenario
seeders describe how many rows and relationships to create.

## Field seeders

Attach a provider to a column in its migration:

```go
m.CreateTable("contacts", func(t *Table) {
    t.UUID("id").PrimaryKey().SeedUUID()
    t.String("first_name").NotNull().SeedFirstName(EnUS)
    t.String("last_name").NotNull().SeedLastName(EnUS)
    t.String("email").NotNull().Unique().SeedEmail()
    t.String("phone").Nullable().SeedPhoneNumber(UnitedStates).SeedNull(0.15)
    t.String("time_zone").NotNull().SeedTimeZone()
    t.String("company_name").Nullable().SeedCompanyName()
})
```

Seed declarations are migration metadata. They do not emit database DDL and
do not add a runtime dependency on Pickle.

## Common providers

| Category | Providers |
|----------|-----------|
| Fixed and choice | `SeedValue`, `SeedValues`, `SeedRandomStringIn` |
| Numeric | `SeedInteger`, `SeedBigInteger`, `SeedDecimal`, `SeedMoney` |
| General | `SeedUUID`, `SeedBoolean`, `SeedBooleanWeighted`, `SeedBytes` |
| People | `SeedFirstName`, `SeedLastName`, `SeedFullName`, `SeedUsername`, `SeedJobTitle` |
| Company | `SeedCompanyName`, `SeedCompanySuffix`, `SeedIndustry`, `SeedDepartment` |
| Contact | `SeedEmail`, `SeedSafeEmail`, `SeedPhoneNumber`, `SeedDomainName`, `SeedURL` |
| Address | `SeedStreetAddress`, `SeedCity`, `SeedState`, `SeedPostalCode`, `SeedCountry` |
| Locale | `SeedLocale`, `SeedTimeZone`, `SeedCountryCode` |
| Time | `SeedDateBetween`, `SeedTimeBetween`, `SeedPastTime`, `SeedFutureTime` |
| Text | `SeedWords`, `SeedSentence`, `SeedParagraph` |

Provider arguments use typed country and locale markers such as
`UnitedStates`, `Canada`, and `EnUS`.

## Predictable seeded passwords

A password composite names other generated fields in order:

```go
t.String("password_hash").SeedPassword(
    []string{"first_name", "last_name", "id"},
)
```

Pickle resolves those values, converts them to lowercase text, and joins them
with `-`. A row containing `Ada`, `Lovelace`, and `1` therefore has the seed
password `ada-lovelace-1`. Generated seeder execution hashes the result for
storage, but a developer inspecting seeded rows can derive the credential.

Seeded accounts are fixture accounts, not safe production accounts.

## Changing a field seeder

Seeder metadata evolves through migrations without altering the database
column:

```go
func (m *LocalizeContactSeeds_2026_07_15_100000) Up() {
    m.AlterTable("contacts", func(t *Table) {
        t.AlterColumn("phone").SeedPhoneNumber(Canada)
        t.AlterColumn("company_name").DropSeeder()
    })
}
```

Declare the inverse metadata change in `Down()` just like any other migration
state transition.

## Custom value seeders

`SeederRef` records a stable custom provider name and its logical return type:

```go
var RoleSeeder = NewSeederRef("RoleSeeder", String)

t.String("role").Seed(RoleSeeder)
```

Pickle verifies that the declared return type matches the destination column.
Scenario and custom provider discovery are covered by the scenario seeder
generation layer.

## Resolution precedence

When a row is generated, values resolve in this order:

1. explicit scenario value;
2. relationship value;
3. migration field seeder;
4. database or generated default;
5. nullable choice;
6. framework-managed identity or timestamp generation.

Required columns without a value source fail before insertion. Foreign keys
come from scenario relationships rather than randomly generated identifiers.

## Scenario graphs

Scenario seeders describe counts and relationships:

```go
func (CRMSeeder) Seed(graph *SeedGraph) {
    user := graph.Create(UserSeeder).One()
    contacts := graph.CreateN(ContactSeeder, 25).For(user).Many()

    graph.ForEach(contacts, func(contact SeedRecord) {
        graph.CreateN(NoteSeeder, graph.Between(1, 8)).For(contact)
    })
}
```

`For` resolves the foreign key from migration metadata. Ambiguous
relationships require an explicit local column selector. Composite foreign
keys always propagate as complete ordered tuples.

## Execution guarantees

Each root scenario receives an explicit 64-bit seed. Pickle derives separate
random streams from the scenario name, graph path, row ordinal, column, and
retry number. Adding an unrelated field therefore does not reshuffle existing
fixture values.

Before insertion, Pickle resolves counts, relationships, overrides, field
providers, and password composites. Password composites are bcrypt-hashed and
marked sensitive before any SQL is issued. The root scenario then runs in one
transaction; an insertion failure rolls the whole scenario back.

Mutation is enabled by default only in `local`, `development`, and `test`.
Other environments require both `--force` and an exact
`--confirm-environment` value. Dry runs are non-mutating and may be planned in
any environment.

Run a root scenario through the project binary or the forwarding Pickle CLI:

```bash
pickle db:seed CRMSeeder --seed 8675309
pickle db:seed --list
pickle db:seed CRMSeeder --dry-run
```
