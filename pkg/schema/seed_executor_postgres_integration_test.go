package schema

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func TestPostgresSeederIntegrityExistingHeadConcurrentRestrictedWriter(t *testing.T) {
	dsn := os.Getenv("PICKLE_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set PICKLE_POSTGRES_TEST_DSN to run PostgreSQL seeder integrity conformance")
	}
	runtimeDSN, err := url.Parse(dsn)
	if err != nil || runtimeDSN.Scheme == "" {
		t.Skip("PICKLE_POSTGRES_TEST_DSN must be a URL for restricted-role verification")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tableName, role, password := "pickle_seed_chain_"+suffix, "pickle_seed_writer_"+suffix, "seed-test-"+suffix
	quote := func(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
	for _, statement := range []string{
		"CREATE ROLE " + quote(role) + " LOGIN PASSWORD '" + password + "' NOSUPERUSER NOBYPASSRLS",
		"CREATE TABLE " + quote(tableName) + " (id uuid PRIMARY KEY, row_hash bytea NOT NULL, prev_hash bytea NOT NULL, body text NOT NULL)",
		"GRANT SELECT, INSERT ON " + quote(tableName) + " TO " + quote(role),
	} {
		if _, err := admin.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	runtimeDSN.User = url.UserPassword(role, password)
	runtimeDB, err := sql.Open("postgres", runtimeDSN.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		runtimeDB.Close()
		admin.Exec("DROP TABLE IF EXISTS " + quote(tableName) + " CASCADE")
		admin.Exec("DROP ROLE IF EXISTS " + quote(role))
	})
	table := &Table{Name: tableName, IsAppendOnly: true, Columns: []*Column{{Name: "id", Type: UUID, IsPrimaryKey: true}, {Name: "row_hash", Type: Binary}, {Name: "prev_hash", Type: Binary}, {Name: "body", Type: String}}}
	run := func(id, body string) error {
		graph := &SeedGraph{Nodes: []SeedNode{{ID: 1, Seeder: NewRowSeederRef("EventSeeder", tableName), Count: FixedCount(1), Values: map[string]any{"id": id, "body": body}, UniqueColumns: []string{"id"}}}}
		_, err := (SeedExecutor{DB: runtimeDB, Tables: []*Table{table}}).Run(context.Background(), graph, SeedExecutionOptions{Scenario: body, RootSeed: 7, Environment: "test", Driver: "postgres", Policy: InsertOrIgnore})
		return err
	}
	if err := run("018cc251-f400-7000-8000-000000000001", "existing"); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errors := make(chan error, 2)
	for index, id := range []string{"018cc251-f400-7000-8000-000000000002", "018cc251-f400-7000-8000-000000000003"} {
		wait.Add(1)
		go func(index int, id string) {
			defer wait.Done()
			errors <- run(id, fmt.Sprintf("concurrent-%d", index))
		}(index, id)
	}
	wait.Wait()
	close(errors)
	succeeded := 0
	for err := range errors {
		if err == nil {
			succeeded++
		}
	}
	if succeeded == 0 {
		t.Fatal("both concurrent writers failed")
	}
	rows, err := admin.Query("SELECT id::text,row_hash,prev_hash,body FROM " + quote(tableName) + " ORDER BY id")
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
			t.Fatalf("forked predecessor at row %d", count)
		}
		expected := computeSeedRowHash(prev, map[string]any{"id": id, "body": body}, table.Columns)
		if !bytes.Equal(rowHash, expected) {
			t.Fatalf("hash mismatch at row %d", count)
		}
		prev = rowHash
		count++
	}
	if count != 1+succeeded {
		t.Fatalf("rows=%d succeeded=%d", count, succeeded)
	}
}
