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
type ContactStatus string
type ContactSeed struct{ Status ContactStatus ` + "`seed:\"status\"`" + ` }
func (ContactSeeder) Table() string { return "crm_contacts" }
func (ContactSeeder) Seed(ctx *SeedValueContext) ContactSeed { return ContactSeed{} }
type RegionSeeder struct{}
func (RegionSeeder) Seed(ctx *SeedValueContext) string { return "west" }
`
	if err := os.WriteFile(filepath.Join(dir, "crm.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	definitions, err := ScanSeeders(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 3 {
		t.Fatalf("definitions = %#v", definitions)
	}
	if definitions[0].Name != "CRMSeeder" || definitions[0].Kind != "scenario" {
		t.Fatalf("scenario = %#v", definitions[0])
	}
	if definitions[0].Policy != "Upsert" {
		t.Fatalf("policy = %q", definitions[0].Policy)
	}
	if len(definitions[0].GraphCalls) != 4 {
		t.Fatalf("safe graph calls = %#v", definitions[0].GraphCalls)
	}
	for _, call := range definitions[0].GraphCalls {
		if call.Method == "With" && (len(call.Arguments) != 1 || call.Arguments[0] != `"password"`) {
			t.Fatalf("With value was not redacted: %#v", call)
		}
	}
	if definitions[1].Name != "ContactSeeder" || definitions[1].Kind != "row" || definitions[1].Table != "crm_contacts" || definitions[1].ReturnType != "ContactSeed" {
		t.Fatalf("row = %#v", definitions[1])
	}
	if len(definitions[1].Fields) != 1 || definitions[1].Fields[0].Name != "status" || definitions[1].Fields[0].Underlying != "string" {
		t.Fatalf("typed fields = %#v", definitions[1].Fields)
	}
	if definitions[2].Kind != "value" || definitions[2].ReturnType != "string" {
		t.Fatalf("value = %#v", definitions[2])
	}
}
