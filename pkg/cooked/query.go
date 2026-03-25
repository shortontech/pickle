package cooked

import (
	"database/sql"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// DB is the package-level database connection. Set during app initialization.
var DB *sql.DB

// Connections holds named database connections for multi-connection support.
// Keyed by connection name from config/database.go.
// Deprecated: Use ManagedConnections and WrapConnection for hot-reloadable connections.
var Connections = map[string]*sql.DB{}

// Query starts a new query for the given model type.
func Query[T any](table string, connection ...string) *QueryBuilder[T] {
	q := &QueryBuilder[T]{table: table}
	if len(connection) > 0 {
		q.connection = connection[0]
	}
	return q
}

// visibilityMode controls which columns a query may return.
type visibilityMode int

const (
	visibilityNone   visibilityMode = iota // no scope set
	visibilityPublic                       // only Public columns
	visibilityOwner                        // Public + OwnerSees columns
	visibilityAll                          // all columns
)

// QueryBuilder is the generic query builder for all models.
type QueryBuilder[T any] struct {
	table        string
	connection   string // named connection ("" = default DB)
	conditions   []condition
	orderBy      []string
	limit        int
	offset       int
	eagerLoads   []string
	selectedCols []string
	visibility   visibilityMode
	tx           *sql.Tx       // transaction connection (nil = use global DB)
	lockMode     string        // "", "FOR UPDATE", "FOR SHARE"
	lockOpt      string        // "", "SKIP LOCKED", "NOWAIT"
	lockTimeout  time.Duration // per-query lock timeout (0 = use server default)
	managedConn  *ManagedConnection // tracked for Release() after query completes
}

type condition struct {
	column string
	op     string
	value  any
}

// where adds a condition to the query.
func (q *QueryBuilder[T]) where(column string, value any) *QueryBuilder[T] {
	q.conditions = append(q.conditions, condition{column: column, op: "=", value: value})
	return q
}

// whereOp adds a condition with a custom operator.
func (q *QueryBuilder[T]) whereOp(column, op string, value any) *QueryBuilder[T] {
	q.conditions = append(q.conditions, condition{column: column, op: op, value: value})
	return q
}

// whereIn adds a column IN (...) condition.
func (q *QueryBuilder[T]) whereIn(column string, values any) *QueryBuilder[T] {
	q.conditions = append(q.conditions, condition{column: column, op: "IN", value: values})
	return q
}

// whereNotIn adds a column NOT IN (...) condition.
func (q *QueryBuilder[T]) whereNotIn(column string, values any) *QueryBuilder[T] {
	q.conditions = append(q.conditions, condition{column: column, op: "NOT IN", value: values})
	return q
}

// OrderBy adds an ORDER BY clause. The column name must be a valid SQL
// identifier (letters, digits, underscores only). Direction must be ASC or DESC.
// Invalid values panic — this is a programming error, not user input.
//
// Prefer the generated typed methods (OrderByEmail, OrderByCreatedAt, etc.)
// which are safe by construction. This method exists for generated code and
// internal use; calling it with unsanitized user input is a bug.
func (q *QueryBuilder[T]) OrderBy(column, direction string) *QueryBuilder[T] {
	if !validSQLIdentifier(column) {
		panic("pickle: OrderBy column must be a valid identifier, got: " + column)
	}
	dir := strings.ToUpper(strings.TrimSpace(direction))
	if dir != "ASC" && dir != "DESC" {
		panic("pickle: OrderBy direction must be ASC or DESC, got: " + direction)
	}
	q.orderBy = append(q.orderBy, column+" "+dir)
	return q
}

// validSQLIdentifier returns true if s is a simple SQL identifier:
// non-empty, starts with a letter or underscore, contains only [a-zA-Z0-9_].
func validSQLIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' {
			continue
		}
		if i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
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

// AnyOwner signals that this query intentionally does not scope by ownership.
// It is a no-op — it exists so that Squeeze recognizes the explicit opt-out.
func (q *QueryBuilder[T]) AnyOwner() *QueryBuilder[T] {
	return q
}

// addSelect adds a column to the explicit select list.
func (q *QueryBuilder[T]) addSelect(col string) {
	q.selectedCols = append(q.selectedCols, col)
}

// setVisibility sets the visibility mode for the query.
func (q *QueryBuilder[T]) setVisibility(v visibilityMode) {
	q.visibility = v
}

// ScopeBuilder is a restricted query builder that exposes filter/sort methods
// but no terminal methods (First, All, Count, Create, Update, Delete).
// Scopes are defined against this type to prevent data access in scope functions.
type ScopeBuilder[T any] struct {
	conditions   []condition
	orderBy      []string
	limit        int
	offset       int
	selectedCols []string
	visibility   visibilityMode
}

// NewScopeBuilder creates a ScopeBuilder from a QueryBuilder's current state.
func NewScopeBuilder[T any](q *QueryBuilder[T]) *ScopeBuilder[T] {
	sb := &ScopeBuilder[T]{
		conditions:   append([]condition{}, q.conditions...),
		orderBy:      append([]string{}, q.orderBy...),
		limit:        q.limit,
		offset:       q.offset,
		selectedCols: append([]string{}, q.selectedCols...),
		visibility:   q.visibility,
	}
	return sb
}

// ApplyScopeBuilder merges a ScopeBuilder's state back into a QueryBuilder.
func ApplyScopeBuilder[T any](q *QueryBuilder[T], sb *ScopeBuilder[T]) *QueryBuilder[T] {
	q.conditions = append(q.conditions, sb.conditions...)
	q.orderBy = append(q.orderBy, sb.orderBy...)
	if sb.limit > 0 {
		q.limit = sb.limit
	}
	if sb.offset > 0 {
		q.offset = sb.offset
	}
	if len(sb.selectedCols) > 0 {
		q.selectedCols = sb.selectedCols
	}
	if sb.visibility != visibilityNone {
		q.visibility = sb.visibility
	}
	return q
}

func (sb *ScopeBuilder[T]) where(column string, value any) *ScopeBuilder[T] {
	sb.conditions = append(sb.conditions, condition{column: column, op: "=", value: value})
	return sb
}

func (sb *ScopeBuilder[T]) whereOp(column, op string, value any) *ScopeBuilder[T] {
	sb.conditions = append(sb.conditions, condition{column: column, op: op, value: value})
	return sb
}

func (sb *ScopeBuilder[T]) whereIn(column string, values any) *ScopeBuilder[T] {
	sb.conditions = append(sb.conditions, condition{column: column, op: "IN", value: values})
	return sb
}

func (sb *ScopeBuilder[T]) whereNotIn(column string, values any) *ScopeBuilder[T] {
	sb.conditions = append(sb.conditions, condition{column: column, op: "NOT IN", value: values})
	return sb
}

// OrderBy adds an ORDER BY clause to the scope builder.
func (sb *ScopeBuilder[T]) OrderBy(column, direction string) *ScopeBuilder[T] {
	if !validSQLIdentifier(column) {
		panic("pickle: OrderBy column must be a valid identifier, got: " + column)
	}
	dir := strings.ToUpper(strings.TrimSpace(direction))
	if dir != "ASC" && dir != "DESC" {
		panic("pickle: OrderBy direction must be ASC or DESC, got: " + direction)
	}
	sb.orderBy = append(sb.orderBy, column+" "+dir)
	return sb
}

// Limit sets the LIMIT clause.
func (sb *ScopeBuilder[T]) Limit(n int) *ScopeBuilder[T] {
	sb.limit = n
	return sb
}

// Offset sets the OFFSET clause.
func (sb *ScopeBuilder[T]) Offset(n int) *ScopeBuilder[T] {
	sb.offset = n
	return sb
}

// addSelect adds a column to the explicit select list.
func (sb *ScopeBuilder[T]) addSelect(col string) {
	sb.selectedCols = append(sb.selectedCols, col)
}

// setVisibility sets the visibility mode.
func (sb *ScopeBuilder[T]) setVisibility(v visibilityMode) {
	sb.visibility = v
}

// db returns the database executor for this query — either the transaction
// connection or the global DB (or a named connection). When a ManagedConnection
// is acquired, call releaseConn() when the query completes.
func (q *QueryBuilder[T]) db() dbExecutor {
	if q.tx != nil {
		return q.tx
	}
	if q.connection != "" {
		// Try ManagedConnections first (hot-reloadable)
		if mc := acquireConnection(q.connection); mc != nil {
			q.managedConn = mc
			return mc.DB
		}
		// Fall back to legacy Connections map
		if conn, ok := Connections[q.connection]; ok {
			return conn
		}
	}
	// Try default managed connection
	if mc := acquireConnection("default"); mc != nil {
		q.managedConn = mc
		return mc.DB
	}
	return DB
}

// releaseConn releases the managed connection acquired by db(), if any.
func (q *QueryBuilder[T]) releaseConn() {
	if q.managedConn != nil {
		q.managedConn.Release()
		q.managedConn = nil
	}
}

// setTx associates this query builder with a transaction.
func (q *QueryBuilder[T]) setTx(tx *sql.Tx) {
	q.tx = tx
}

// Lock adds FOR UPDATE to the query. Must be used inside a Transaction.
func (q *QueryBuilder[T]) Lock() *QueryBuilder[T] {
	q.lockMode = "FOR UPDATE"
	return q
}

// LockForUpdate is an alias for Lock().
func (q *QueryBuilder[T]) LockForUpdate() *QueryBuilder[T] {
	return q.Lock()
}

// LockForShare adds FOR SHARE to the query — blocks writes but allows
// other FOR SHARE reads.
func (q *QueryBuilder[T]) LockForShare() *QueryBuilder[T] {
	q.lockMode = "FOR SHARE"
	return q
}

// SkipLocked adds SKIP LOCKED to the lock clause — skips rows that are
// currently locked by another transaction. Useful for work queue patterns.
func (q *QueryBuilder[T]) SkipLocked() *QueryBuilder[T] {
	q.lockOpt = "SKIP LOCKED"
	return q
}

// NoWait adds NOWAIT to the lock clause — fails immediately instead of
// blocking if the target row is locked.
func (q *QueryBuilder[T]) NoWait() *QueryBuilder[T] {
	q.lockOpt = "NOWAIT"
	return q
}

// Timeout sets a per-query lock timeout. If the lock isn't acquired within
// this duration, a LockTimeoutError is returned.
func (q *QueryBuilder[T]) Timeout(d time.Duration) *QueryBuilder[T] {
	q.lockTimeout = d
	return q
}

// checkLockRequiresTransaction returns an error if lock mode is set but
// we're not inside a transaction.
func (q *QueryBuilder[T]) checkLockRequiresTransaction() error {
	if q.lockMode != "" && q.tx == nil {
		return &LockOutsideTransactionError{Table: q.table}
	}
	return nil
}

// applyLockTimeout executes SET LOCAL lock_timeout if a per-query timeout is configured.
func (q *QueryBuilder[T]) applyLockTimeout() error {
	if q.lockTimeout > 0 && q.tx != nil {
		ms := q.lockTimeout.Milliseconds()
		_, err := q.tx.Exec(fmt.Sprintf("SET LOCAL lock_timeout = '%dms'", ms))
		return err
	}
	return nil
}

// EagerLoad marks a relationship for eager loading.
func (q *QueryBuilder[T]) EagerLoad(relation string) *QueryBuilder[T] {
	q.eagerLoads = append(q.eagerLoads, relation)
	return q
}

// ErrNoVisibilityScope is returned when a fetch method is called without setting a visibility scope.
var ErrNoVisibilityScope = fmt.Errorf("no visibility scope set — call SelectPublic(), SelectOwner(), or SelectAll()")

// First returns the first matching record.
func (q *QueryBuilder[T]) First() (*T, error) {
	if err := q.checkLockRequiresTransaction(); err != nil {
		return nil, err
	}
	if err := q.applyLockTimeout(); err != nil {
		return nil, mapLockError(q.table, err)
	}

	q.limit = 1
	query, args := q.buildSelect()
	db := q.db()
	defer q.releaseConn()
	row := db.QueryRow(query, args...)

	var result T
	if err := scanRow(row, &result); err != nil {
		return nil, mapLockError(q.table, err)
	}
	return &result, nil
}

// All returns all matching records.
func (q *QueryBuilder[T]) All() ([]T, error) {
	if err := q.checkLockRequiresTransaction(); err != nil {
		return nil, err
	}
	if err := q.applyLockTimeout(); err != nil {
		return nil, mapLockError(q.table, err)
	}

	query, args := q.buildSelect()
	db := q.db()
	defer q.releaseConn()
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, mapLockError(q.table, err)
	}
	defer rows.Close()

	return scanRows[T](rows)
}

// Count returns the number of matching records.
func (q *QueryBuilder[T]) Count() (int64, error) {
	query, args := q.buildCount()
	db := q.db()
	defer q.releaseConn()
	var count int64
	err := db.QueryRow(query, args...).Scan(&count)
	return count, err
}

// aggregate runs a SQL aggregate function (SUM, AVG, etc.) on a column.
func (q *QueryBuilder[T]) aggregate(fn, column string) (*float64, error) {
	query, args := q.buildAggregate(fn, column)
	db := q.db()
	defer q.releaseConn()
	var result *float64
	err := db.QueryRow(query, args...).Scan(&result)
	return result, err
}

func (q *QueryBuilder[T]) buildAggregate(fn, column string) (string, []any) {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("SELECT %s(%s) FROM %s", fn, column, q.table))
	args := q.appendWhere(&b)
	return b.String(), args
}

// Create inserts a new record and scans the returned row back into the struct,
// populating DB-generated values (UUIDs, timestamps, defaults).
func (q *QueryBuilder[T]) Create(record *T) error {
	query, args := buildInsert(q.table, record)
	cols := dbColumns(record)
	query += " RETURNING " + strings.Join(cols, ", ")
	db := q.db()
	defer q.releaseConn()
	row := db.QueryRow(query, args...)
	return row.Scan(dbScanDest(record)...)
}

// Update updates an existing record.
func (q *QueryBuilder[T]) Update(record *T) error {
	query, args := buildUpdate(q.table, record, q.conditions)
	db := q.db()
	defer q.releaseConn()
	_, err := db.Exec(query, args...)
	return err
}

// Delete removes matching records.
func (q *QueryBuilder[T]) Delete(record *T) error {
	query, args := q.buildDelete()
	db := q.db()
	defer q.releaseConn()
	_, err := db.Exec(query, args...)
	return err
}

func (q *QueryBuilder[T]) buildSelect() (string, []any) {
	var cols []string
	if len(q.selectedCols) > 0 {
		cols = q.selectedCols
	} else {
		var zero T
		cols = dbColumns(&zero)
	}

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
// Zero-value "id", "created_at", and "updated_at" fields are omitted so that
// database defaults (gen_random_uuid(), NOW(), etc.) fire.
func buildInsert[T any](table string, record *T) (string, []any) {
	rv := reflect.ValueOf(record).Elem()
	rt := rv.Type()

	// Fields where a zero value means "let the DB default handle it"
	dbDefaultFields := map[string]bool{"id": true, "created_at": true, "updated_at": true}

	var cols []string
	var vals []any
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("db")
		if tag == "" || tag == "-" {
			continue
		}
		field := rv.Field(i)
		if dbDefaultFields[tag] && field.IsZero() {
			continue
		}
		cols = append(cols, tag)
		vals = append(vals, field.Interface())
	}

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

// uuidV7Time extracts the millisecond-precision timestamp embedded in a UUID v7.
// UUID v7 stores a 48-bit Unix timestamp in milliseconds in bytes 0–5.
// The id parameter is accepted as [16]byte so that uuid.UUID (which is [16]byte)
// can be passed directly without importing the uuid package here.
func uuidV7Time(id [16]byte) time.Time {
	ms := int64(id[0])<<40 | int64(id[1])<<32 | int64(id[2])<<24 |
		int64(id[3])<<16 | int64(id[4])<<8 | int64(id[5])
	return time.UnixMilli(ms).UTC()
}

// Pagination holds pagination metadata for search results.
type Pagination struct {
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Pages    int   `json:"pages"`
}

// FilterOp represents a filter with an operator: filter[column][op]=value.
type FilterOp struct {
	Column   string
	Operator string
	Value    string
}

// parseQueryFilters returns filter[key]=value pairs from the query string.
func parseQueryFilters(r *http.Request) map[string]string {
	filters := make(map[string]string)
	for key, vals := range r.URL.Query() {
		if !strings.HasPrefix(key, "filter[") || !strings.HasSuffix(key, "]") {
			continue
		}
		inner := key[7 : len(key)-1]
		if strings.Contains(inner, "][") {
			continue
		}
		if len(vals) > 0 {
			filters[inner] = vals[0]
		}
	}
	return filters
}

// parseQueryFilterOps returns filter[key][op]=value triples from the query string.
func parseQueryFilterOps(r *http.Request) []FilterOp {
	var ops []FilterOp
	for key, vals := range r.URL.Query() {
		if !strings.HasPrefix(key, "filter[") || !strings.HasSuffix(key, "]") {
			continue
		}
		inner := key[7 : len(key)-1]
		parts := strings.SplitN(inner, "][", 2)
		if len(parts) != 2 {
			continue
		}
		if len(vals) > 0 {
			ops = append(ops, FilterOp{Column: parts[0], Operator: parts[1], Value: vals[0]})
		}
	}
	return ops
}

// parseQuerySort returns the sort column and direction from ?sort=col or ?sort=-col.
func parseQuerySort(r *http.Request) (column, direction string) {
	s := r.URL.Query().Get("sort")
	if s == "" {
		return "", ""
	}
	if strings.HasPrefix(s, "-") {
		return s[1:], "DESC"
	}
	return s, "ASC"
}

// parseQueryPage returns page number and page size from ?page[number]=N&page[size]=N.
func parseQueryPage(r *http.Request) (page, size int) {
	page = 1
	size = 25
	q := r.URL.Query()
	if v := q.Get("page[number]"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := q.Get("page[size]"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			size = n
			if size > 100 {
				size = 100
			}
		}
	}
	return page, size
}
