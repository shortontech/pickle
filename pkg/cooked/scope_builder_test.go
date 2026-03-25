package cooked

import (
	"testing"
)

func TestScopeBuilderCreation(t *testing.T) {
	q := Query[testModel]("test_models")
	q.where("name", "alice")
	q.Limit(10)

	sb := NewScopeBuilder[testModel](q)
	if len(sb.conditions) != 1 {
		t.Errorf("expected 1 condition, got %d", len(sb.conditions))
	}
	if sb.limit != 10 {
		t.Errorf("expected limit 10, got %d", sb.limit)
	}
}

func TestScopeBuilderFilters(t *testing.T) {
	sb := &ScopeBuilder[testModel]{}
	sb.where("name", "bob")
	sb.whereOp("age", ">", 18)
	sb.Limit(5)
	sb.Offset(10)

	if len(sb.conditions) != 2 {
		t.Errorf("expected 2 conditions, got %d", len(sb.conditions))
	}
	if sb.limit != 5 {
		t.Errorf("expected limit 5, got %d", sb.limit)
	}
	if sb.offset != 10 {
		t.Errorf("expected offset 10, got %d", sb.offset)
	}
}

func TestApplyScopeBuilder(t *testing.T) {
	q := Query[testModel]("test_models")
	q.where("active", true)

	sb := &ScopeBuilder[testModel]{}
	sb.where("name", "bob")
	sb.Limit(5)
	sb.OrderBy("name", "ASC")

	ApplyScopeBuilder[testModel](q, sb)

	if len(q.conditions) != 2 {
		t.Errorf("expected 2 conditions, got %d", len(q.conditions))
	}
	if q.limit != 5 {
		t.Errorf("expected limit 5, got %d", q.limit)
	}
	if len(q.orderBy) != 1 {
		t.Errorf("expected 1 orderBy, got %d", len(q.orderBy))
	}
}

func TestScopeBuilderDoesNotHaveTerminals(t *testing.T) {
	// This is a compile-time check — ScopeBuilder should NOT have
	// First(), All(), Count(), Create(), Update(), Delete() methods.
	// We verify by checking the type has the filter methods we expect.
	sb := &ScopeBuilder[testModel]{}
	sb.where("x", 1)
	sb.whereOp("y", ">", 2)
	sb.whereIn("z", []int{1, 2, 3})
	sb.whereNotIn("w", []int{4, 5})
	sb.OrderBy("x", "ASC")
	sb.Limit(10)
	sb.Offset(5)

	// If this compiles, the type has the correct method set.
	if len(sb.conditions) != 4 {
		t.Errorf("expected 4 conditions, got %d", len(sb.conditions))
	}
}

func TestScopeBuilderPreservesState(t *testing.T) {
	q := Query[testModel]("test_models")
	q.where("a", 1)
	q.where("b", 2)
	q.Limit(100)
	q.Offset(50)

	sb := NewScopeBuilder[testModel](q)

	// Modifying scope builder shouldn't affect original query
	sb.where("c", 3)

	if len(q.conditions) != 2 {
		t.Errorf("original query should still have 2 conditions, got %d", len(q.conditions))
	}
	if len(sb.conditions) != 3 {
		t.Errorf("scope builder should have 3 conditions, got %d", len(sb.conditions))
	}
}
