package cooked

import (
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestTxConnReturnsUnderlyingConnection(t *testing.T) {
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

func TestSetLocalUsesBoundTransactionLocalConfig(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	sqlTx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec(regexp.QuoteMeta("SELECT set_config($1, $2, true)")).WithArgs("dill.workspace_id", "workspace-1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	tx := &Tx{conn: sqlTx}
	if err := tx.SetLocal("dill.workspace_id", "workspace-1"); err != nil {
		t.Fatal(err)
	}
	if err := sqlTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSetLocalRejectsEmptyName(t *testing.T) {
	tx := &Tx{}
	if err := tx.SetLocal(" ", "value"); err == nil {
		t.Fatal("expected validation error")
	}
}
