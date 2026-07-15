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
func (CRMSeeder) Seed(graph *SeedGraph) {}
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
	if definitions[1].Name != "ContactSeeder" || definitions[1].Kind != "row" || definitions[1].Table != "contacts" || definitions[1].ReturnType != "ContactSeed" {
		t.Fatalf("row = %#v", definitions[1])
	}
}
