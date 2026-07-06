//go:build ignore

package migration

import (
	"strings"
	"testing"
)

// These tests exercise encrypted/sealed column expansion in the DDL builders.
// Like the rest of pkg/migration, the file is a template (//go:build ignore):
// it compiles and runs once tickled into a generated project's migrations
// package (alongside the schema types), mirroring derive_test.go.

func encCreateTableSQL(fn func(tb *Table)) string {
	var m Migration
	m.CreateTable("users", fn)
	g := &postgresGenerator{}
	return g.CreateTable(m.GetOperations()[0].TableDef)
}

func encAddColumnSQL(fn func(tb *Table)) []string {
	var m Migration
	m.AddColumn("users", fn)
	r := &Runner{Generator: &postgresGenerator{}}
	return r.opsToSQL(m.GetOperations()[0])
}

func TestEncryptedColumnExpandsInCreateTable(t *testing.T) {
	sql := encCreateTableSQL(func(tb *Table) {
		tb.UUID("id").PrimaryKey()
		tb.String("email", 255).NotNull().Unique().Encrypted()
	})

	// Deterministic encryption preserves uniqueness on the ciphertext column.
	if !strings.Contains(sql, `"email_encrypted" TEXT NOT NULL UNIQUE`) {
		t.Errorf("expected email_encrypted TEXT NOT NULL UNIQUE, got:\n%s", sql)
	}
	// Rotation slot is always nullable and never NOT NULL.
	if !strings.Contains(sql, `"email_encrypted_v2" TEXT`) {
		t.Errorf("expected email_encrypted_v2 TEXT, got:\n%s", sql)
	}
	if strings.Contains(sql, `"email_encrypted_v2" TEXT NOT NULL`) {
		t.Errorf("email_encrypted_v2 must be nullable, got:\n%s", sql)
	}
	// The plaintext column and its declared VARCHAR type are never emitted.
	for _, line := range strings.Split(sql, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), `"email" `) {
			t.Errorf("bare email column must not be emitted: %q", line)
		}
	}
	if strings.Contains(sql, `"email_encrypted" VARCHAR`) {
		t.Errorf("ciphertext column must be TEXT, not VARCHAR:\n%s", sql)
	}
}

func TestSealedColumnDropsUniquenessInCreateTable(t *testing.T) {
	sql := encCreateTableSQL(func(tb *Table) {
		tb.UUID("id").PrimaryKey()
		tb.Text("private_key").NotNull().Unique().Sealed()
	})

	if !strings.Contains(sql, `"private_key_encrypted" TEXT NOT NULL`) {
		t.Errorf("expected private_key_encrypted TEXT NOT NULL, got:\n%s", sql)
	}
	// Non-deterministic encryption makes uniqueness meaningless — it is dropped.
	if strings.Contains(sql, "UNIQUE") {
		t.Errorf("sealed column must not carry UNIQUE, got:\n%s", sql)
	}
	if !strings.Contains(sql, `"private_key_encrypted_v2" TEXT`) {
		t.Errorf("expected private_key_encrypted_v2 TEXT, got:\n%s", sql)
	}
}

func TestEncryptedColumnExpandsInAddColumn(t *testing.T) {
	sqls := encAddColumnSQL(func(tb *Table) {
		tb.String("ssn", 255).NotNull().Unique().Encrypted()
	})
	if len(sqls) != 2 {
		t.Fatalf("expected 2 ADD COLUMN statements, got %d: %v", len(sqls), sqls)
	}
	joined := strings.Join(sqls, "\n")
	if !strings.Contains(joined, `ADD COLUMN "ssn_encrypted" TEXT NOT NULL UNIQUE`) {
		t.Errorf("expected ADD COLUMN ssn_encrypted TEXT NOT NULL UNIQUE, got:\n%s", joined)
	}
	if !strings.Contains(joined, `ADD COLUMN "ssn_encrypted_v2" TEXT`) {
		t.Errorf("expected ADD COLUMN ssn_encrypted_v2 TEXT, got:\n%s", joined)
	}
	if strings.Contains(joined, `"ssn" `) {
		t.Errorf("bare ssn column must not be added, got:\n%s", joined)
	}
}

func TestPlainColumnUnaffectedByExpansion(t *testing.T) {
	sql := encCreateTableSQL(func(tb *Table) {
		tb.UUID("id").PrimaryKey()
		tb.String("name", 255).NotNull()
	})
	if !strings.Contains(sql, `"name" VARCHAR(255) NOT NULL`) {
		t.Errorf("plain column must be unchanged, got:\n%s", sql)
	}
}
