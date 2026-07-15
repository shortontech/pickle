package schema

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestPlanSeedGraphExpandsRelationshipsAndHashesPasswords(t *testing.T) {
	users := &Table{Name: "users", Columns: []*Column{
		{Name: "id", Type: BigInteger},
		{Name: "first_name", Type: String},
		{Name: "last_name", Type: String},
		{Name: "password_hash", Type: String, Seeder: &SeedSpec{Kind: "password", Fields: []string{"first_name", "last_name", "id"}}},
	}}
	contacts := &Table{Name: "contacts", Columns: []*Column{
		{Name: "id", Type: BigInteger},
		{Name: "user_id", Type: BigInteger, ForeignKeyTable: "users", ForeignKeyColumn: "id"},
		{Name: "email", Type: String, Seeder: &SeedSpec{Kind: "safe_email"}},
	}}
	graph := &SeedGraph{Nodes: []SeedNode{
		{ID: 1, Seeder: NewRowSeederRef("UserSeeder", "users"), Count: FixedCount(1), Values: map[string]any{"id": int64(1), "first_name": "Ada", "last_name": "Lovelace"}},
		{ID: 2, Seeder: NewRowSeederRef("ContactSeeder", "contacts"), Count: FixedCount(2), ParentNodeID: 1, Values: map[string]any{}},
	}}
	rows, err := PlanSeedGraph(graph, []*Table{users, contacts}, SeedExecutionOptions{
		Scenario: "CRMSeeder", RootSeed: 8675309,
		PasswordHasher: func(value string) (string, error) { return "hash:" + value, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("planned %d rows, want 3", len(rows))
	}
	if got := rows[0].Values["password_hash"]; got != "hash:ada-lovelace-1" {
		t.Fatalf("password hash = %q", got)
	}
	if !rows[0].Sensitive["password_hash"] {
		t.Fatal("password column was not marked sensitive")
	}
	for _, row := range rows[1:] {
		if got := row.Values["user_id"]; got != int64(1) {
			t.Fatalf("flowed user_id = %#v", got)
		}
	}
}

func TestPlanSeedGraphFlowsCompositeForeignKey(t *testing.T) {
	parents := &Table{Name: "organizations", Columns: []*Column{{Name: "tenant_id"}, {Name: "id"}}}
	children := &Table{Name: "contacts", Columns: []*Column{{Name: "tenant_id"}, {Name: "organization_id"}}, ForeignKeys: []*ForeignKey{{
		Columns: []string{"tenant_id", "organization_id"}, ReferencedTable: "organizations", ReferencedColumns: []string{"tenant_id", "id"},
	}}}
	graph := &SeedGraph{Nodes: []SeedNode{
		{ID: 1, Seeder: NewRowSeederRef("OrganizationSeeder", "organizations"), Count: FixedCount(1), Values: map[string]any{"tenant_id": int64(9), "id": int64(3)}},
		{ID: 2, Seeder: NewRowSeederRef("ContactSeeder", "contacts"), Count: FixedCount(1), ParentNodeID: 1, Values: map[string]any{}},
	}}
	rows, err := PlanSeedGraph(graph, []*Table{parents, children}, SeedExecutionOptions{Scenario: "CRM", PasswordHasher: func(value string) (string, error) { return value, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if rows[1].Values["tenant_id"] != int64(9) || rows[1].Values["organization_id"] != int64(3) {
		t.Fatalf("composite identity was not flowed: %#v", rows[1].Values)
	}
}

func TestSeedExecutorRollsBackScenario(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	table := &Table{Name: "roles", Columns: []*Column{{Name: "name", Type: String}}}
	graph := &SeedGraph{Nodes: []SeedNode{{ID: 1, Seeder: NewRowSeederRef("RoleSeeder", "roles"), Count: FixedCount(2), Values: map[string]any{"name": "admin"}}}}
	mock.ExpectBegin()
	insert := regexp.QuoteMeta(`INSERT INTO "roles" ("name") VALUES (?)`)
	mock.ExpectExec(insert).WithArgs("admin").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(insert).WithArgs("admin").WillReturnError(errors.New("duplicate"))
	mock.ExpectRollback()
	_, err = (SeedExecutor{DB: db, Tables: []*Table{table}}).Run(context.Background(), graph, SeedExecutionOptions{Scenario: "RoleSeeder", Environment: "test", Driver: "sqlite"})
	if err == nil {
		t.Fatal("expected insertion failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSeedExecutorDryRunDoesNotOpenTransaction(t *testing.T) {
	table := &Table{Name: "roles", Columns: []*Column{{Name: "name", Type: String}}}
	graph := &SeedGraph{Nodes: []SeedNode{{ID: 1, Seeder: NewRowSeederRef("RoleSeeder", "roles"), Count: FixedCount(1), Values: map[string]any{"name": "admin"}}}}
	result, err := (SeedExecutor{Tables: []*Table{table}}).Run(context.Background(), graph, SeedExecutionOptions{Scenario: "RoleSeeder", Environment: "production", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || len(result.Rows) != 1 {
		t.Fatalf("unexpected dry-run result: %#v", result)
	}
}

func TestValidateSeedEnvironment(t *testing.T) {
	if err := ValidateSeedEnvironment("production", false, "", false); err == nil {
		t.Fatal("production mutation should require confirmation")
	}
	if err := ValidateSeedEnvironment("production", true, "staging", false); err == nil {
		t.Fatal("mismatched environment confirmation should fail")
	}
	if err := ValidateSeedEnvironment("production", true, "production", false); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSeedEnvironment("production", false, "", true); err != nil {
		t.Fatal(err)
	}
}

func TestSeedRepeatPoliciesGenerateDriverSQL(t *testing.T) {
	row := SeedPlannedRow{Table: "users", Values: map[string]any{"email": "ada@example.test", "name": "Ada"}, UniqueBy: []string{"email"}, Updates: []string{"name"}}
	tests := []struct {
		name, driver string
		policy       SeedPolicy
		want         string
	}{
		{"postgres ignore", "postgres", InsertOrIgnore, `ON CONFLICT ("email") DO NOTHING`},
		{"sqlite upsert", "sqlite", Upsert, `ON CONFLICT ("email") DO UPDATE SET "name" = excluded."name"`},
		{"mysql ignore", "mysql", InsertOrIgnore, "INSERT IGNORE INTO"},
		{"mysql upsert", "mysql", Upsert, "ON DUPLICATE KEY UPDATE `name` = VALUES(`name`)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, _, err := seedInsertSQL(test.driver, row, SeedExecutionOptions{Policy: test.policy})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(query, test.want) {
				t.Fatalf("query %q missing %q", query, test.want)
			}
		})
	}
}

func TestSeedRepeatPolicyRequiresExplicitIdentity(t *testing.T) {
	rows := []SeedPlannedRow{{Table: "users", Values: map[string]any{"email": "ada@example.test"}}}
	if err := validateSeedRepeatPolicy(rows, SeedExecutionOptions{Policy: InsertOrIgnore}); err == nil {
		t.Fatal("expected missing identity error")
	}
	rows[0].UniqueBy = []string{"email"}
	if err := validateSeedRepeatPolicy(rows, SeedExecutionOptions{Policy: Upsert}); err == nil {
		t.Fatal("expected missing update allowlist error")
	}
}

func TestReplaceScenarioRequiresProvenance(t *testing.T) {
	if err := validateSeedRepeatPolicy(nil, SeedExecutionOptions{Policy: ReplaceScenario}); err == nil {
		t.Fatal("expected provenance error")
	}
}
