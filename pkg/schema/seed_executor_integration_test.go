//go:build cgo

package schema

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

func TestSeedExecutorSQLiteEndToEnd(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE users (id INTEGER PRIMARY KEY, first_name TEXT NOT NULL, last_name TEXT NOT NULL, email TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL)`,
		`CREATE TABLE contacts (id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL, label TEXT NOT NULL, FOREIGN KEY (user_id) REFERENCES users(id))`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("schema %q: %v", statement, err)
		}
	}

	users := &Table{Name: "users", Columns: []*Column{
		{Name: "id", Type: BigInteger, IsPrimaryKey: true, HasDefault: true}, {Name: "first_name", Type: String}, {Name: "last_name", Type: String},
		{Name: "email", Type: String},
		{Name: "password_hash", Type: String, Seeder: &SeedSpec{Kind: "password", Fields: []string{"first_name", "last_name", "id"}}},
	}}
	contacts := &Table{Name: "contacts", Columns: []*Column{
		{Name: "id", Type: BigInteger, IsPrimaryKey: true, HasDefault: true},
		{Name: "user_id", Type: BigInteger, ForeignKeyTable: "users", ForeignKeyColumn: "id"},
		{Name: "label", Type: String},
	}}
	graph := &SeedGraph{Nodes: []SeedNode{
		{ID: 1, Seeder: NewRowSeederRef("UserSeeder", "users"), Count: FixedCount(1), Values: map[string]any{}, UniqueColumns: []string{"id"}, UpdateColumns: []string{"first_name", "last_name", "email"}},
		{ID: 2, Seeder: NewRowSeederRef("ContactSeeder", "contacts"), Count: FixedCount(2), ParentNodeID: 1, Values: map[string]any{}, UniqueColumns: []string{"id"}, UpdateColumns: []string{"label"}},
	}}

	version := 1
	resolver := func(name string, ctx SeedValueContext) (any, bool, error) {
		switch name {
		case "UserSeeder":
			first := "Ada"
			if version == 2 {
				first = "Augusta"
			}
			return map[string]any{"id": int64(1), "first_name": first, "last_name": "Lovelace", "email": "ada@example.test"}, true, nil
		case "ContactSeeder":
			return map[string]any{"id": int64(10 + ctx.RowOrdinal), "label": fmt.Sprintf("v%d-contact-%d", version, ctx.RowOrdinal)}, true, nil
		default:
			return nil, false, nil
		}
	}
	hasher := func(value string) (string, error) {
		hash, err := bcrypt.GenerateFromPassword([]byte(value), bcrypt.MinCost)
		return string(hash), err
	}
	options := SeedExecutionOptions{Scenario: "CRMSeeder", RootSeed: 8675309, Environment: "test", Driver: "sqlite", Policy: Upsert, SeederResolver: resolver, PasswordHasher: hasher}
	executor := SeedExecutor{DB: db, Tables: []*Table{users, contacts}}
	result, err := executor.Run(context.Background(), graph, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 3 {
		t.Fatalf("inserted plan rows = %d", len(result.Rows))
	}

	var firstName, passwordHash string
	if err := db.QueryRow(`SELECT first_name, password_hash FROM users WHERE id = 1`).Scan(&firstName, &passwordHash); err != nil {
		t.Fatal(err)
	}
	if firstName != "Ada" {
		t.Fatalf("first_name = %q", firstName)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte("ada-lovelace-1")); err != nil {
		t.Fatalf("seed password was not hashed from composite: %v", err)
	}
	var contactCount, linkedCount int
	if err := db.QueryRow(`SELECT COUNT(*), SUM(CASE WHEN user_id = 1 THEN 1 ELSE 0 END) FROM contacts`).Scan(&contactCount, &linkedCount); err != nil {
		t.Fatal(err)
	}
	if contactCount != 2 || linkedCount != 2 {
		t.Fatalf("contacts = %d linked = %d", contactCount, linkedCount)
	}

	version = 2
	if _, err := executor.Run(context.Background(), graph, options); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT first_name FROM users WHERE id = 1`).Scan(&firstName); err != nil {
		t.Fatal(err)
	}
	if firstName != "Augusta" {
		t.Fatalf("upserted first_name = %q", firstName)
	}
	var label string
	if err := db.QueryRow(`SELECT label FROM contacts WHERE id = 10`).Scan(&label); err != nil {
		t.Fatal(err)
	}
	if label != "v2-contact-0" {
		t.Fatalf("upserted label = %q", label)
	}

	rollbackGraph := &SeedGraph{Nodes: []SeedNode{
		{ID: 1, Seeder: NewRowSeederRef("RollbackUserSeeder", "users"), Count: FixedCount(1), Values: map[string]any{}},
		{ID: 2, Seeder: NewRowSeederRef("RollbackContactSeeder", "contacts"), Count: FixedCount(2), ParentNodeID: 1, Values: map[string]any{}},
	}}
	rollbackResolver := func(name string, _ SeedValueContext) (any, bool, error) {
		switch name {
		case "RollbackUserSeeder":
			return map[string]any{"id": int64(2), "first_name": "Grace", "last_name": "Hopper", "email": "grace@example.test"}, true, nil
		case "RollbackContactSeeder":
			return map[string]any{"id": int64(20), "label": "duplicate"}, true, nil
		default:
			return nil, false, nil
		}
	}
	rollbackOptions := SeedExecutionOptions{Scenario: "RollbackSeeder", RootSeed: 1, Environment: "test", Driver: "sqlite", SeederResolver: rollbackResolver, PasswordHasher: hasher}
	if _, err := executor.Run(context.Background(), rollbackGraph, rollbackOptions); err == nil {
		t.Fatal("expected duplicate contact failure")
	}
	var rolledBackUsers, rolledBackContacts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE id = 2`).Scan(&rolledBackUsers); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM contacts WHERE id = 20`).Scan(&rolledBackContacts); err != nil {
		t.Fatal(err)
	}
	if rolledBackUsers != 0 || rolledBackContacts != 0 {
		t.Fatalf("partial rollback rows: users=%d contacts=%d", rolledBackUsers, rolledBackContacts)
	}

	generatedGraph := &SeedGraph{Nodes: []SeedNode{
		{ID: 1, Seeder: NewRowSeederRef("GeneratedUserSeeder", "users"), Count: FixedCount(1), Values: map[string]any{}},
		{ID: 2, Seeder: NewRowSeederRef("GeneratedContactSeeder", "contacts"), Count: FixedCount(1), ParentNodeID: 1, Values: map[string]any{}},
	}}
	generatedResolver := func(name string, _ SeedValueContext) (any, bool, error) {
		switch name {
		case "GeneratedUserSeeder":
			return map[string]any{"first_name": "Margaret", "last_name": "Hamilton", "email": "margaret@example.test"}, true, nil
		case "GeneratedContactSeeder":
			return map[string]any{"label": "generated identity"}, true, nil
		default:
			return nil, false, nil
		}
	}
	generatedOptions := SeedExecutionOptions{Scenario: "GeneratedIdentitySeeder", RootSeed: 99, Environment: "test", Driver: "sqlite", SeederResolver: generatedResolver, PasswordHasher: hasher}
	if _, err := executor.Run(context.Background(), generatedGraph, generatedOptions); err != nil {
		t.Fatal(err)
	}
	var generatedUserID, generatedContactUserID int64
	if err := db.QueryRow(`SELECT u.id, c.user_id FROM users u JOIN contacts c ON c.user_id = u.id WHERE u.email = 'margaret@example.test'`).Scan(&generatedUserID, &generatedContactUserID); err != nil {
		t.Fatal(err)
	}
	if generatedUserID == 0 || generatedContactUserID != generatedUserID {
		t.Fatalf("database rows did not share generated identity: user=%d contact=%d", generatedUserID, generatedContactUserID)
	}
}

func TestSeedExecutorSQLiteDerivesAppendOnlyIntegrityAndRepeats(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE events (id TEXT PRIMARY KEY, row_hash BLOB NOT NULL, prev_hash BLOB NOT NULL, body TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	table := &Table{Name: "events", IsAppendOnly: true, Columns: []*Column{{Name: "id", Type: UUID, IsPrimaryKey: true}, {Name: "row_hash", Type: Binary}, {Name: "prev_hash", Type: Binary}, {Name: "body", Type: String}}}
	graph := &SeedGraph{Nodes: []SeedNode{{ID: 1, Seeder: NewRowSeederRef("EventSeeder", "events"), Count: FixedCount(2), Values: map[string]any{}, UniqueColumns: []string{"id"}}}}
	resolver := func(_ string, ctx SeedValueContext) (any, bool, error) {
		return map[string]any{"body": fmt.Sprintf("event-%d", ctx.RowOrdinal)}, true, nil
	}
	options := SeedExecutionOptions{Scenario: "Audit", RootSeed: 8675309, Environment: "test", Driver: "sqlite", Policy: InsertOrIgnore, SeederResolver: resolver}
	executor := SeedExecutor{DB: db, Tables: []*Table{table}}
	if _, err := executor.Run(context.Background(), graph, options); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT id,row_hash,prev_hash,body FROM events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	prev := make([]byte, 32)
	count := 0
	for rows.Next() {
		var id, body string
		var rowHash, prevHash []byte
		if err := rows.Scan(&id, &rowHash, &prevHash, &body); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(prevHash, prev) {
			t.Fatalf("row %d predecessor mismatch", count)
		}
		expected := computeSeedRowHash(prev, map[string]any{"id": id, "body": body}, table.Columns)
		if !bytes.Equal(rowHash, expected) {
			t.Fatalf("row %d hash mismatch", count)
		}
		prev = rowHash
		count++
	}
	if count != 2 {
		t.Fatalf("rows = %d", count)
	}
	if _, err := executor.Run(context.Background(), graph, options); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("repeat rows = %d", count)
	}
}

func TestSeedExecutorSQLiteImmutableVersionsAndRollback(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE documents (id TEXT NOT NULL, version_id TEXT PRIMARY KEY, row_hash BLOB NOT NULL, prev_hash BLOB NOT NULL, body TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	table := &Table{Name: "documents", IsImmutable: true, Columns: []*Column{{Name: "id", Type: UUID}, {Name: "version_id", Type: UUID, IsPrimaryKey: true}, {Name: "row_hash", Type: Binary}, {Name: "prev_hash", Type: Binary}, {Name: "body", Type: String}}}
	graph := &SeedGraph{Nodes: []SeedNode{{ID: 1, Seeder: NewRowSeederRef("DocumentSeeder", "documents"), Count: FixedCount(2), UniqueColumns: []string{"version_id"}}}}
	resolver := func(_ string, ctx SeedValueContext) (any, bool, error) {
		return map[string]any{"id": "018cc251-f400-7000-8000-000000000001", "body": fmt.Sprintf("version-%d", ctx.RowOrdinal)}, true, nil
	}
	executor := SeedExecutor{DB: db, Tables: []*Table{table}}
	options := SeedExecutionOptions{Scenario: "Documents", RootSeed: 42, Environment: "test", Driver: "sqlite", Policy: InsertOrIgnore, SeederResolver: resolver}
	if _, err := executor.Run(context.Background(), graph, options); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT id,version_id,row_hash,prev_hash,body FROM documents ORDER BY id,version_id`)
	if err != nil {
		t.Fatal(err)
	}
	prev := make([]byte, 32)
	count := 0
	for rows.Next() {
		var id, versionID, body string
		var rowHash, prevHash []byte
		if err := rows.Scan(&id, &versionID, &rowHash, &prevHash, &body); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(prevHash, prev) {
			t.Fatalf("version %d predecessor mismatch", count)
		}
		expected := computeSeedRowHash(prev, map[string]any{"id": id, "version_id": versionID, "body": body}, table.Columns)
		if !bytes.Equal(rowHash, expected) {
			t.Fatalf("version %d hash mismatch", count)
		}
		prev = rowHash
		count++
	}
	rows.Close()
	if count != 2 {
		t.Fatalf("versions = %d", count)
	}

	rollbackGraph := &SeedGraph{Nodes: []SeedNode{{ID: 1, Seeder: NewRowSeederRef("RollbackSeeder", "documents"), Count: FixedCount(2), UniqueColumns: []string{"version_id"}}}}
	if _, err := db.Exec(`CREATE TRIGGER reject_second_version BEFORE INSERT ON documents WHEN NEW.body = 'rollback-1' BEGIN SELECT RAISE(FAIL, 'rejected'); END`); err != nil {
		t.Fatal(err)
	}
	rollbackResolver := func(_ string, ctx SeedValueContext) (any, bool, error) {
		return map[string]any{"id": "018cc251-f400-7000-8000-000000000002", "body": fmt.Sprintf("rollback-%d", ctx.RowOrdinal)}, true, nil
	}
	rollbackOptions := options
	rollbackOptions.Scenario = "Rollback"
	rollbackOptions.Policy = InsertOnly
	rollbackOptions.SeederResolver = rollbackResolver
	if _, err := executor.Run(context.Background(), rollbackGraph, rollbackOptions); err == nil {
		t.Fatal("expected second insert to fail")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM documents`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("failed scenario mutated rows: %d", count)
	}
}

func TestSeedExecutorSQLiteReplaceScenarioUsesOwnedProvenance(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE contacts (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	table := &Table{Name: "contacts", Columns: []*Column{{Name: "id", Type: BigInteger, IsPrimaryKey: true}, {Name: "name", Type: String}}}
	graph := &SeedGraph{Nodes: []SeedNode{{ID: 1, Seeder: NewRowSeederRef("ContactSeeder", "contacts"), Count: FixedCount(2), UniqueColumns: []string{"id"}}}}
	version := 1
	resolver := func(_ string, ctx SeedValueContext) (any, bool, error) {
		return map[string]any{"id": int64(version*10 + ctx.RowOrdinal), "name": fmt.Sprintf("v%d-%d", version, ctx.RowOrdinal)}, true, nil
	}
	options := SeedExecutionOptions{Scenario: "CRM", Environment: "test", Driver: "sqlite", Policy: ReplaceScenario, ProvenanceEnabled: true, SeederResolver: resolver}
	executor := SeedExecutor{DB: db, Tables: []*Table{table}}
	if _, err := executor.Run(context.Background(), graph, options); err != nil {
		t.Fatal(err)
	}
	version = 2
	graph.Nodes[0].Count = FixedCount(1)
	if _, err := executor.Run(context.Background(), graph, options); err != nil {
		t.Fatal(err)
	}
	var count int
	var id int64
	if err := db.QueryRow(`SELECT COUNT(*), MIN(id) FROM contacts`).Scan(&count, &id); err != nil {
		t.Fatal(err)
	}
	if count != 1 || id != 20 {
		t.Fatalf("replacement rows=%d id=%d", count, id)
	}
	if _, err := db.Exec(`INSERT INTO contacts (id,name) VALUES (99,'unowned')`); err != nil {
		t.Fatal(err)
	}
	version = 3
	if _, err := executor.Run(context.Background(), graph, options); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM contacts WHERE id = 99`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("replacement deleted a row not owned by the scenario")
	}
}
