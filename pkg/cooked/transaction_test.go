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

func TestSetLocalRejectsReservedPolicyIdentity(t *testing.T) {
	tx := &Tx{}
	if err := tx.SetLocal("pickle.identity.user_id", "spoofed"); err == nil {
		t.Fatal("expected reserved namespace error")
	}
}

func TestPostgresPolicyContextUsesTransactionLocalSettings(t *testing.T) {
	oldRegistry := rowPolicyRuntimeRegistry
	rowPolicyRuntimeRegistry = map[string]rowPolicyRuntimeDefinition{"messages": {Table: "messages", IdentityTypes: map[string]string{"user_id": "uuid"}}}
	t.Cleanup(func() { rowPolicyRuntimeRegistry = oldRegistry })
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
	mock.ExpectExec(regexp.QuoteMeta("SELECT set_config($1, $2, true)")).WithArgs("pickle.identity.user_id", "11111111-1111-4111-8111-111111111111").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("SELECT set_config($1, $2, true)")).WithArgs("pickle.identity.roles", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()
	tx := &Tx{conn: sqlTx}
	ctx := NewVerifiedPolicyContext(map[string]string{"user_id": "11111111-1111-4111-8111-111111111111"}, []string{"member"})
	if err := tx.WithPostgresPolicyContext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := tx.WithPolicyContext(ctx); err == nil {
		t.Fatal("expected sealed context error")
	}
	_ = sqlTx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
