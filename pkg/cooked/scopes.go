//go:build pickle_template

package cooked

import "time"

// pickle:scope all
func (q *QueryBuilder[T]) Where__Column__(val __type__) *QueryBuilder[T] {
	return q.Where("__column__", val)
}

// pickle:scope all
func (q *QueryBuilder[T]) Where__Column__Not(val __type__) *QueryBuilder[T] {
	return q.WhereOp("__column__", "!=", val)
}

// pickle:scope all
func (q *QueryBuilder[T]) Where__Column__In(vals []__type__) *QueryBuilder[T] {
	return q.WhereIn("__column__", vals)
}

// pickle:scope all
func (q *QueryBuilder[T]) Where__Column__NotIn(vals []__type__) *QueryBuilder[T] {
	return q.WhereNotIn("__column__", vals)
}

// pickle:scope string
func (q *QueryBuilder[T]) Where__Column__Like(val string) *QueryBuilder[T] {
	return q.WhereOp("__column__", "LIKE", val)
}

// pickle:scope string
func (q *QueryBuilder[T]) Where__Column__NotLike(val string) *QueryBuilder[T] {
	return q.WhereOp("__column__", "NOT LIKE", val)
}

// pickle:scope numeric
func (q *QueryBuilder[T]) Where__Column__GT(val __type__) *QueryBuilder[T] {
	return q.WhereOp("__column__", ">", val)
}

// pickle:scope numeric
func (q *QueryBuilder[T]) Where__Column__GTE(val __type__) *QueryBuilder[T] {
	return q.WhereOp("__column__", ">=", val)
}

// pickle:scope numeric
func (q *QueryBuilder[T]) Where__Column__LT(val __type__) *QueryBuilder[T] {
	return q.WhereOp("__column__", "<", val)
}

// pickle:scope numeric
func (q *QueryBuilder[T]) Where__Column__LTE(val __type__) *QueryBuilder[T] {
	return q.WhereOp("__column__", "<=", val)
}

// pickle:scope timestamp
func (q *QueryBuilder[T]) Where__Column__Before(val time.Time) *QueryBuilder[T] {
	return q.WhereOp("__column__", "<", val)
}

// pickle:scope timestamp
func (q *QueryBuilder[T]) Where__Column__After(val time.Time) *QueryBuilder[T] {
	return q.WhereOp("__column__", ">", val)
}

// pickle:scope timestamp
func (q *QueryBuilder[T]) Where__Column__Between(start, end time.Time) *QueryBuilder[T] {
	return q.WhereOp("__column__", ">=", start).WhereOp("__column__", "<=", end)
}

// pickle:end
