package cooked

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ImmutableQuery starts a new immutable query for the given model type.
func ImmutableQuery[T any](table string, softDeletes bool, connection ...string) *ImmutableQueryBuilder[T] {
	q := &ImmutableQueryBuilder[T]{table: table, softDeletes: softDeletes}
	if len(connection) > 0 {
		q.connection = connection[0]
	}
	return q
}

// ImmutableQueryBuilder is the query builder for immutable (versioned) tables.
// It is completely separate from QueryBuilder — no embedding, no inherited methods.
// All queries dedup to the latest version_id per id.
type ImmutableQueryBuilder[T any] struct {
	table        string
	connection   string
	conditions   []condition
	orderBy      []string
	limit        int
	offset       int
	eagerLoads   []string
	selectedCols []string
	visibility   visibilityMode
	softDeletes  bool
	allVersions  bool
	tx           *sql.Tx       // transaction connection (nil = use global DB)
	lockMode     string        // "", "FOR UPDATE", "FOR SHARE"
	lockOpt      string        // "", "SKIP LOCKED", "NOWAIT"
	lockTimeout  time.Duration // per-query lock timeout (0 = use server default)
}

// --- Internal condition builders ---

func (q *ImmutableQueryBuilder[T]) where(column string, value any) *ImmutableQueryBuilder[T] {
	q.conditions = append(q.conditions, condition{column: column, op: "=", value: value})
	return q
}

func (q *ImmutableQueryBuilder[T]) whereOp(column, op string, value any) *ImmutableQueryBuilder[T] {
	q.conditions = append(q.conditions, condition{column: column, op: op, value: value})
	return q
}

func (q *ImmutableQueryBuilder[T]) whereIn(column string, values any) *ImmutableQueryBuilder[T] {
	q.conditions = append(q.conditions, condition{column: column, op: "IN", value: values})
	return q
}

func (q *ImmutableQueryBuilder[T]) whereNotIn(column string, values any) *ImmutableQueryBuilder[T] {
	q.conditions = append(q.conditions, condition{column: column, op: "NOT IN", value: values})
	return q
}

func (q *ImmutableQueryBuilder[T]) OrderBy(column, direction string) *ImmutableQueryBuilder[T] {
	q.orderBy = append(q.orderBy, column+" "+direction)
	return q
}

func (q *ImmutableQueryBuilder[T]) Limit(n int) *ImmutableQueryBuilder[T] {
	q.limit = n
	return q
}

func (q *ImmutableQueryBuilder[T]) Offset(n int) *ImmutableQueryBuilder[T] {
	q.offset = n
	return q
}

func (q *ImmutableQueryBuilder[T]) AnyOwner() *ImmutableQueryBuilder[T] {
	return q
}

func (q *ImmutableQueryBuilder[T]) addSelect(col string) {
	q.selectedCols = append(q.selectedCols, col)
}

func (q *ImmutableQueryBuilder[T]) setVisibility(v visibilityMode) {
	q.visibility = v
}

func (q *ImmutableQueryBuilder[T]) EagerLoad(relation string) *ImmutableQueryBuilder[T] {
	q.eagerLoads = append(q.eagerLoads, relation)
	return q
}

func (q *ImmutableQueryBuilder[T]) db() dbExecutor {
	if q.tx != nil {
		return q.tx
	}
	if q.connection != "" {
		if conn, ok := Connections[q.connection]; ok {
			return conn
		}
	}
	return DB
}

// setTx associates this query builder with a transaction.
func (q *ImmutableQueryBuilder[T]) setTx(tx *sql.Tx) {
	q.tx = tx
}

// Lock adds FOR UPDATE to the query. Must be used inside a Transaction.
func (q *ImmutableQueryBuilder[T]) Lock() *ImmutableQueryBuilder[T] {
	q.lockMode = "FOR UPDATE"
	return q
}

// LockForUpdate is an alias for Lock().
func (q *ImmutableQueryBuilder[T]) LockForUpdate() *ImmutableQueryBuilder[T] {
	return q.Lock()
}

// LockForShare adds FOR SHARE to the query.
func (q *ImmutableQueryBuilder[T]) LockForShare() *ImmutableQueryBuilder[T] {
	q.lockMode = "FOR SHARE"
	return q
}

// SkipLocked adds SKIP LOCKED to the lock clause.
func (q *ImmutableQueryBuilder[T]) SkipLocked() *ImmutableQueryBuilder[T] {
	q.lockOpt = "SKIP LOCKED"
	return q
}

// NoWait adds NOWAIT to the lock clause.
func (q *ImmutableQueryBuilder[T]) NoWait() *ImmutableQueryBuilder[T] {
	q.lockOpt = "NOWAIT"
	return q
}

// Timeout sets a per-query lock timeout.
func (q *ImmutableQueryBuilder[T]) Timeout(d time.Duration) *ImmutableQueryBuilder[T] {
	q.lockTimeout = d
	return q
}

func (q *ImmutableQueryBuilder[T]) checkLockRequiresTransaction() error {
	if q.lockMode != "" && q.tx == nil {
		return &LockOutsideTransactionError{Table: q.table}
	}
	return nil
}

func (q *ImmutableQueryBuilder[T]) applyLockTimeout() error {
	if q.lockTimeout > 0 && q.tx != nil {
		ms := q.lockTimeout.Milliseconds()
		_, err := q.tx.Exec(fmt.Sprintf("SET LOCAL lock_timeout = '%dms'", ms))
		return err
	}
	return nil
}

// --- Version control ---

// AllVersions bypasses deduplication and returns the full version history.
func (q *ImmutableQueryBuilder[T]) AllVersions() *ImmutableQueryBuilder[T] {
	q.allVersions = true
	return q
}

// --- Terminal methods (all dedup-aware) ---

// First returns the latest version of the first matching record.
func (q *ImmutableQueryBuilder[T]) First() (*T, error) {
	if err := q.checkLockRequiresTransaction(); err != nil {
		return nil, err
	}
	if err := q.applyLockTimeout(); err != nil {
		return nil, mapLockError(q.table, err)
	}

	query, args := q.buildSelect(1)
	row := q.db().QueryRow(query, args...)
	var result T
	if err := scanRow(row, &result); err != nil {
		return nil, mapLockError(q.table, err)
	}
	return &result, nil
}

// All returns the latest version of all matching records.
func (q *ImmutableQueryBuilder[T]) All() ([]T, error) {
	if err := q.checkLockRequiresTransaction(); err != nil {
		return nil, err
	}
	if err := q.applyLockTimeout(); err != nil {
		return nil, mapLockError(q.table, err)
	}

	query, args := q.buildSelect(0)
	rows, err := q.db().Query(query, args...)
	if err != nil {
		return nil, mapLockError(q.table, err)
	}
	defer rows.Close()
	return scanRows[T](rows)
}

// Count returns the number of distinct records matching conditions.
func (q *ImmutableQueryBuilder[T]) Count() (int64, error) {
	query, args := q.buildCount()
	var count int64
	err := q.db().QueryRow(query, args...).Scan(&count)
	return count, err
}

// aggregate runs a SQL aggregate function on the latest version of each record.
func (q *ImmutableQueryBuilder[T]) aggregate(fn, column string) (*float64, error) {
	query, args := q.buildAggregate(fn, column)
	var result *float64
	err := q.db().QueryRow(query, args...).Scan(&result)
	return result, err
}

// Create inserts a new record.
func (q *ImmutableQueryBuilder[T]) Create(record *T) error {
	query, args := buildInsert(q.table, record)
	cols := dbColumns(record)
	query += " RETURNING " + strings.Join(cols, ", ")
	row := q.db().QueryRow(query, args...)
	return row.Scan(dbScanDest(record)...)
}

// --- SQL builders ---

func (q *ImmutableQueryBuilder[T]) cols() []string {
	if len(q.selectedCols) > 0 {
		return q.selectedCols
	}
	var zero T
	return dbColumns(&zero)
}

func (q *ImmutableQueryBuilder[T]) buildSelect(limit int) (string, []any) {
	cols := q.cols()
	prefixed := make([]string, len(cols))
	for i, c := range cols {
		prefixed[i] = "t." + c
	}

	var b strings.Builder
	args := make([]any, 0, len(q.conditions))
	argIdx := 1

	if q.allVersions {
		// Bypass dedup — return all version rows
		b.WriteString("SELECT ")
		b.WriteString(strings.Join(prefixed, ", "))
		b.WriteString(" FROM ")
		b.WriteString(q.table)
		b.WriteString(" t")
		if len(q.conditions) > 0 {
			b.WriteString(" WHERE ")
			for i, c := range q.conditions {
				if i > 0 {
					b.WriteString(" AND ")
				}
				b.WriteString(fmt.Sprintf("t.%s %s $%d", c.column, c.op, argIdx))
				args = append(args, c.value)
				argIdx++
			}
		}
		b.WriteString(" ORDER BY t.id, t.version_id ASC")
	} else {
		// Dedup to latest version per id
		b.WriteString("SELECT ")
		b.WriteString(strings.Join(prefixed, ", "))
		b.WriteString(" FROM ")
		b.WriteString(q.table)
		b.WriteString(" t WHERE t.version_id = (SELECT MAX(version_id) FROM ")
		b.WriteString(q.table)
		b.WriteString(" WHERE id = t.id")
		if q.softDeletes {
			b.WriteString(" AND deleted_at IS NULL")
		}
		b.WriteString(")")

		var extra []string
		for _, c := range q.conditions {
			extra = append(extra, fmt.Sprintf("t.%s %s $%d", c.column, c.op, argIdx))
			args = append(args, c.value)
			argIdx++
		}
		if q.softDeletes {
			extra = append(extra, "t.deleted_at IS NULL")
		}
		if len(extra) > 0 {
			b.WriteString(" AND ")
			b.WriteString(strings.Join(extra, " AND "))
		}
		if len(q.orderBy) > 0 {
			b.WriteString(" ORDER BY ")
			b.WriteString(strings.Join(q.orderBy, ", "))
		} else {
			b.WriteString(" ORDER BY t.id")
		}
	}

	if limit > 0 {
		b.WriteString(fmt.Sprintf(" LIMIT %d", limit))
	} else if q.limit > 0 {
		b.WriteString(fmt.Sprintf(" LIMIT %d", q.limit))
	}
	if q.offset > 0 {
		b.WriteString(fmt.Sprintf(" OFFSET %d", q.offset))
	}

	if q.lockMode != "" {
		b.WriteString(" ")
		b.WriteString(q.lockMode)
		if q.lockOpt != "" {
			b.WriteString(" ")
			b.WriteString(q.lockOpt)
		}
	}

	return b.String(), args
}

func (q *ImmutableQueryBuilder[T]) buildCount() (string, []any) {
	var b strings.Builder
	args := make([]any, 0, len(q.conditions))
	argIdx := 1

	b.WriteString("SELECT COUNT(*) FROM (SELECT t.id FROM ")
	b.WriteString(q.table)
	b.WriteString(" t WHERE t.version_id = (SELECT MAX(version_id) FROM ")
	b.WriteString(q.table)
	b.WriteString(" WHERE id = t.id")
	if q.softDeletes {
		b.WriteString(" AND deleted_at IS NULL")
	}
	b.WriteString(")")

	var extra []string
	for _, c := range q.conditions {
		extra = append(extra, fmt.Sprintf("t.%s %s $%d", c.column, c.op, argIdx))
		args = append(args, c.value)
		argIdx++
	}
	if q.softDeletes {
		extra = append(extra, "t.deleted_at IS NULL")
	}
	if len(extra) > 0 {
		b.WriteString(" AND ")
		b.WriteString(strings.Join(extra, " AND "))
	}
	b.WriteString(") AS _dedup")

	return b.String(), args
}

func (q *ImmutableQueryBuilder[T]) buildAggregate(fn, column string) (string, []any) {
	var b strings.Builder
	args := make([]any, 0, len(q.conditions))
	argIdx := 1

	b.WriteString(fmt.Sprintf("SELECT %s(_dedup.%s) FROM (SELECT t.%s FROM ", fn, column, column))
	b.WriteString(q.table)
	b.WriteString(" t WHERE t.version_id = (SELECT MAX(version_id) FROM ")
	b.WriteString(q.table)
	b.WriteString(" WHERE id = t.id")
	if q.softDeletes {
		b.WriteString(" AND deleted_at IS NULL")
	}
	b.WriteString(")")

	var extra []string
	for _, c := range q.conditions {
		extra = append(extra, fmt.Sprintf("t.%s %s $%d", c.column, c.op, argIdx))
		args = append(args, c.value)
		argIdx++
	}
	if q.softDeletes {
		extra = append(extra, "t.deleted_at IS NULL")
	}
	if len(extra) > 0 {
		b.WriteString(" AND ")
		b.WriteString(strings.Join(extra, " AND "))
	}
	b.WriteString(") AS _dedup")

	return b.String(), args
}

func (q *ImmutableQueryBuilder[T]) appendWhere(b *strings.Builder) []any {
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
