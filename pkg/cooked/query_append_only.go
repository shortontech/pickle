package cooked

import (
	"database/sql"
	"time"
)

// AppendOnlyQuery starts a query whose method set structurally excludes
// Update and Delete. It has the same read and insert implementation as the
// regular query builder, without inheriting its mutation methods.
func AppendOnlyQuery[T any](table string, connection ...string) *AppendOnlyQueryBuilder[T] {
	return (*AppendOnlyQueryBuilder[T])(Query[T](table, connection...))
}

// AppendOnlyQueryBuilder is the query builder for insert-only tables.
// It intentionally uses a distinct defined type: embedding QueryBuilder would
// promote Update and Delete onto generated append-only query wrappers.
type AppendOnlyQueryBuilder[T any] QueryBuilder[T]

func (q *AppendOnlyQueryBuilder[T]) base() *QueryBuilder[T] { return (*QueryBuilder[T])(q) }

func (q *AppendOnlyQueryBuilder[T]) WithPolicyContext(context PolicyContext) *AppendOnlyQueryBuilder[T] {
	q.base().WithPolicyContext(context)
	return q
}
func (q *AppendOnlyQueryBuilder[T]) where(column string, value any) *AppendOnlyQueryBuilder[T] {
	q.base().where(column, value)
	return q
}
func (q *AppendOnlyQueryBuilder[T]) whereOp(column, op string, value any) *AppendOnlyQueryBuilder[T] {
	q.base().whereOp(column, op, value)
	return q
}
func (q *AppendOnlyQueryBuilder[T]) whereIn(column string, values any) *AppendOnlyQueryBuilder[T] {
	q.base().whereIn(column, values)
	return q
}
func (q *AppendOnlyQueryBuilder[T]) whereNotIn(column string, values any) *AppendOnlyQueryBuilder[T] {
	q.base().whereNotIn(column, values)
	return q
}
func (q *AppendOnlyQueryBuilder[T]) OrderBy(column, direction string) *AppendOnlyQueryBuilder[T] {
	q.base().OrderBy(column, direction)
	return q
}
func (q *AppendOnlyQueryBuilder[T]) Limit(n int) *AppendOnlyQueryBuilder[T] {
	q.base().Limit(n)
	return q
}
func (q *AppendOnlyQueryBuilder[T]) Offset(n int) *AppendOnlyQueryBuilder[T] {
	q.base().Offset(n)
	return q
}
func (q *AppendOnlyQueryBuilder[T]) AnyOwner() *AppendOnlyQueryBuilder[T] { return q }
func (q *AppendOnlyQueryBuilder[T]) addSelect(column string)              { q.base().addSelect(column) }
func (q *AppendOnlyQueryBuilder[T]) setVisibility(visibility visibilityMode) {
	q.base().setVisibility(visibility)
}
func (q *AppendOnlyQueryBuilder[T]) db() dbExecutor   { return q.base().db() }
func (q *AppendOnlyQueryBuilder[T]) setTx(tx *sql.Tx) { q.base().setTx(tx) }
func (q *AppendOnlyQueryBuilder[T]) UseTransaction(tx *sql.Tx) *AppendOnlyQueryBuilder[T] {
	q.base().UseTransaction(tx)
	return q
}
func (q *AppendOnlyQueryBuilder[T]) Lock() *AppendOnlyQueryBuilder[T] {
	q.base().Lock()
	return q
}
func (q *AppendOnlyQueryBuilder[T]) LockForUpdate() *AppendOnlyQueryBuilder[T] { return q.Lock() }
func (q *AppendOnlyQueryBuilder[T]) LockForShare() *AppendOnlyQueryBuilder[T] {
	q.base().LockForShare()
	return q
}
func (q *AppendOnlyQueryBuilder[T]) SkipLocked() *AppendOnlyQueryBuilder[T] {
	q.base().SkipLocked()
	return q
}
func (q *AppendOnlyQueryBuilder[T]) NoWait() *AppendOnlyQueryBuilder[T] {
	q.base().NoWait()
	return q
}
func (q *AppendOnlyQueryBuilder[T]) Timeout(timeout time.Duration) *AppendOnlyQueryBuilder[T] {
	q.base().Timeout(timeout)
	return q
}
func (q *AppendOnlyQueryBuilder[T]) EagerLoad(relation string) *AppendOnlyQueryBuilder[T] {
	q.base().EagerLoad(relation)
	return q
}
func (q *AppendOnlyQueryBuilder[T]) First() (*T, error)    { return q.base().First() }
func (q *AppendOnlyQueryBuilder[T]) All() ([]T, error)     { return q.base().All() }
func (q *AppendOnlyQueryBuilder[T]) Count() (int64, error) { return q.base().Count() }
func (q *AppendOnlyQueryBuilder[T]) aggregate(fn, column string) (*float64, error) {
	return q.base().aggregate(fn, column)
}
func (q *AppendOnlyQueryBuilder[T]) Create(record *T) error { return q.base().Create(record) }

func NewAppendOnlyScopeBuilder[T any](q *AppendOnlyQueryBuilder[T]) *ScopeBuilder[T] {
	return NewScopeBuilder(q.base())
}

func ApplyAppendOnlyScopeBuilder[T any](q *AppendOnlyQueryBuilder[T], sb *ScopeBuilder[T]) *AppendOnlyQueryBuilder[T] {
	ApplyScopeBuilder(q.base(), sb)
	return q
}
