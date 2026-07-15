package generator

import (
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
)

func TestGenerateSeederGlue(t *testing.T) {
	source, err := GenerateSeederGlue("seeders", "example.com/crm/database/migrations", []SeederDefinition{{Name: "CRMSeeder", Kind: "scenario"}}, []*schema.Table{{
		Name:    "users",
		Columns: []*schema.Column{{Name: "id", Type: schema.BigInteger}, {Name: "first_name", Type: schema.String, Seeder: &schema.SeedSpec{Kind: "first_name"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, expected := range []string{"CRMSeeder", "func Resolve", "func Graph", "policyScenario", "func Tables", `Kind: "first_name"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("generated glue missing %q:\n%s", expected, text)
		}
	}
}
