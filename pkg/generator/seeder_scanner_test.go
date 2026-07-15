package generator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanSeeders(t *testing.T) {
	dir := t.TempDir()
	source := `package seeders
type CRMSeeder struct{}
func (CRMSeeder) Policy() SeedPolicy { return Upsert }
func (CRMSeeder) Seed(graph *SeedGraph) {
    graph.CreateN(ContactSeederRef, 25).UniqueBy("email").Update("name").With("password", "redacted")
}
type ContactSeeder struct{}
type ContactSeed struct{}
func (ContactSeeder) Seed(ctx *SeedValueContext) ContactSeed { return ContactSeed{} }
`
	if err := os.WriteFile(filepath.Join(dir, "crm.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	definitions, err := ScanSeeders(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 2 {
		t.Fatalf("definitions = %#v", definitions)
	}
	if definitions[0].Name != "CRMSeeder" || definitions[0].Kind != "scenario" {
		t.Fatalf("scenario = %#v", definitions[0])
	}
	if definitions[0].Policy != "Upsert" {
		t.Fatalf("policy = %q", definitions[0].Policy)
	}
	if len(definitions[0].GraphCalls) != 3 {
		t.Fatalf("safe graph calls = %#v", definitions[0].GraphCalls)
	}
	for _, call := range definitions[0].GraphCalls {
		if call.Method == "With" {
			t.Fatal("value-bearing With call must be redacted")
		}
	}
	if definitions[1].Name != "ContactSeeder" || definitions[1].Kind != "row" || definitions[1].Table != "contacts" || definitions[1].ReturnType != "ContactSeed" {
		t.Fatalf("row = %#v", definitions[1])
	}
}
