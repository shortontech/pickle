package generator

import (
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
)

func TestFilterExposedModels_Basic(t *testing.T) {
	tables := []*schema.Table{
		{Name: "users"},
		{Name: "posts"},
		{Name: "comments"},
	}

	state := DerivedGraphQLState{
		Exposures: []DerivedExposure{
			{Model: "users", Operations: []string{"list", "show"}},
			{Model: "posts", Operations: []string{"list", "show", "create", "update", "delete"}},
		},
	}

	result := FilterExposedModels(tables, state)

	if len(result) != 2 {
		t.Fatalf("expected 2 exposed models, got %d", len(result))
	}

	if result[0].Model != "users" {
		t.Errorf("expected first model to be users, got %s", result[0].Model)
	}
	if len(result[0].Operations) != 2 {
		t.Errorf("expected 2 operations for users, got %d", len(result[0].Operations))
	}
	if result[0].Table.Name != "users" {
		t.Errorf("expected table name users, got %s", result[0].Table.Name)
	}

	if result[1].Model != "posts" {
		t.Errorf("expected second model to be posts, got %s", result[1].Model)
	}
	if len(result[1].Operations) != 5 {
		t.Errorf("expected 5 operations for posts, got %d", len(result[1].Operations))
	}
}

func TestFilterExposedModels_NoMatchingTable(t *testing.T) {
	tables := []*schema.Table{
		{Name: "users"},
	}

	state := DerivedGraphQLState{
		Exposures: []DerivedExposure{
			{Model: "posts", Operations: []string{"list"}},
		},
	}

	result := FilterExposedModels(tables, state)

	if len(result) != 0 {
		t.Fatalf("expected 0 exposed models, got %d", len(result))
	}
}

func TestFilterExposedModels_EmptyOperations(t *testing.T) {
	tables := []*schema.Table{
		{Name: "users"},
	}

	state := DerivedGraphQLState{
		Exposures: []DerivedExposure{
			{Model: "users", Operations: []string{}},
		},
	}

	result := FilterExposedModels(tables, state)

	if len(result) != 0 {
		t.Fatalf("expected 0 exposed models for empty operations, got %d", len(result))
	}
}

func TestFilterExposedModels_EmptyState(t *testing.T) {
	tables := []*schema.Table{
		{Name: "users"},
		{Name: "posts"},
	}

	state := DerivedGraphQLState{}

	result := FilterExposedModels(tables, state)

	if len(result) != 0 {
		t.Fatalf("expected 0 exposed models, got %d", len(result))
	}
}

func TestFilterExposedModels_PreservesOrder(t *testing.T) {
	tables := []*schema.Table{
		{Name: "users"},
		{Name: "posts"},
		{Name: "comments"},
	}

	state := DerivedGraphQLState{
		Exposures: []DerivedExposure{
			{Model: "comments", Operations: []string{"list"}},
			{Model: "users", Operations: []string{"show"}},
		},
	}

	result := FilterExposedModels(tables, state)

	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	// Order follows state.Exposures, not tables
	if result[0].Model != "comments" {
		t.Errorf("expected comments first, got %s", result[0].Model)
	}
	if result[1].Model != "users" {
		t.Errorf("expected users second, got %s", result[1].Model)
	}
}
