package schema

import "testing"

func TestSeedValueStableSubstreams(t *testing.T) {
	ctx := SeedValueContext{RootSeed: 8675309, Scenario: "CRMSeeder", NodePath: "users", RowOrdinal: 0, Column: "first_name"}
	spec := &SeedSpec{Kind: "first_name"}
	first, err := SeedValue(spec, ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SeedValue(spec, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("same seed produced %v then %v", first, second)
	}

	other := ctx
	other.Column = "unrelated"
	if _, err := SeedValue(spec, other); err != nil {
		t.Fatal(err)
	}
	again, _ := SeedValue(spec, ctx)
	if again != first {
		t.Fatalf("unrelated stream reshuffled value: %v != %v", again, first)
	}
}

func TestGenerateSeedRowPasswordComposite(t *testing.T) {
	table := &Table{Name: "users", Columns: []*Column{
		{Name: "id", Type: BigInteger},
		{Name: "first_name", Type: String},
		{Name: "last_name", Type: String},
		{Name: "password_hash", Type: String, Seeder: &SeedSpec{Kind: "password", Fields: []string{"first_name", "last_name", "id"}}},
	}}
	row, err := GenerateSeedRow(table, map[string]any{"id": int64(1), "first_name": "Ada", "last_name": "Lovelace"}, SeedValueContext{RootSeed: 1, Scenario: "CRMSeeder"})
	if err != nil {
		t.Fatal(err)
	}
	if got := row["password_hash"]; got != "ada-lovelace-1" {
		t.Fatalf("password composite = %q", got)
	}
}

func TestGenerateSeedRowRequiresCompositeFields(t *testing.T) {
	table := &Table{Name: "users", Columns: []*Column{{Name: "password_hash", Type: String, Seeder: &SeedSpec{Kind: "password", Fields: []string{"first_name"}}}}}
	if _, err := GenerateSeedRow(table, nil, SeedValueContext{}); err == nil {
		t.Fatal("expected missing composite field error")
	}
}
