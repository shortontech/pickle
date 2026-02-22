package schema

import "testing"

func TestTableColumns(t *testing.T) {
	tbl := &Table{Name: "users"}
	tbl.UUID("id").PrimaryKey().Default("uuid_generate_v7()")
	tbl.String("name", 255).NotNull()
	tbl.String("email", 255).NotNull().Unique()
	tbl.String("password", 255).NotNull()
	tbl.Timestamps()

	if len(tbl.Columns) != 6 {
		t.Fatalf("expected 6 columns, got %d", len(tbl.Columns))
	}

	id := tbl.Columns[0]
	if id.Name != "id" || id.Type != UUID || !id.IsPrimaryKey || !id.HasDefault {
		t.Errorf("id column: got %+v", id)
	}

	email := tbl.Columns[2]
	if email.Name != "email" || !email.IsUnique || email.IsNullable {
		t.Errorf("email column: got %+v", email)
	}

	createdAt := tbl.Columns[4]
	if createdAt.Name != "created_at" || createdAt.Type != Timestamp || createdAt.IsNullable {
		t.Errorf("created_at column: got %+v", createdAt)
	}
}

func TestForeignKey(t *testing.T) {
	tbl := &Table{Name: "posts"}
	tbl.UUID("user_id").NotNull().ForeignKey("users", "id")

	col := tbl.Columns[0]
	if col.ForeignKeyTable != "users" || col.ForeignKeyColumn != "id" {
		t.Errorf("foreign key: got table=%s column=%s", col.ForeignKeyTable, col.ForeignKeyColumn)
	}
}

func TestDecimalPrecision(t *testing.T) {
	tbl := &Table{Name: "transfers"}
	tbl.Decimal("amount", 18, 2).NotNull()

	col := tbl.Columns[0]
	if col.Precision != 18 || col.Scale != 2 {
		t.Errorf("decimal: got precision=%d scale=%d", col.Precision, col.Scale)
	}
}

func TestStringDefaultLength(t *testing.T) {
	tbl := &Table{Name: "test"}
	tbl.String("name")

	if tbl.Columns[0].Length != 255 {
		t.Errorf("expected default length 255, got %d", tbl.Columns[0].Length)
	}
}

func TestNullable(t *testing.T) {
	tbl := &Table{Name: "test"}
	tbl.JSONB("metadata").Nullable()

	if !tbl.Columns[0].IsNullable {
		t.Error("expected nullable")
	}
}

func TestMigrationCreateTable(t *testing.T) {
	m := &Migration{}
	m.CreateTable("users", func(t *Table) {
		t.UUID("id").PrimaryKey()
		t.String("email").NotNull()
	})

	if len(m.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(m.Operations))
	}

	op := m.Operations[0]
	if op.Type != OpCreateTable || op.Table != "users" {
		t.Errorf("unexpected operation: %+v", op)
	}
	if len(op.TableDef.Columns) != 2 {
		t.Errorf("expected 2 columns, got %d", len(op.TableDef.Columns))
	}
}

func TestMigrationAddIndex(t *testing.T) {
	m := &Migration{}
	m.AddIndex("users", "email")
	m.AddUniqueIndex("users", "email", "tenant_id")

	if len(m.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(m.Operations))
	}

	if m.Operations[0].Index.Unique {
		t.Error("first index should not be unique")
	}
	if !m.Operations[1].Index.Unique {
		t.Error("second index should be unique")
	}
	if len(m.Operations[1].Index.Columns) != 2 {
		t.Errorf("expected 2 columns in composite index, got %d", len(m.Operations[1].Index.Columns))
	}
}

func TestMigrationDropAndRename(t *testing.T) {
	m := &Migration{}
	m.DropTableIfExists("old_table")
	m.RenameTable("users", "accounts")
	m.DropColumn("users", "legacy_field")
	m.RenameColumn("users", "name", "full_name")

	if len(m.Operations) != 4 {
		t.Fatalf("expected 4 operations, got %d", len(m.Operations))
	}

	if m.Operations[0].Type != OpDropTableIfExists {
		t.Error("expected drop table")
	}
	if m.Operations[1].Type != OpRenameTable || m.Operations[1].NewName != "accounts" {
		t.Error("expected rename table")
	}
	if m.Operations[2].Type != OpDropColumn || m.Operations[2].ColumnName != "legacy_field" {
		t.Error("expected drop column")
	}
	if m.Operations[3].Type != OpRenameColumn || m.Operations[3].NewName != "full_name" {
		t.Error("expected rename column")
	}
}
