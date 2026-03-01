# Views

Views let you define database views in your migration files. Pickle generates read-only model structs and typed query scopes from them — same as tables, minus the write operations.

## When to Use Views

- Computed columns (aggregations, window functions)
- Joins you query frequently
- Denormalized read models
- Anything where `SELECT` is all you need

## CreateView / DropView

```go
func (m *MyMigration) Up() {
    m.CreateView("customer_transfer_rankings", func(v *View) {
        v.From("transfers", "t")
        v.Join("customers", "c", "c.id = t.customer_id")
        v.Column("t.id")
        v.Column("t.customer_id")
        v.Column("c.name", "customer_name")
        v.Column("t.created_at")
        v.GroupBy("t.customer_id", "c.name", "t.id", "t.created_at")
        v.SelectRaw("rank", "ROW_NUMBER() OVER (PARTITION BY t.customer_id ORDER BY t.created_at DESC)").BigInteger()
        v.SelectRaw("total_amount", "SUM(t.amount) OVER (PARTITION BY t.customer_id)").Decimal(18, 2)
    })
}

func (m *MyMigration) Down() {
    m.DropView("customer_transfer_rankings")
}
```

## DSL Reference

### Sources

| Method | Description |
|--------|-------------|
| `v.From(table, alias)` | Primary source table |
| `v.Join(table, alias, on)` | INNER JOIN |
| `v.LeftJoin(table, alias, on)` | LEFT JOIN |

All sources require aliases. Column references use `"alias.column"` format to avoid ambiguity.

### Columns

| Method | Description |
|--------|-------------|
| `v.Column("t.id")` | Reference a source column — type is resolved from the source table |
| `v.Column("c.name", "customer_name")` | Same, with an output alias |
| `v.SelectRaw("name", "SQL expr")` | Computed column — must declare type explicitly |
| `v.GroupBy("col1", "col2")` | GROUP BY clause |

### Type Builders (for SelectRaw)

Chain a type method after `SelectRaw` to declare the Go type:

```go
v.SelectRaw("rank", "ROW_NUMBER() OVER (...)").BigInteger()
v.SelectRaw("total", "SUM(amount)").Decimal(18, 2)
v.SelectRaw("label", "CONCAT(first, ' ', last)").StringType()
v.SelectRaw("is_active", "CASE WHEN ...").BooleanType()
v.SelectRaw("last_seen", "MAX(created_at)").TimestampType()
v.SelectRaw("user_id", "t.user_id").UUIDType()
v.SelectRaw("data", "jsonb_agg(...)").JSONBType()
v.SelectRaw("note", "string_agg(...)").TextType()
v.SelectRaw("count", "COUNT(*)").IntegerType()
```

## Type Resolution

- **Plain columns** (`v.Column("t.id")`) — type is resolved from the source table's schema at generation time. If the source table defines `id` as UUID, the view column is UUID.
- **SelectRaw columns** — type must be declared explicitly via builder methods. Pickle can't infer types from SQL expressions.

## Generated Code

Given the view above, Pickle generates:

**Model struct** (`models/customerTransferRanking.go`):
```go
type CustomerTransferRanking struct {
    ID             uuid.UUID       `json:"id" db:"id"`
    CustomerID     uuid.UUID       `json:"customer_id" db:"customer_id"`
    CustomerName   string          `json:"customer_name" db:"customer_name"`
    CreatedAt      time.Time       `json:"created_at" db:"created_at"`
    Rank           int64           `json:"rank" db:"rank"`
    TotalAmount    decimal.Decimal `json:"total_amount" db:"total_amount"`
}
```

**Read-only query type** (`models/customerTransferRanking_query.go`):
```go
type CustomerTransferRankingQuery struct {
    *QueryBuilder[CustomerTransferRanking]
}

func QueryCustomerTransferRanking() *CustomerTransferRankingQuery { ... }

// Where methods for each column
func (q *CustomerTransferRankingQuery) WhereCustomerID(val uuid.UUID) *CustomerTransferRankingQuery { ... }
func (q *CustomerTransferRankingQuery) WhereRank(val int64) *CustomerTransferRankingQuery { ... }
// etc.
```

Views generate **read-only** query types. There are no `Create`, `Update`, or `Delete` methods — those are only available on table models.

## Usage

```go
// Top 10 customers by transfer count
rankings, err := models.QueryCustomerTransferRanking().
    WhereRankLTE(10).
    All()

// Stats for a specific user
stats, err := models.QueryUserPostStat().
    WhereID(userID).
    First()
```
