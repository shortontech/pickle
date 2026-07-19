package generator

import (
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
)

func TestAppendOnlyQueryStructurallyExcludesMutableBuilder(t *testing.T) {
	table := &schema.Table{Name: "inventory_movements", IsAppendOnly: true}
	table.Columns = []*schema.Column{
		{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
		{Name: "row_hash", Type: schema.Binary},
		{Name: "prev_hash", Type: schema.Binary},
		{Name: "quantity", Type: schema.Integer},
	}

	src, err := GenerateQueryScopes(table, loadScopeBlocks(t), "models")
	if err != nil {
		t.Fatalf("GenerateQueryScopes: %v", err)
	}
	got := string(src)
	for _, want := range []string{
		`"database/sql"`,
		"*AppendOnlyQueryBuilder[InventoryMovement]",
		"AppendOnlyQuery[InventoryMovement](\"inventory_movements\")",
		"q.AppendOnlyQueryBuilder.Create(model)",
		"TransactionOn(",
		`lockIntegrityChain(q.db(), "inventory_movements")`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("generated append-only query missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "*QueryBuilder[InventoryMovement]") {
		t.Fatalf("append-only query embeds mutable QueryBuilder:\n%s", got)
	}
	if strings.Contains(got, "func (q *InventoryMovementQuery) Update(") || strings.Contains(got, "func (q *InventoryMovementQuery) Delete(") {
		t.Fatalf("append-only query generated a mutation method:\n%s", got)
	}
}

func TestAppendOnlyTxUsesRestrictedBuilder(t *testing.T) {
	table := &schema.Table{Name: "inventory_movements", IsAppendOnly: true}
	src, err := GenerateTxMethods([]*schema.Table{table}, nil, "app/models", "models")
	if err != nil {
		t.Fatalf("GenerateTxMethods: %v", err)
	}
	got := string(src)
	if !strings.Contains(got, "q.AppendOnlyQueryBuilder.setTx(tx.Conn())") {
		t.Fatalf("append-only transaction query does not retain restricted builder:\n%s", got)
	}
	if strings.Contains(got, "q.QueryBuilder.setTx") {
		t.Fatalf("append-only transaction query reaches mutable builder:\n%s", got)
	}
}
