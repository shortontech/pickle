package generator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
)

func TestRowPolicyConformanceCorpusIsVersionedAndComplete(t *testing.T) {
	type row struct {
		ID, Predicate, Identity, Row string
		Decision                     bool
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "row-policy-conformance", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []row
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if row.ID == "" || row.Predicate == "" {
			t.Fatalf("incomplete case: %#v", row)
		}
		if seen[row.ID] {
			t.Fatalf("duplicate case %s", row.ID)
		}
		seen[row.ID] = true
	}
	for _, predicate := range []string{"allow", "deny", "equal", "not_equal", "and", "or", "not", "exists"} {
		found := false
		for _, row := range rows {
			if row.Predicate == predicate {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing predicate %s", predicate)
		}
	}
}

func TestRowPolicyConformanceMatrixEnumeratesEverySurface(t *testing.T) {
	type matrix struct {
		Version             int               `json:"version"`
		PredicateNodes      []string          `json:"predicate_nodes"`
		OperationPositions  []string          `json:"operation_positions"`
		ExistsPositions     []string          `json:"exists_positions"`
		SubjectCombinations []string          `json:"subject_combinations"`
		IdentityTypes       []string          `json:"identity_types"`
		NullStates          []string          `json:"null_states"`
		PhysicalPlans       []string          `json:"physical_plans"`
		TerminalSurfaces    map[string]string `json:"terminal_surfaces"`
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "row-policy-conformance", "matrix.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got matrix
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Version < 1 {
		t.Fatal("conformance matrix must be versioned")
	}
	checks := []struct {
		name string
		got  []string
		want []string
	}{
		{"predicates", got.PredicateNodes, []string{"allow", "deny", "identity", "column", "equal", "not_equal", "in", "and", "or", "not", "exists"}},
		{"positions", got.OperationPositions, []string{"select", "insert", "update_old", "update_new", "delete"}},
		{"exists positions", got.ExistsPositions, []string{"select", "update_old", "delete"}},
		{"subjects", got.SubjectCombinations, []string{"any", "all"}},
		{"identity types", got.IdentityTypes, []string{"uuid", "string", "strings", "int64", "int64s"}},
		{"null states", got.NullStates, []string{"value", "null", "missing_identity"}},
	}
	for _, check := range checks {
		if !reflect.DeepEqual(check.got, check.want) {
			t.Errorf("%s=%v want %v", check.name, check.got, check.want)
		}
	}
	for _, terminal := range []string{"first", "all", "count", "aggregate", "lock_first", "lock_all", "graphql_relationship_loader", "create", "update", "delete", "immutable_update", "immutable_delete", "append_only_update", "append_only_delete", "restore", "bulk_update", "bulk_delete", "raw_builder"} {
		if got.TerminalSurfaces[terminal] == "" {
			t.Errorf("terminal %s has no enforcement classification", terminal)
		}
	}
}

func TestEveryPredicateLowersInEveryLegalOperationPosition(t *testing.T) {
	identity := schema.Identity("user_id")
	column := schema.PolicyColumn("user_id")
	predicates := map[string]schema.RowPredicate{
		"allow": schema.Allow(), "deny": schema.Deny(),
		"identity": schema.Equal(column, identity),
		"column":   schema.Equal(column, identity), "equal": schema.Equal(column, identity),
		"not_equal": schema.NotEqual(column, identity),
		"and":       schema.And(schema.Equal(column, identity), schema.Allow()),
		"or":        schema.Or(schema.Equal(column, identity), schema.Deny()),
		"not":       schema.Not(schema.Equal(column, identity)),
		"exists":    {Kind: schema.PredicateExists, RelatedTable: "memberships", LocalColumn: "id", ForeignColumn: "message_id", Children: []schema.RowPredicate{schema.Equal(schema.PolicyColumn("user_id"), identity)}},
	}
	positions := []string{"select", "insert", "update_old", "update_new", "delete"}
	for name, predicate := range predicates {
		for _, position := range positions {
			if name == "exists" && (position == "insert" || position == "update_new") {
				continue
			}
			t.Run(name+"_"+position, func(t *testing.T) {
				rule := schema.RowRule{Key: "matrix", Subject: schema.RowSubject{Kind: schema.SubjectPublic}}
				switch position {
				case "select":
					rule.Select = &predicate
				case "insert":
					rule.Insert = &predicate
				case "update_old":
					rule.UpdateOld, rule.UpdateNew = &predicate, ptrPredicate(schema.Allow())
				case "update_new":
					rule.UpdateOld, rule.UpdateNew = ptrPredicate(schema.Allow()), &predicate
				case "delete":
					rule.Delete = &predicate
				}
				operation := position
				if strings.HasPrefix(position, "update_") {
					operation = "update"
				}
				resolved := ResolvedRowPolicy{Protection: schema.RowProtection{Table: "messages", SubjectCombination: schema.AnyOfSubjects, Rules: []schema.RowRule{rule}}, EnforcementClass: "portable", Identities: map[string]schema.PolicyIdentityType{"user_id": schema.PolicyIdentityUUID}, PhysicalPlans: map[string]string{operation: operation}}
				plans, err := LowerPostgresRowPolicies([]ResolvedRowPolicy{resolved})
				if err != nil {
					t.Fatal(err)
				}
				if len(plans) != 1 || len(plans[0].Policies) == 0 {
					t.Fatalf("no PostgreSQL policy for %s/%s: %#v", name, position, plans)
				}
			})
		}
	}
}

func ptrPredicate(predicate schema.RowPredicate) *schema.RowPredicate { return &predicate }
