package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGraphQLSafetyFixtureQueries(t *testing.T) {
	queryDir := filepath.Join("..", "..", "testdata", "graphql-safety", "queries")
	cases := []string{
		"allowed.graphql",
		"over_depth.graphql",
		"wide_fields.graphql",
		"repeated_aliases.graphql",
		"huge_first.graphql",
		"huge_in_filter.graphql",
		"relationship_fanout.graphql",
		"unexposed_create.graphql",
		"unexposed_delete.graphql",
		"introspection_disabled.graphql",
		"multi_operation.graphql",
	}
	for _, name := range cases {
		data, err := os.ReadFile(filepath.Join(queryDir, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if len(strings.TrimSpace(string(data))) == 0 {
			t.Fatalf("%s should not be empty", name)
		}
	}
}

func TestGraphQLSafetyFixtureHugeInputs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "graphql-safety", "queries", "huge_in_filter.graphql"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "00000000-0000-0000-0000-"); got <= 100 {
		t.Fatalf("huge_in_filter should exceed maxGraphQLInputListSize; got %d ids", got)
	}

	data, err = os.ReadFile(filepath.Join("..", "..", "testdata", "graphql-safety", "queries", "repeated_aliases.graphql"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), ": users"); got <= 25 {
		t.Fatalf("repeated_aliases should exceed maxQueryAliases; got %d aliases", got)
	}
}
