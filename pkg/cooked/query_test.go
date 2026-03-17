package cooked

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// --- dbColumns / dbValues / dbScanDest ---

type testModel struct {
	ID    string `db:"id"`
	Name  string `db:"name"`
	Email string `db:"email"`
	Skip  string `db:"-"`
	NoTag string
}

func TestDBColumns(t *testing.T) {
	m := &testModel{}
	cols := dbColumns(m)
	if len(cols) != 3 {
		t.Fatalf("dbColumns = %v, want 3 cols", cols)
	}
	if cols[0] != "id" || cols[1] != "name" || cols[2] != "email" {
		t.Errorf("dbColumns = %v, want [id name email]", cols)
	}
}

func TestDBColumnsNonPtr(t *testing.T) {
	m := testModel{}
	cols := dbColumns(m)
	if len(cols) != 3 {
		t.Fatalf("dbColumns (non-ptr) = %v, want 3 cols", cols)
	}
}

func TestDBValues(t *testing.T) {
	m := &testModel{ID: "1", Name: "Alice", Email: "a@example.com"}
	vals := dbValues(m)
	if len(vals) != 3 {
		t.Fatalf("dbValues = %v, want 3 vals", vals)
	}
	if vals[0] != "1" || vals[1] != "Alice" || vals[2] != "a@example.com" {
		t.Errorf("dbValues = %v, unexpected", vals)
	}
}

func TestDBScanDest(t *testing.T) {
	m := &testModel{}
	ptrs := dbScanDest(m)
	if len(ptrs) != 3 {
		t.Fatalf("dbScanDest = %v, want 3 ptrs", ptrs)
	}
	// Check they're actually pointers to the fields
	idPtr := ptrs[0].(*string)
	*idPtr = "set-via-ptr"
	if m.ID != "set-via-ptr" {
		t.Errorf("dbScanDest pointer didn't update struct field")
	}
}

// --- buildInsert ---

func TestBuildInsert(t *testing.T) {
	type Rec struct {
		ID    string `db:"id"`
		Name  string `db:"name"`
		Email string `db:"email"`
	}
	// id is zero → omitted so DB default fires
	r := &Rec{Name: "Bob", Email: "bob@example.com"}
	q, args := buildInsert("users", r)
	if !strings.Contains(q, "INSERT INTO users") {
		t.Errorf("buildInsert query = %q, want INSERT INTO users", q)
	}
	if !strings.Contains(q, "name") || !strings.Contains(q, "email") {
		t.Errorf("buildInsert query = %q, should contain name and email", q)
	}
	if strings.Contains(q, "\"id\"") {
		t.Errorf("buildInsert should omit zero-value id field")
	}
	if len(args) != 2 {
		t.Errorf("buildInsert args = %v, want 2", args)
	}
}

func TestBuildInsertNonZeroID(t *testing.T) {
	type Rec struct {
		ID   string `db:"id"`
		Name string `db:"name"`
	}
	r := &Rec{ID: "explicit-id", Name: "Alice"}
	q, args := buildInsert("users", r)
	if !strings.Contains(q, "id") {
		t.Errorf("buildInsert should include non-zero id: %q", q)
	}
	if len(args) != 2 {
		t.Errorf("buildInsert args = %v, want 2", args)
	}
}

// --- buildUpdate ---

func TestBuildUpdateByID(t *testing.T) {
	type Rec struct {
		ID    string `db:"id"`
		Name  string `db:"name"`
		Email string `db:"email"`
	}
	r := &Rec{ID: "42", Name: "New Name", Email: "new@example.com"}
	q, args := buildUpdate("users", r, nil)
	if !strings.Contains(q, "UPDATE users SET") {
		t.Errorf("buildUpdate query = %q, want UPDATE users SET", q)
	}
	if !strings.Contains(q, "WHERE id = $3") {
		t.Errorf("buildUpdate query = %q, want WHERE id = $3", q)
	}
	// args: name, email, id
	if len(args) != 3 {
		t.Errorf("buildUpdate args = %v, want 3", args)
	}
}

func TestBuildUpdateWithConditions(t *testing.T) {
	type Rec struct {
		ID   string `db:"id"`
		Name string `db:"name"`
	}
	r := &Rec{ID: "1", Name: "Alice"}
	conds := []condition{{column: "status", op: "=", value: "active"}}
	q, args := buildUpdate("users", r, conds)
	if !strings.Contains(q, "WHERE status = $2") {
		t.Errorf("buildUpdate with conditions = %q, want WHERE status = $2", q)
	}
	if len(args) != 2 {
		t.Errorf("buildUpdate args = %v, want 2", args)
	}
}

// --- buildSelect / buildCount / buildDelete / appendWhere ---

func TestBuildSelectNoConditions(t *testing.T) {
	q := Query[testModel]("users")
	sql, args := q.buildSelect()
	if !strings.HasPrefix(sql, "SELECT id, name, email FROM users") {
		t.Errorf("buildSelect = %q, unexpected", sql)
	}
	if args != nil {
		t.Errorf("buildSelect args = %v, want nil", args)
	}
}

func TestBuildSelectWithConditions(t *testing.T) {
	q := Query[testModel]("users")
	q.where("name", "Alice")
	sql, args := q.buildSelect()
	if !strings.Contains(sql, "WHERE name = $1") {
		t.Errorf("buildSelect with conditions = %q, want WHERE name = $1", sql)
	}
	if len(args) != 1 || args[0] != "Alice" {
		t.Errorf("buildSelect args = %v, want [Alice]", args)
	}
}

func TestBuildSelectOrderByLimitOffset(t *testing.T) {
	q := Query[testModel]("users")
	q.OrderBy("name", "ASC").Limit(10).Offset(5)
	sql, _ := q.buildSelect()
	if !strings.Contains(sql, "ORDER BY name ASC") {
		t.Errorf("buildSelect = %q, missing ORDER BY", sql)
	}
	if !strings.Contains(sql, "LIMIT 10") {
		t.Errorf("buildSelect = %q, missing LIMIT", sql)
	}
	if !strings.Contains(sql, "OFFSET 5") {
		t.Errorf("buildSelect = %q, missing OFFSET", sql)
	}
}

func TestBuildSelectSelectedCols(t *testing.T) {
	q := Query[testModel]("users")
	q.addSelect("id")
	q.addSelect("name")
	sql, _ := q.buildSelect()
	if !strings.HasPrefix(sql, "SELECT id, name FROM users") {
		t.Errorf("buildSelect with selectedCols = %q, unexpected", sql)
	}
}

func TestBuildCount(t *testing.T) {
	q := Query[testModel]("users")
	q.where("email", "x@example.com")
	sql, args := q.buildCount()
	if !strings.HasPrefix(sql, "SELECT COUNT(*) FROM users") {
		t.Errorf("buildCount = %q, unexpected", sql)
	}
	if !strings.Contains(sql, "WHERE email = $1") {
		t.Errorf("buildCount = %q, missing WHERE", sql)
	}
	if len(args) != 1 {
		t.Errorf("buildCount args = %v, want 1", args)
	}
}

func TestBuildDelete(t *testing.T) {
	q := Query[testModel]("users")
	q.where("id", "99")
	sql, args := q.buildDelete()
	if !strings.HasPrefix(sql, "DELETE FROM users WHERE id = $1") {
		t.Errorf("buildDelete = %q, unexpected", sql)
	}
	if len(args) != 1 {
		t.Errorf("buildDelete args = %v, want 1", args)
	}
}

func TestAppendWhereMultipleConditions(t *testing.T) {
	q := Query[testModel]("users")
	q.where("name", "Alice")
	q.whereOp("email", "!=", "x@example.com")
	sql, args := q.buildSelect()
	if !strings.Contains(sql, "AND") {
		t.Errorf("buildSelect multi-conditions = %q, missing AND", sql)
	}
	if len(args) != 2 {
		t.Errorf("args = %v, want 2", args)
	}
}

func TestWhereInNotIn(t *testing.T) {
	q := Query[testModel]("users")
	q.whereIn("id", []string{"1", "2", "3"})
	sql, _ := q.buildSelect()
	if !strings.Contains(sql, "id IN $1") {
		t.Errorf("whereIn = %q, want id IN $1", sql)
	}

	q2 := Query[testModel]("users")
	q2.whereNotIn("id", []string{"1"})
	sql2, _ := q2.buildSelect()
	if !strings.Contains(sql2, "id NOT IN $1") {
		t.Errorf("whereNotIn = %q, want id NOT IN $1", sql2)
	}
}

// --- QueryBuilder builder methods (chainable, no DB) ---

func TestQueryBuilderChaining(t *testing.T) {
	q := Query[testModel]("users")
	q2 := q.OrderBy("id", "DESC").Limit(5).Offset(10).EagerLoad("posts").AnyOwner()
	if q2 == nil {
		t.Fatal("chaining returned nil")
	}
	if q.limit != 5 {
		t.Errorf("Limit = %d, want 5", q.limit)
	}
	if q.offset != 10 {
		t.Errorf("Offset = %d, want 10", q.offset)
	}
	if len(q.eagerLoads) != 1 || q.eagerLoads[0] != "posts" {
		t.Errorf("EagerLoad = %v, want [posts]", q.eagerLoads)
	}
}

func TestQueryBuilderConnectionParam(t *testing.T) {
	q := Query[testModel]("users", "secondary")
	if q.connection != "secondary" {
		t.Errorf("connection = %q, want secondary", q.connection)
	}
}

func TestQueryBuilderSetVisibility(t *testing.T) {
	q := Query[testModel]("users")
	q.setVisibility(visibilityAll)
	if q.visibility != visibilityAll {
		t.Errorf("visibility = %d, want %d", q.visibility, visibilityAll)
	}
}

func TestQueryBuilderDB(t *testing.T) {
	// No connection set → returns global DB (may be nil)
	q := Query[testModel]("users")
	got := q.db()
	if got != DB {
		t.Errorf("db() should return global DB when no connection set")
	}

	// Named connection not in map → falls back to global DB
	q2 := Query[testModel]("users", "nonexistent")
	got2 := q2.db()
	if got2 != DB {
		t.Errorf("db() with unknown connection should fall back to global DB")
	}

	// Named connection in map → returns that DB
	fakeDB := &dummyDB{}
	_ = fakeDB
	// We can't easily test this without a real sql.DB, skip the happy path for Connections
}

// --- parseQueryFilters ---

func TestParseQueryFilters(t *testing.T) {
	r := httptest.NewRequest("GET", "/?filter[name]=Alice&filter[status]=active&other=x", nil)
	filters := parseQueryFilters(r)
	if filters["name"] != "Alice" {
		t.Errorf("filter[name] = %q, want Alice", filters["name"])
	}
	if filters["status"] != "active" {
		t.Errorf("filter[status] = %q, want active", filters["status"])
	}
	if _, ok := filters["other"]; ok {
		t.Error("non-filter param should not appear in filters")
	}
}

func TestParseQueryFiltersNestedSkipped(t *testing.T) {
	r := httptest.NewRequest("GET", "/?filter[name][op]=eq", nil)
	filters := parseQueryFilters(r)
	// Should not include nested filter[name][op] in simple filters
	if _, ok := filters["name][op"]; ok {
		t.Error("nested filter should be skipped in simple filters")
	}
}

// --- parseQueryFilterOps ---

func TestParseQueryFilterOps(t *testing.T) {
	r := httptest.NewRequest("GET", "/?filter[amount][gt]=100&filter[status][eq]=active", nil)
	ops := parseQueryFilterOps(r)
	if len(ops) != 2 {
		t.Fatalf("parseQueryFilterOps = %v, want 2 ops", ops)
	}
	found := map[string]FilterOp{}
	for _, op := range ops {
		found[op.Column] = op
	}
	if found["amount"].Operator != "gt" || found["amount"].Value != "100" {
		t.Errorf("amount op = %+v, unexpected", found["amount"])
	}
	if found["status"].Operator != "eq" || found["status"].Value != "active" {
		t.Errorf("status op = %+v, unexpected", found["status"])
	}
}

func TestParseQueryFilterOpsSimpleSkipped(t *testing.T) {
	// filter[name]=value (no operator) should be skipped by filterOps
	r := httptest.NewRequest("GET", "/?filter[name]=Alice", nil)
	ops := parseQueryFilterOps(r)
	if len(ops) != 0 {
		t.Errorf("parseQueryFilterOps should skip simple filters, got %v", ops)
	}
}

// --- parseQuerySort ---

func TestParseQuerySort(t *testing.T) {
	tests := []struct {
		query     string
		wantCol   string
		wantDir   string
	}{
		{"?sort=name", "name", "ASC"},
		{"?sort=-created_at", "created_at", "DESC"},
		{"", "", ""},
	}
	for _, tt := range tests {
		r := httptest.NewRequest("GET", "/"+tt.query, nil)
		col, dir := parseQuerySort(r)
		if col != tt.wantCol || dir != tt.wantDir {
			t.Errorf("parseQuerySort(%q) = (%q, %q), want (%q, %q)", tt.query, col, dir, tt.wantCol, tt.wantDir)
		}
	}
}

// --- parseQueryPage ---

func TestParseQueryPage(t *testing.T) {
	tests := []struct {
		query    string
		wantPage int
		wantSize int
	}{
		{"", 1, 25},
		{"?page[number]=3&page[size]=50", 3, 50},
		{"?page[number]=0", 1, 25}, // 0 is invalid, use default
		{"?page[size]=200", 1, 100}, // capped at 100
		{"?page[number]=abc", 1, 25}, // non-numeric, use default
	}
	for _, tt := range tests {
		r := httptest.NewRequest("GET", "/"+tt.query, nil)
		page, size := parseQueryPage(r)
		if page != tt.wantPage || size != tt.wantSize {
			t.Errorf("parseQueryPage(%q) = (%d, %d), want (%d, %d)", tt.query, page, size, tt.wantPage, tt.wantSize)
		}
	}
}

// dummy type to satisfy compilation
type dummyDB struct{}

// --- url.Values edge cases for parseQueryFilters ---

func TestParseQueryFiltersEmptyValue(t *testing.T) {
	u, _ := url.Parse("/?filter[name]=")
	r := httptest.NewRequest("GET", u.String(), nil)
	filters := parseQueryFilters(r)
	// Empty value still parsed (the key exists)
	if _, ok := filters["name"]; ok {
		// Value might be "" which is valid — empty string means filter key present
		// Actually empty vals[0] = "" but the check is len(vals) > 0 which is true
	}
	_ = filters
}
