package generator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
