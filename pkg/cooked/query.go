package cooked

import (
	"database/sql"
	"fmt"
	"strings"
)

// DB is the package-level database connection. Set during app initialization.
var DB *sql.DB

// Query starts a new query for the given model type.
func Query[T any]() *QueryBuilder[T] {
	return &QueryBuilder[T]{}
}

// QueryBuilder is the generic query builder for all models.
type QueryBuilder[T any] struct {
	table      string
	conditions []condition
	orderBy    []string
	limit      int
	offset     int
	eagerLoads []string
}

type condition struct {
	column string
	op     string
	value  any
}

// Where adds a condition to the query.
func (q *QueryBuilder[T]) Where(column string, value any) *QueryBuilder[T] {
	q.conditions = append(q.conditions, condition{column: column, op: "=", value: value})
	return q
}

// WhereOp adds a condition with a custom operator.
func (q *QueryBuilder[T]) WhereOp(column, op string, value any) *QueryBuilder[T] {
	q.conditions = append(q.conditions, condition{column: column, op: op, value: value})
	return q
}

// WhereIn adds a column IN (...) condition.
func (q *QueryBuilder[T]) WhereIn(column string, values any) *QueryBuilder[T] {
	q.conditions = append(q.conditions, condition{column: column, op: "IN", value: values})
	return q
}

// WhereNotIn adds a column NOT IN (...) condition.
func (q *QueryBuilder[T]) WhereNotIn(column string, values any) *QueryBuilder[T] {
	q.conditions = append(q.conditions, condition{column: column, op: "NOT IN", value: values})
	return q
}

// OrderBy adds an ORDER BY clause.
func (q *QueryBuilder[T]) OrderBy(column, direction string) *QueryBuilder[T] {
	q.orderBy = append(q.orderBy, column+" "+direction)
	return q
}

// Limit sets the LIMIT clause.
func (q *QueryBuilder[T]) Limit(n int) *QueryBuilder[T] {
	q.limit = n
	return q
}

// Offset sets the OFFSET clause.
func (q *QueryBuilder[T]) Offset(n int) *QueryBuilder[T] {
	q.offset = n
	return q
}

// EagerLoad marks a relationship for eager loading.
func (q *QueryBuilder[T]) EagerLoad(relation string) *QueryBuilder[T] {
	q.eagerLoads = append(q.eagerLoads, relation)
	return q
}

// First returns the first matching record.
func (q *QueryBuilder[T]) First() (*T, error) {
	q.limit = 1
	query, args := q.buildSelect()
	row := DB.QueryRow(query, args...)

	var result T
	if err := scanRow(row, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// All returns all matching records.
func (q *QueryBuilder[T]) All() ([]T, error) {
	query, args := q.buildSelect()
	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanRows[T](rows)
}

// Count returns the number of matching records.
func (q *QueryBuilder[T]) Count() (int64, error) {
	query, args := q.buildCount()
	var count int64
	err := DB.QueryRow(query, args...).Scan(&count)
	return count, err
}

// Create inserts a new record.
func (q *QueryBuilder[T]) Create(record *T) error {
	query, args := buildInsert(q.table, record)
	_, err := DB.Exec(query, args...)
	return err
}

// Update updates an existing record.
func (q *QueryBuilder[T]) Update(record *T) error {
	query, args := buildUpdate(q.table, record, q.conditions)
	_, err := DB.Exec(query, args...)
	return err
}

// Delete removes matching records.
func (q *QueryBuilder[T]) Delete(record *T) error {
	query, args := q.buildDelete()
	_, err := DB.Exec(query, args...)
	return err
}

func (q *QueryBuilder[T]) buildSelect() (string, []any) {
	var b strings.Builder
	b.WriteString("SELECT * FROM ")
	b.WriteString(q.table)

	args := q.appendWhere(&b)

	if len(q.orderBy) > 0 {
		b.WriteString(" ORDER BY ")
		b.WriteString(strings.Join(q.orderBy, ", "))
	}
	if q.limit > 0 {
		b.WriteString(fmt.Sprintf(" LIMIT %d", q.limit))
	}
	if q.offset > 0 {
		b.WriteString(fmt.Sprintf(" OFFSET %d", q.offset))
	}

	return b.String(), args
}

func (q *QueryBuilder[T]) buildCount() (string, []any) {
	var b strings.Builder
	b.WriteString("SELECT COUNT(*) FROM ")
	b.WriteString(q.table)

	args := q.appendWhere(&b)
	return b.String(), args
}

func (q *QueryBuilder[T]) buildDelete() (string, []any) {
	var b strings.Builder
	b.WriteString("DELETE FROM ")
	b.WriteString(q.table)

	args := q.appendWhere(&b)
	return b.String(), args
}

func (q *QueryBuilder[T]) appendWhere(b *strings.Builder) []any {
	if len(q.conditions) == 0 {
		return nil
	}

	var args []any
	b.WriteString(" WHERE ")
	for i, c := range q.conditions {
		if i > 0 {
			b.WriteString(" AND ")
		}
		b.WriteString(fmt.Sprintf("%s %s $%d", c.column, c.op, i+1))
		args = append(args, c.value)
	}
	return args
}

// scanRow scans a single row into a struct. Placeholder — real implementation
// uses reflection or generated code to map columns to struct fields.
func scanRow[T any](row *sql.Row, dest *T) error {
	// TODO: generated per-model scan function
	return row.Err()
}

// scanRows scans multiple rows into structs.
func scanRows[T any](rows *sql.Rows) ([]T, error) {
	// TODO: generated per-model scan function
	var results []T
	return results, nil
}

// buildInsert builds an INSERT statement. Placeholder — real implementation
// uses reflection or generated code to extract column values.
func buildInsert[T any](table string, record *T) (string, []any) {
	// TODO: generated per-model insert builder
	return fmt.Sprintf("INSERT INTO %s DEFAULT VALUES", table), nil
}

// buildUpdate builds an UPDATE statement. Placeholder.
func buildUpdate[T any](table string, record *T, conditions []condition) (string, []any) {
	// TODO: generated per-model update builder
	return fmt.Sprintf("UPDATE %s SET updated_at = NOW()", table), nil
}
