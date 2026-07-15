//go:build cgo

package schema

import (
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
		{Name: "id", Type: BigInteger}, {Name: "first_name", Type: String}, {Name: "last_name", Type: String},
		{Name: "email", Type: String},
		{Name: "password_hash", Type: String, Seeder: &SeedSpec{Kind: "password", Fields: []string{"first_name", "last_name", "id"}}},
	}}
	contacts := &Table{Name: "contacts", Columns: []*Column{
		{Name: "id", Type: BigInteger},
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
}
