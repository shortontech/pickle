package schema

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestParseSeedAnchor(t *testing.T) {
	parsed, err := ParseSeedAnchor("2026-07-18T05:00:00-07:00")
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Format(time.RFC3339); got != "2026-07-18T12:00:00Z" {
		t.Fatalf("normalized anchor = %s", got)
	}
	for _, invalid := range []string{"", "2026-07-18", "2026-07-18T12:00:00", "tomorrow"} {
		if _, err := ParseSeedAnchor(invalid); err == nil {
			t.Errorf("ParseSeedAnchor(%q) succeeded", invalid)
		}
	}
}

func TestSeedAnchorFlagRejectsConflictingValues(t *testing.T) {
	var flag SeedAnchorFlag
	if err := flag.Set("2026-07-18T05:00:00-07:00"); err != nil {
		t.Fatal(err)
	}
	if err := flag.Set("2026-07-18T12:00:00Z"); err != nil {
		t.Fatalf("same normalized anchor rejected: %v", err)
	}
	if err := flag.Set("2026-07-19T12:00:00Z"); err == nil {
		t.Fatal("conflicting anchor accepted")
	}
}

func TestPostgresIntegrityLockUsesTransactionAdvisoryLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(\$1\)`).WithArgs(sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := lockSeedIntegrityTable(context.Background(), tx, "postgres", "events"); err != nil {
		t.Fatal(err)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPlanSeedGraphExpandsRelationshipsAndHashesPasswords(t *testing.T) {
	users := &Table{Name: "users", Columns: []*Column{
		{Name: "id", Type: BigInteger},
		{Name: "first_name", Type: String},
		{Name: "last_name", Type: String},
		{Name: "password_hash", Type: String, Seeder: &SeedSpec{Kind: "password", Fields: []string{"first_name", "last_name", "id"}}},
	}}
	contacts := &Table{Name: "contacts", Columns: []*Column{
		{Name: "id", Type: BigInteger, HasDefault: true},
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
	parents := &Table{Name: "organizations", Columns: []*Column{{Name: "tenant_id", Type: BigInteger}, {Name: "id", Type: BigInteger}}}
	children := &Table{Name: "contacts", Columns: []*Column{{Name: "tenant_id", Type: BigInteger}, {Name: "organization_id", Type: BigInteger}}, ForeignKeys: []*ForeignKey{{
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

func TestImmutableAndAppendOnlySeedRules(t *testing.T) {
	appendOnly := &Table{Name: "events", IsAppendOnly: true, Columns: []*Column{{Name: "id", Type: UUID, IsPrimaryKey: true}, {Name: "row_hash", Type: Binary}, {Name: "prev_hash", Type: Binary}, {Name: "body", Type: String}}}
	graph := &SeedGraph{Nodes: []SeedNode{{ID: 1, Seeder: NewRowSeederRef("EventSeeder", "events"), Count: FixedCount(1), Values: map[string]any{"body": "created", "row_hash": []byte("authored")}}}}
	if _, err := PlanSeedGraph(graph, []*Table{appendOnly}, SeedExecutionOptions{Scenario: "Audit"}); err == nil || !strings.Contains(err.Error(), "framework-derived") {
		t.Fatalf("authored hash error = %v", err)
	}
	graph.Nodes[0].Values = map[string]any{"body": "created"}
	rows, err := PlanSeedGraph(graph, []*Table{appendOnly}, SeedExecutionOptions{Scenario: "Audit", RootSeed: 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Values["id"] == "" || !rows[0].IntegrityDerived {
		t.Fatalf("planned integrity row = %#v", rows)
	}
	rows[0].UniqueBy = []string{"id"}
	rows[0].Updates = []string{"body"}
	if err := validateSeedRepeatPolicy(rows, SeedExecutionOptions{Policy: Upsert}); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("append-only upsert error = %v", err)
	}
}

func TestReplaceScenarioRequiresProvenance(t *testing.T) {
	if err := validateSeedRepeatPolicy(nil, SeedExecutionOptions{Policy: ReplaceScenario}); err == nil {
		t.Fatal("expected provenance error")
	}
}

func TestPlanSeedGraphUsesCustomRowAndFieldSeeders(t *testing.T) {
	type userSeed struct {
		ID        int64  `db:"id"`
		FirstName string `json:"first_name"`
	}
	table := &Table{Name: "users", Columns: []*Column{
		{Name: "id", Type: BigInteger},
		{Name: "first_name", Type: String},
		{Name: "role", Type: String, Seeder: &SeedSpec{Kind: "custom", Reference: "RoleSeeder"}},
	}}
	graph := &SeedGraph{Nodes: []SeedNode{{ID: 1, Seeder: NewRowSeederRef("UserSeeder", "users"), Count: FixedCount(1), Values: map[string]any{}}}}
	resolver := func(name string, _ SeedValueContext) (any, bool, error) {
		switch name {
		case "UserSeeder":
			return userSeed{ID: 7, FirstName: "Ada"}, true, nil
		case "RoleSeeder":
			return "admin", true, nil
		default:
			return nil, false, nil
		}
	}
	rows, err := PlanSeedGraph(graph, []*Table{table}, SeedExecutionOptions{Scenario: "CRM", SeederResolver: resolver})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Values["id"] != int64(7) || rows[0].Values["first_name"] != "Ada" || rows[0].Values["role"] != "admin" {
		t.Fatalf("custom seeded row = %#v", rows)
	}
}

func TestCustomFieldSeederMustBeRegistered(t *testing.T) {
	table := &Table{Name: "users", Columns: []*Column{{Name: "role", Type: String, Seeder: &SeedSpec{Kind: "custom", Reference: "RoleSeeder"}}}}
	if _, err := GenerateSeedRow(table, nil, SeedValueContext{}); err == nil {
		t.Fatal("expected missing custom seeder error")
	}
}

func TestPlanSeedGraphPropagatesGeneratedParentIdentity(t *testing.T) {
	parents := &Table{Name: "users", Columns: []*Column{{Name: "id", Type: BigInteger, IsPrimaryKey: true, HasDefault: true}, {Name: "name", Type: String}}}
	children := &Table{Name: "contacts", Columns: []*Column{{Name: "id", Type: BigInteger, IsPrimaryKey: true, HasDefault: true}, {Name: "user_id", Type: BigInteger, ForeignKeyTable: "users", ForeignKeyColumn: "id"}, {Name: "label", Type: String}}}
	graph := &SeedGraph{Nodes: []SeedNode{
		{ID: 1, Seeder: NewRowSeederRef("UserSeeder", "users"), Count: FixedCount(1), Values: map[string]any{"name": "Ada"}},
		{ID: 2, Seeder: NewRowSeederRef("ContactSeeder", "contacts"), Count: FixedCount(1), ParentNodeID: 1, Values: map[string]any{"label": "work"}},
	}}
	rows, err := PlanSeedGraph(graph, []*Table{parents, children}, SeedExecutionOptions{Scenario: "CRM", RootSeed: 42})
	if err != nil {
		t.Fatal(err)
	}
	parentID := rows[0].Values["id"]
	if parentID == nil || rows[1].Values["user_id"] != parentID {
		t.Fatalf("generated identity did not propagate: %#v", rows)
	}
}
