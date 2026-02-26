package cooked

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
)

// DB is the package-level database connection. Set during app initialization.
var DB *sql.DB

// Query starts a new query for the given model type.
func Query[T any](table string) *QueryBuilder[T] {
	return &QueryBuilder[T]{table: table}
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
	var zero T
	cols := dbColumns(&zero)

	var b strings.Builder
	b.WriteString("SELECT ")
	b.WriteString(strings.Join(cols, ", "))
	b.WriteString(" FROM ")
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

// dbColumns returns the db-tagged column names from a struct in field order.
func dbColumns(v any) []string {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	rt := rv.Type()
	var cols []string
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("db")
		if tag != "" && tag != "-" {
			cols = append(cols, tag)
		}
	}
	return cols
}

// dbValues returns field values from a struct in db tag field order.
func dbValues(v any) []any {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	rt := rv.Type()
	var vals []any
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("db")
		if tag != "" && tag != "-" {
			vals = append(vals, rv.Field(i).Interface())
		}
	}
	return vals
}

// dbScanDest returns a slice of field pointers from a struct in db tag field order.
func dbScanDest(v any) []any {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	rt := rv.Type()
	var ptrs []any
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("db")
		if tag != "" && tag != "-" {
			ptrs = append(ptrs, rv.Field(i).Addr().Interface())
		}
	}
	return ptrs
}

// scanRow scans a single row into a struct using db tag field order.
// buildSelect emits explicit column names in the same order, so positions align.
func scanRow[T any](row *sql.Row, dest *T) error {
	return row.Scan(dbScanDest(dest)...)
}

// scanRows scans multiple rows into structs using db tag field order.
func scanRows[T any](rows *sql.Rows) ([]T, error) {
	var results []T
	for rows.Next() {
		var item T
		if err := rows.Scan(dbScanDest(&item)...); err != nil {
			return nil, err
		}
		results = append(results, item)
	}
	return results, rows.Err()
}

// buildInsert builds a parameterized INSERT statement from a struct's db tags.
func buildInsert[T any](table string, record *T) (string, []any) {
	cols := dbColumns(record)
	vals := dbValues(record)
	placeholders := make([]string, len(cols))
	for i := range cols {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	return fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		table,
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
	), vals
}

// buildUpdate builds a parameterized UPDATE statement from a struct's db tags.
// The "id" column is excluded from SET and used in WHERE if no conditions are set.
func buildUpdate[T any](table string, record *T, conditions []condition) (string, []any) {
	rv := reflect.ValueOf(record).Elem()
	rt := rv.Type()

	var setCols []string
	var setVals []any
	var idVal any

	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("db")
		if tag == "" || tag == "-" {
			continue
		}
		val := rv.Field(i).Interface()
		if tag == "id" {
			idVal = val
			continue
		}
		setCols = append(setCols, tag)
		setVals = append(setVals, val)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("UPDATE %s SET ", table))
	setParts := make([]string, len(setCols))
	for i, col := range setCols {
		setParts[i] = fmt.Sprintf("%s = $%d", col, i+1)
	}
	b.WriteString(strings.Join(setParts, ", "))

	args := append([]any{}, setVals...)

	if len(conditions) > 0 {
		b.WriteString(" WHERE ")
		for i, c := range conditions {
			if i > 0 {
				b.WriteString(" AND ")
			}
			b.WriteString(fmt.Sprintf("%s %s $%d", c.column, c.op, len(args)+1))
			args = append(args, c.value)
		}
	} else if idVal != nil {
		b.WriteString(fmt.Sprintf(" WHERE id = $%d", len(args)+1))
		args = append(args, idVal)
	}

	return b.String(), args
}
