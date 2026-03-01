//go:build pickle_template

package cooked

import "time"

// pickle:scope all
func (q *QueryBuilder[T]) Where__Column__(val __type__) *QueryBuilder[T] {
	q.Where("__column__", val)
	return q
}

// pickle:scope all
func (q *QueryBuilder[T]) Where__Column__Not(val __type__) *QueryBuilder[T] {
	q.WhereOp("__column__", "!=", val)
	return q
}

// pickle:scope all
func (q *QueryBuilder[T]) Where__Column__In(vals []__type__) *QueryBuilder[T] {
	q.WhereIn("__column__", vals)
	return q
}

// pickle:scope all
func (q *QueryBuilder[T]) Where__Column__NotIn(vals []__type__) *QueryBuilder[T] {
	q.WhereNotIn("__column__", vals)
	return q
}

// pickle:scope string
func (q *QueryBuilder[T]) Where__Column__Like(val string) *QueryBuilder[T] {
	q.WhereOp("__column__", "LIKE", val)
	return q
}

// pickle:scope string
func (q *QueryBuilder[T]) Where__Column__NotLike(val string) *QueryBuilder[T] {
	q.WhereOp("__column__", "NOT LIKE", val)
	return q
}

// pickle:scope numeric
func (q *QueryBuilder[T]) Where__Column__GT(val __type__) *QueryBuilder[T] {
	q.WhereOp("__column__", ">", val)
	return q
}

// pickle:scope numeric
func (q *QueryBuilder[T]) Where__Column__GTE(val __type__) *QueryBuilder[T] {
	q.WhereOp("__column__", ">=", val)
	return q
}

// pickle:scope numeric
func (q *QueryBuilder[T]) Where__Column__LT(val __type__) *QueryBuilder[T] {
	q.WhereOp("__column__", "<", val)
	return q
}

// pickle:scope numeric
func (q *QueryBuilder[T]) Where__Column__LTE(val __type__) *QueryBuilder[T] {
	q.WhereOp("__column__", "<=", val)
	return q
}

// pickle:scope timestamp
func (q *QueryBuilder[T]) Where__Column__Before(val time.Time) *QueryBuilder[T] {
	q.WhereOp("__column__", "<", val)
	return q
}

// pickle:scope timestamp
func (q *QueryBuilder[T]) Where__Column__After(val time.Time) *QueryBuilder[T] {
	q.WhereOp("__column__", ">", val)
	return q
}

// pickle:scope timestamp
func (q *QueryBuilder[T]) Where__Column__Between(start, end time.Time) *QueryBuilder[T] {
	q.WhereOp("__column__", ">=", start)
	q.WhereOp("__column__", "<=", end)
	return q
}

// pickle:scope table
// FetchResource fetches a single __Model__.
func (q *QueryBuilder[T]) FetchResource(_ string) (any, error) {
	return q.First()
}

// pickle:scope table
// FetchResources fetches all matching __Model__ records.
func (q *QueryBuilder[T]) FetchResources(_ string) (any, error) {
	return q.All()
}

// pickle:scope table_owned
// FetchResource fetches a single __Model__ and serializes it based on ownership.
func (q *QueryBuilder[T]) FetchResource(ownerID string) (any, error) {
	record, err := q.First()
	if err != nil {
		return nil, err
	}
	return Serialize__Model__(record, ownerID), nil
}

// pickle:scope table_owned
// FetchResources fetches all matching __Model__ records and serializes them based on ownership.
func (q *QueryBuilder[T]) FetchResources(ownerID string) (any, error) {
	records, err := q.All()
	if err != nil {
		return nil, err
	}
	return Serialize__Model__s(records, ownerID), nil
}

// pickle:end
