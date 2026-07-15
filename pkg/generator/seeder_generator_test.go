package generator

import (
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
)

func TestGenerateSeederGlue(t *testing.T) {
	source, err := GenerateSeederGlue("seeders", "example.com/crm/database/migrations", []SeederDefinition{{Name: "CRMSeeder", Kind: "scenario"}, {Name: "UserSeeder", Kind: "row"}}, []*schema.Table{{
		Name:                 "users",
		CompositePrimaryKeys: []string{"tenant_id", "id"},
		Columns:              []*schema.Column{{Name: "id", Type: schema.BigInteger, IsPrimaryKey: true, IsUnique: true}, {Name: "first_name", Type: schema.String, Seeder: &schema.SeedSpec{Kind: "first_name"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, expected := range []string{"CRMSeeder", "func Resolve", "func Graph", "policyScenario", "func ResolveValue", `(&UserSeeder{}).Seed(&context)`, "func Tables", `CompositePrimaryKeys: []string{"tenant_id", "id"}`, "IsPrimaryKey: true", "IsUnique: true", `Kind: "first_name"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("generated glue missing %q:\n%s", expected, text)
		}
	}
}
