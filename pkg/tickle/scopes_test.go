package tickle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pickle-framework/pickle/pkg/schema"
)

func TestParseScopeBlocks(t *testing.T) {
	dir := t.TempDir()
	content := `//go:build pickle_template

package cooked

import "time"

// pickle:scope all
func (q *QueryBuilder[T]) Where__Column__(val __type__) *QueryBuilder[T] {
	return q.Where("__column__", val)
}

// pickle:scope string
func (q *QueryBuilder[T]) Where__Column__Like(val string) *QueryBuilder[T] {
	return q.WhereOp("__column__", "LIKE", val)
}

// pickle:scope timestamp
func (q *QueryBuilder[T]) Where__Column__Before(val time.Time) *QueryBuilder[T] {
	return q.WhereOp("__column__", "<", val)
}

// pickle:end
`
	path := filepath.Join(dir, "scopes.go")
	os.WriteFile(path, []byte(content), 0o644)

	blocks, err := ParseScopeBlocks(path)
	if err != nil {
		t.Fatalf("ParseScopeBlocks: %v", err)
	}

	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}

	if blocks[0].Scope != "all" {
		t.Errorf("block 0 scope: got %q, want %q", blocks[0].Scope, "all")
	}
	if blocks[1].Scope != "string" {
		t.Errorf("block 1 scope: got %q, want %q", blocks[1].Scope, "string")
	}
	if blocks[2].Scope != "timestamp" {
		t.Errorf("block 2 scope: got %q, want %q", blocks[2].Scope, "timestamp")
	}

	if !strings.Contains(blocks[0].Body, "Where__Column__") {
		t.Errorf("block 0 body missing placeholder: %s", blocks[0].Body)
	}
}

func TestGenerateScopes(t *testing.T) {
	blocks := []ScopeBlock{
		{Scope: "all", Body: `func (q *QueryBuilder[T]) Where__Column__(val __type__) *QueryBuilder[T] {
	return q.Where("__column__", val)
}`},
		{Scope: "string", Body: `func (q *QueryBuilder[T]) Where__Column__Like(val string) *QueryBuilder[T] {
	return q.WhereOp("__column__", "LIKE", val)
}`},
		{Scope: "timestamp", Body: `func (q *QueryBuilder[T]) Where__Column__Before(val time.Time) *QueryBuilder[T] {
	return q.WhereOp("__column__", "<", val)
}`},
	}

	tbl := &schema.Table{Name: "users"}
	tbl.String("email", 255).NotNull()
	tbl.Timestamp("created_at").NotNull()

	columns := ColumnsFromTable(tbl)
	output := GenerateScopes(blocks, columns, "User")

	// email should get "all" + "string" scopes
	if !strings.Contains(output, "WhereEmail(val string)") {
		t.Error("missing WhereEmail")
	}
	if !strings.Contains(output, "WhereEmailLike(val string)") {
		t.Error("missing WhereEmailLike")
	}
	// email should NOT get timestamp scopes
	if strings.Contains(output, "WhereEmailBefore") {
		t.Error("email should not have Before scope")
	}

	// created_at should get "all" + "timestamp" scopes
	if !strings.Contains(output, "WhereCreatedAt(val time.Time)") {
		t.Error("missing WhereCreatedAt")
	}
	if !strings.Contains(output, "WhereCreatedAtBefore(val time.Time)") {
		t.Error("missing WhereCreatedAtBefore")
	}
	// created_at should NOT get string scopes
	if strings.Contains(output, "WhereCreatedAtLike") {
		t.Error("created_at should not have Like scope")
	}

	// Check snake_case column names in SQL
	if !strings.Contains(output, `"email"`) {
		t.Error("missing snake_case column name for email")
	}
	if !strings.Contains(output, `"created_at"`) {
		t.Error("missing snake_case column name for created_at")
	}
}

func TestScopeForType(t *testing.T) {
	tests := []struct {
		colType schema.ColumnType
		want    string
	}{
		{schema.String, "string"},
		{schema.Text, "string"},
		{schema.Integer, "numeric"},
		{schema.BigInteger, "numeric"},
		{schema.Decimal, "numeric"},
		{schema.Timestamp, "timestamp"},
		{schema.Date, "timestamp"},
		{schema.UUID, "other"},
		{schema.Boolean, "other"},
		{schema.JSONB, "other"},
	}

	for _, tt := range tests {
		got := scopeForType(tt.colType)
		if got != tt.want {
			t.Errorf("scopeForType(%v) = %q, want %q", tt.colType, got, tt.want)
		}
	}
}
