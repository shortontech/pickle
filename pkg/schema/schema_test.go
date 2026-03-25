package schema

import (
	"testing"
)

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

// --- Column modifier tests ---

func TestColumnPrimaryKey(t *testing.T) {
	c := &Column{Name: "id", Type: UUID}
	c.PrimaryKey()
	if !c.IsPrimaryKey {
		t.Error("expected IsPrimaryKey true")
	}
}

func TestColumnNotNull(t *testing.T) {
	c := &Column{Name: "email", Type: String, IsNullable: true}
	c.NotNull()
	if c.IsNullable {
		t.Error("expected IsNullable false after NotNull()")
	}
}

func TestColumnDefault(t *testing.T) {
	c := &Column{Name: "status", Type: String}
	c.Default("active")
	if !c.HasDefault || c.DefaultValue != "active" {
		t.Errorf("expected default 'active', got %v, HasDefault=%v", c.DefaultValue, c.HasDefault)
	}
}

func TestColumnForeignKeyMethod(t *testing.T) {
	c := &Column{Name: "user_id", Type: UUID}
	c.ForeignKey("users", "id")
	if c.ForeignKeyTable != "users" || c.ForeignKeyColumn != "id" {
		t.Errorf("unexpected FK: table=%s col=%s", c.ForeignKeyTable, c.ForeignKeyColumn)
	}
}

func TestColumnPublic(t *testing.T) {
	c := &Column{Name: "name", Type: String}
	c.Public()
	if !c.IsPublic {
		t.Error("expected IsPublic true")
	}
}

func TestColumnOwnerSees(t *testing.T) {
	c := &Column{Name: "email", Type: String}
	c.OwnerSees()
	if !c.IsOwnerSees {
		t.Error("expected IsOwnerSees true")
	}
}

func TestColumnEncrypted(t *testing.T) {
	c := &Column{Name: "ssn", Type: String}
	c.Encrypted()
	if !c.IsEncrypted {
		t.Error("expected IsEncrypted true")
	}
}

func TestColumnUnsafePublic(t *testing.T) {
	c := &Column{Name: "ssn", Type: String}
	c.UnsafePublic()
	if !c.IsUnsafePublic {
		t.Error("expected IsUnsafePublic true")
	}
}

func TestColumnIsOwner(t *testing.T) {
	c := &Column{Name: "user_id", Type: UUID}
	c.IsOwner()
	if !c.IsOwnerColumn {
		t.Error("expected IsOwnerColumn true")
	}
}

func TestColumnChaining(t *testing.T) {
	tbl := &Table{Name: "users"}
	col := tbl.UUID("id").PrimaryKey().Default("uuid_generate_v7()")
	if !col.IsPrimaryKey || !col.HasDefault || col.DefaultValue != "uuid_generate_v7()" {
		t.Errorf("chaining failed: %+v", col)
	}
}

// --- Table column type tests ---

func TestTableAllColumnTypes(t *testing.T) {
	tbl := &Table{Name: "all_types"}
	tbl.UUID("uid")
	tbl.String("str")
	tbl.Text("txt")
	tbl.Integer("num")
	tbl.BigInteger("bignum")
	tbl.Decimal("amount", 10, 2)
	tbl.Boolean("active")
	tbl.Timestamp("at")
	tbl.JSONB("meta")
	tbl.Date("dob")
	tbl.Time("tod")
	tbl.Binary("data")

	expected := []ColumnType{UUID, String, Text, Integer, BigInteger, Decimal, Boolean, Timestamp, JSONB, Date, Time, Binary}
	if len(tbl.Columns) != len(expected) {
		t.Fatalf("expected %d columns, got %d", len(expected), len(tbl.Columns))
	}
	for i, typ := range expected {
		if tbl.Columns[i].Type != typ {
			t.Errorf("col[%d]: expected %v, got %v", i, typ, tbl.Columns[i].Type)
		}
	}
}

func TestTableStringCustomLength(t *testing.T) {
	tbl := &Table{Name: "t"}
	tbl.String("code", 10)
	if tbl.Columns[0].Length != 10 {
		t.Errorf("expected length 10, got %d", tbl.Columns[0].Length)
	}
}

func TestTableStringLengthPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for length < 1")
		}
	}()
	tbl := &Table{Name: "t"}
	tbl.String("x", 0)
}

func TestTableDecimalPanics(t *testing.T) {
	tests := []struct {
		name      string
		precision int
		scale     int
	}{
		{"precision zero", 0, 0},
		{"scale negative", 5, -1},
		{"scale > precision", 3, 5},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("expected panic for precision=%d scale=%d", tc.precision, tc.scale)
				}
			}()
			tbl := &Table{Name: "t"}
			tbl.Decimal("amount", tc.precision, tc.scale)
		})
	}
}

func TestAddColumnEmptyNamePanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty column name")
		}
	}()
	tbl := &Table{Name: "t"}
	tbl.UUID("")
}

// --- ColumnType.String() ---

func TestColumnTypeString(t *testing.T) {
	cases := []struct {
		typ  ColumnType
		want string
	}{
		{UUID, "uuid"},
		{String, "string"},
		{Text, "text"},
		{Integer, "integer"},
		{BigInteger, "bigint"},
		{Decimal, "decimal"},
		{Boolean, "boolean"},
		{Timestamp, "timestamp"},
		{JSONB, "jsonb"},
		{Date, "date"},
		{Time, "time"},
		{Binary, "binary"},
		{ColumnType(999), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.typ.String(); got != tc.want {
			t.Errorf("ColumnType(%d).String() = %q, want %q", tc.typ, got, tc.want)
		}
	}
}

// --- Migration helpers ---

func TestMigrationTransactionalDefault(t *testing.T) {
	m := &Migration{}
	if !m.Transactional() {
		t.Error("expected Transactional() to return true by default")
	}
}

func TestMigrationConnectionDefault(t *testing.T) {
	m := &Migration{}
	if m.Connection() != "" {
		t.Errorf("expected empty connection, got %q", m.Connection())
	}
}

func TestMigrationReset(t *testing.T) {
	m := &Migration{}
	m.CreateTable("x", func(t *Table) { t.UUID("id") })
	m.Reset()
	if len(m.Operations) != 0 {
		t.Error("expected operations cleared after Reset()")
	}
}

func TestMigrationGetOperations(t *testing.T) {
	m := &Migration{}
	m.DropTableIfExists("foo")
	ops := m.GetOperations()
	if len(ops) != 1 || ops[0].Type != OpDropTableIfExists {
		t.Errorf("unexpected GetOperations result: %v", ops)
	}
}

func TestMigrationCreateTableEmptyNamePanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty table name")
		}
	}()
	m := &Migration{}
	m.CreateTable("", func(t *Table) {})
}

func TestMigrationRenameTableEmptyPanic(t *testing.T) {
	tests := []struct{ old, new string }{
		{"", "b"},
		{"a", ""},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.old+"/"+tc.new, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Error("expected panic")
				}
			}()
			m := &Migration{}
			m.RenameTable(tc.old, tc.new)
		})
	}
}

func TestMigrationRenameColumnEmptyPanic(t *testing.T) {
	tests := []struct{ table, old, new string }{
		{"", "old", "new"},
		{"t", "", "new"},
		{"t", "old", ""},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.table+"/"+tc.old+"/"+tc.new, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Error("expected panic")
				}
			}()
			m := &Migration{}
			m.RenameColumn(tc.table, tc.old, tc.new)
		})
	}
}

func TestMigrationAddIndexEmptyPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for no columns")
		}
	}()
	m := &Migration{}
	m.AddIndex("users")
}

func TestMigrationAddUniqueIndexEmptyPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for no columns")
		}
	}()
	m := &Migration{}
	m.AddUniqueIndex("users")
}

func TestMigrationDropColumn(t *testing.T) {
	m := &Migration{}
	m.DropColumn("users", "legacy")
	op := m.Operations[0]
	if op.Type != OpDropColumn || op.Table != "users" || op.ColumnName != "legacy" {
		t.Errorf("unexpected op: %+v", op)
	}
}

func TestMigrationAddColumn(t *testing.T) {
	m := &Migration{}
	m.AddColumn("users", func(t *Table) {
		t.String("nickname")
	})
	if len(m.Operations) != 1 || m.Operations[0].Type != OpAddColumn {
		t.Error("expected OpAddColumn")
	}
}

func TestMigrationAlterTable(t *testing.T) {
	m := &Migration{}
	m.AlterTable("users", func(t *Table) {
		t.String("phone")
		t.String("bio")
	})
	if len(m.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(m.Operations))
	}
	for _, op := range m.Operations {
		if op.Type != OpAddColumn {
			t.Errorf("expected OpAddColumn, got %v", op.Type)
		}
	}
}

func TestMigrationCreateAndDropView(t *testing.T) {
	m := &Migration{}
	m.CreateView("active_users", func(v *View) {
		v.From("users", "u")
		v.Column("u.id")
		v.Column("u.email")
	})
	m.DropView("active_users")

	if len(m.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(m.Operations))
	}
	if m.Operations[0].Type != OpCreateView {
		t.Error("expected OpCreateView")
	}
	if m.Operations[1].Type != OpDropView {
		t.Error("expected OpDropView")
	}
}

// --- HasMany / HasOne relationships ---

func TestHasManyFlattensOperations(t *testing.T) {
	m := &Migration{}
	m.CreateTable("users", func(t *Table) {
		t.UUID("id").PrimaryKey()
		t.String("email").NotNull()
		t.HasMany("posts", func(t *Table) {
			t.UUID("id").PrimaryKey()
			t.String("title").NotNull()
		})
	})

	// Expect: OpCreateTable(users), OpCreateTable(posts), OpAddIndex(posts)
	if len(m.Operations) != 3 {
		t.Fatalf("expected 3 operations, got %d", len(m.Operations))
	}
	if m.Operations[1].Type != OpCreateTable || m.Operations[1].Table != "posts" {
		t.Errorf("expected OpCreateTable(posts), got %+v", m.Operations[1])
	}
	if m.Operations[2].Type != OpAddIndex || m.Operations[2].Table != "posts" {
		t.Errorf("expected OpAddIndex(posts), got %+v", m.Operations[2])
	}

	postsTable := m.Operations[1].TableDef
	var fkCol *Column
	for _, col := range postsTable.Columns {
		if col.ForeignKeyTable == "users" {
			fkCol = col
			break
		}
	}
	if fkCol == nil {
		t.Fatal("expected FK column injected into posts")
	}
	if fkCol.Name != "user_id" {
		t.Errorf("expected FK column 'user_id', got %q", fkCol.Name)
	}
	if fkCol.IsUnique {
		t.Error("HasMany FK should not be unique")
	}
}

func TestHasOneCreatesFKUnique(t *testing.T) {
	m := &Migration{}
	m.CreateTable("users", func(t *Table) {
		t.UUID("id").PrimaryKey()
		t.HasOne("profiles", func(t *Table) {
			t.UUID("id").PrimaryKey()
			t.Text("bio")
		})
	})

	profilesTable := m.Operations[1].TableDef
	var fkCol *Column
	for _, col := range profilesTable.Columns {
		if col.ForeignKeyTable == "users" {
			fkCol = col
			break
		}
	}
	if fkCol == nil {
		t.Fatal("expected FK column injected into profiles")
	}
	if !fkCol.IsUnique {
		t.Error("HasOne FK should be unique")
	}
}

func TestRelationshipCollectionAndTopLevel(t *testing.T) {
	tbl := &Table{Name: "users"}
	rel := tbl.HasMany("posts", func(t *Table) {
		t.UUID("id").PrimaryKey()
	})
	rel.Collection().TopLevelModel()
	if !rel.IsCollection || !rel.IsTopLevel {
		t.Errorf("expected IsCollection and IsTopLevel: %+v", rel)
	}
}

func TestHasManyNestedRelationship(t *testing.T) {
	m := &Migration{}
	m.CreateTable("users", func(t *Table) {
		t.UUID("id").PrimaryKey()
		t.HasMany("posts", func(pt *Table) {
			pt.UUID("id").PrimaryKey()
			pt.HasMany("comments", func(ct *Table) {
				ct.UUID("id").PrimaryKey()
				ct.Text("body")
			})
		})
	})
	// users(1) + posts(1) + index(posts.user_id)(1) + comments(1) + index(comments.post_id)(1) = 5
	if len(m.Operations) != 5 {
		t.Fatalf("expected 5 operations for nested HasMany, got %d", len(m.Operations))
	}
}

// --- singularize ---

func TestSingularize(t *testing.T) {
	cases := []struct{ input, want string }{
		{"users", "user"},
		{"posts", "post"},
		{"categories", "category"},
		{"people", "person"},
		{"children", "child"},
		{"men", "man"},
		{"women", "woman"},
		{"mice", "mouse"},
		{"geese", "goose"},
		{"teeth", "tooth"},
		{"feet", "foot"},
		{"data", "datum"},
		{"indices", "index"},
		{"matrices", "matrix"},
		{"buses", "bus"},
		{"boxes", "box"},
		{"fizzes", "fizz"},
		{"churches", "church"},
		{"dishes", "dish"},
		{"already", "already"},
	}
	for _, tc := range cases {
		if got := singularize(tc.input); got != tc.want {
			t.Errorf("singularize(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// --- findPKName and findPKType ---

func TestFindPKName(t *testing.T) {
	tbl := &Table{Name: "users"}
	tbl.UUID("id").PrimaryKey()
	tbl.String("email")

	if got := findPKName(tbl); got != "id" {
		t.Errorf("expected 'id', got %q", got)
	}
}

func TestFindPKNameDefault(t *testing.T) {
	tbl := &Table{Name: "no_pk"}
	tbl.String("email")

	if got := findPKName(tbl); got != "id" {
		t.Errorf("expected default 'id', got %q", got)
	}
}

func TestFindPKType(t *testing.T) {
	tbl := &Table{Name: "transfers"}
	tbl.Integer("id").PrimaryKey()

	if got := findPKType(tbl); got != Integer {
		t.Errorf("expected Integer PK type, got %v", got)
	}
}

func TestFindPKTypeDefault(t *testing.T) {
	tbl := &Table{Name: "no_pk"}
	if got := findPKType(tbl); got != UUID {
		t.Errorf("expected UUID default, got %v", got)
	}
}

// --- insertFKColumn ---

func TestInsertFKColumnAfterPK(t *testing.T) {
	pk := &Column{Name: "id", IsPrimaryKey: true}
	other := &Column{Name: "email"}
	fk := &Column{Name: "user_id"}

	result := insertFKColumn([]*Column{pk, other}, fk)
	if len(result) != 3 || result[1] != fk {
		t.Errorf("expected FK after PK at index 1, got %v", result)
	}
}

func TestInsertFKColumnNoPK(t *testing.T) {
	col := &Column{Name: "email"}
	fk := &Column{Name: "user_id"}

	result := insertFKColumn([]*Column{col}, fk)
	if len(result) != 2 || result[0] != fk {
		t.Errorf("expected FK prepended, got %v", result)
	}
}

// --- DriverCaps ---

func TestDriverCapsPostgres(t *testing.T) {
	for _, driver := range []string{"pgsql", "postgres"} {
		caps := DriverCaps(driver)
		if !caps.TransactionalDDL || !caps.JSONBSupport || !caps.UUIDNativeType || !caps.AdvisoryLocks || !caps.ForeignKeys {
			t.Errorf("postgres driver %q missing expected capabilities: %+v", driver, caps)
		}
	}
}

func TestDriverCapsMySQL(t *testing.T) {
	caps := DriverCaps("mysql")
	if caps.TransactionalDDL || caps.JSONBSupport || !caps.ForeignKeys {
		t.Errorf("mysql caps unexpected: %+v", caps)
	}
}

func TestDriverCapsSQLite(t *testing.T) {
	caps := DriverCaps("sqlite")
	if !caps.TransactionalDDL || !caps.ForeignKeys {
		t.Errorf("sqlite caps unexpected: %+v", caps)
	}
	if caps.JSONBSupport || caps.AdvisoryLocks {
		t.Errorf("sqlite should not have JSONB or advisory locks: %+v", caps)
	}
}

func TestDriverCapsMongoDB(t *testing.T) {
	caps := DriverCaps("mongodb")
	if !caps.EmbeddedDocs || caps.ForeignKeys || caps.TransactionalDDL {
		t.Errorf("mongodb caps unexpected: %+v", caps)
	}
}

func TestDriverCapsDynamoDB(t *testing.T) {
	caps := DriverCaps("dynamodb")
	if !caps.EmbeddedDocs || caps.UniqueIndex {
		t.Errorf("dynamodb caps unexpected: %+v", caps)
	}
}

func TestDriverCapsUnknown(t *testing.T) {
	caps := DriverCaps("unknown_driver")
	if caps.TransactionalDDL || caps.ForeignKeys || caps.EmbeddedDocs {
		t.Errorf("unknown driver should return empty caps: %+v", caps)
	}
}

// --- View ---

func TestViewFrom(t *testing.T) {
	v := &View{Name: "v"}
	v.From("users", "u")
	if len(v.Sources) != 1 || v.Sources[0].Table != "users" || v.Sources[0].Alias != "u" {
		t.Errorf("unexpected Sources: %+v", v.Sources)
	}
}

func TestViewFromPanic(t *testing.T) {
	tests := []struct{ table, alias string }{
		{"", "u"},
		{"users", ""},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.table+"/"+tc.alias, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Error("expected panic")
				}
			}()
			v := &View{Name: "v"}
			v.From(tc.table, tc.alias)
		})
	}
}

func TestViewJoin(t *testing.T) {
	v := &View{Name: "v"}
	v.From("users", "u")
	v.Join("posts", "p", "p.user_id = u.id")
	if len(v.Sources) != 2 || v.Sources[1].JoinType != "JOIN" {
		t.Errorf("unexpected join source: %+v", v.Sources)
	}
}

func TestViewJoinPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty join params")
		}
	}()
	v := &View{Name: "v"}
	v.Join("", "p", "cond")
}

func TestViewLeftJoin(t *testing.T) {
	v := &View{Name: "v"}
	v.From("users", "u")
	v.LeftJoin("posts", "p", "p.user_id = u.id")
	if v.Sources[1].JoinType != "LEFT JOIN" {
		t.Errorf("expected LEFT JOIN, got %q", v.Sources[1].JoinType)
	}
}

func TestViewLeftJoinPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	v := &View{Name: "v"}
	v.LeftJoin("posts", "", "cond")
}

func TestViewColumnRef(t *testing.T) {
	v := &View{Name: "v"}
	v.From("users", "u")
	v.Column("u.email")
	if len(v.Columns) != 1 || v.Columns[0].SourceAlias != "u" || v.Columns[0].SourceColumn != "email" {
		t.Errorf("unexpected column: %+v", v.Columns[0])
	}
}

func TestViewColumnWithAlias(t *testing.T) {
	v := &View{Name: "v"}
	v.From("users", "u")
	v.Column("u.email", "user_email")
	col := v.Columns[0]
	if col.OutputAlias != "user_email" || col.Name != "user_email" {
		t.Errorf("unexpected column alias: %+v", col)
	}
}

func TestViewColumnInvalidRefPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for ref without dot")
		}
	}()
	v := &View{Name: "v"}
	v.Column("no_dot_ref")
}

func TestViewSelectRaw(t *testing.T) {
	v := &View{Name: "v"}
	v.From("orders", "o")
	vc := v.SelectRaw("total", "SUM(o.amount)")
	if vc.RawExpr != "SUM(o.amount)" || vc.Name != "total" {
		t.Errorf("unexpected SelectRaw: %+v", vc)
	}
}

func TestViewSelectRawPanic(t *testing.T) {
	tests := []struct{ name, expr string }{
		{"", "SUM(x)"},
		{"total", ""},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name+"/"+tc.expr, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Error("expected panic")
				}
			}()
			v := &View{Name: "v"}
			v.SelectRaw(tc.name, tc.expr)
		})
	}
}

func TestViewGroupBy(t *testing.T) {
	v := &View{Name: "v"}
	v.GroupBy("u.id", "u.email")
	if len(v.GroupByCols) != 2 || v.GroupByCols[0] != "u.id" {
		t.Errorf("unexpected GroupBy: %+v", v.GroupByCols)
	}
}

func TestViewColumnOutputName(t *testing.T) {
	cases := []struct {
		vc   ViewColumn
		want string
	}{
		{ViewColumn{Column: Column{Name: "x"}, OutputAlias: "alias"}, "alias"},
		{ViewColumn{Column: Column{Name: "x"}, SourceColumn: "src"}, "src"},
		{ViewColumn{Column: Column{Name: "x"}}, "x"},
	}
	for _, tc := range cases {
		if got := tc.vc.OutputName(); got != tc.want {
			t.Errorf("OutputName() = %q, want %q for %+v", got, tc.want, tc.vc)
		}
	}
}

func TestViewColumnTypeBuilders(t *testing.T) {
	vc := &ViewColumn{}
	vc.BigInteger()
	if vc.Type != BigInteger {
		t.Error("expected BigInteger")
	}
	vc.IntegerType()
	if vc.Type != Integer {
		t.Error("expected Integer")
	}
	vc.Decimal(10, 2)
	if vc.Type != Decimal || vc.Precision != 10 || vc.Scale != 2 {
		t.Errorf("expected Decimal(10,2): %+v", vc)
	}
	vc.StringType(100)
	if vc.Type != String || vc.Length != 100 {
		t.Errorf("expected String(100): %+v", vc)
	}
	vc.StringType()
	if vc.Length != 255 {
		t.Errorf("expected default length 255, got %d", vc.Length)
	}
	vc.TextType()
	if vc.Type != Text {
		t.Error("expected Text")
	}
	vc.BooleanType()
	if vc.Type != Boolean {
		t.Error("expected Boolean")
	}
	vc.TimestampType()
	if vc.Type != Timestamp {
		t.Error("expected Timestamp")
	}
	vc.UUIDType()
	if vc.Type != UUID {
		t.Error("expected UUID")
	}
	vc.JSONBType()
	if vc.Type != JSONB {
		t.Error("expected JSONB")
	}
}

// --- Timestamps ---

func TestTimestampsColumns(t *testing.T) {
	tbl := &Table{Name: "t"}
	tbl.Timestamps()
	if len(tbl.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(tbl.Columns))
	}
	ca := tbl.Columns[0]
	ua := tbl.Columns[1]
	if ca.Name != "created_at" || ca.IsNullable || !ca.HasDefault {
		t.Errorf("created_at: %+v", ca)
	}
	if ua.Name != "updated_at" || ua.IsNullable || !ua.HasDefault {
		t.Errorf("updated_at: %+v", ua)
	}
}

// --- AlterTable with relationships ---

func TestAlterTableWithRelationship(t *testing.T) {
	m := &Migration{}
	m.CreateTable("users", func(t *Table) {
		t.UUID("id").PrimaryKey()
	})
	m.AlterTable("users", func(t *Table) {
		t.HasMany("tags", func(ct *Table) {
			ct.UUID("id").PrimaryKey()
			ct.String("name")
		})
	})
	// CreateTable(users)=1, CreateTable(tags)=1, AddIndex(tags.user_id)=1
	if len(m.Operations) != 3 {
		t.Fatalf("expected 3 operations, got %d", len(m.Operations))
	}
}

func TestColumnRoleSees(t *testing.T) {
	col := &Column{Name: "ssn", Type: String}
	col.RoleSees("compliance").RoleSees("support_lead")

	if len(col.VisibleTo) != 2 {
		t.Fatalf("expected 2 entries in VisibleTo, got %d", len(col.VisibleTo))
	}
	if !col.VisibleTo["compliance"] {
		t.Error("expected compliance in VisibleTo")
	}
	if !col.VisibleTo["support_lead"] {
		t.Error("expected support_lead in VisibleTo")
	}
}

func TestColumnRoleSeesChainable(t *testing.T) {
	tbl := &Table{Name: "users"}
	tbl.String("ssn", 11).NotNull().RoleSees("compliance").RoleSees("support_lead").Encrypted()

	col := tbl.Columns[0]
	if !col.IsEncrypted {
		t.Error("expected encrypted after chaining with RoleSees")
	}
	if len(col.VisibleTo) != 2 {
		t.Error("expected 2 VisibleTo entries")
	}
}
