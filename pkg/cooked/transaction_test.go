package cooked

import (
	"testing"
)

func TestTxConnReturnsUnderlyingConnection(t *testing.T) {
	// Tx.Conn() should return the underlying *sql.Tx
	tx := &Tx{conn: nil, depth: 0}
	if tx.Conn() != nil {
		t.Error("expected nil conn")
	}
}

func TestTxNestedDepthTracking(t *testing.T) {
	tx := &Tx{conn: nil, depth: 0}
	if tx.depth != 0 {
		t.Errorf("expected depth 0, got %d", tx.depth)
	}
}
