package cooked

import (
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

type policyTestMessage struct {
	ID          string `db:"id"`
	WorkspaceID string `db:"workspace_id"`
}

func installPolicyTestDefinition(t *testing.T) {
	t.Helper()
	old := rowPolicyRuntimeRegistry
	rowPolicyRuntimeRegistry = map[string]rowPolicyRuntimeDefinition{}
	pred := &rowPolicyRuntimePredicate{Kind: "equal", Children: []rowPolicyRuntimePredicate{{Kind: "column", Name: "workspace_id"}, {Kind: "identity", Name: "workspace_id"}}}
	registerRowPolicyRuntime(rowPolicyRuntimeDefinition{Table: "messages", SubjectCombination: "any", Rules: []rowPolicyRuntimeRule{{Key: "member", SubjectKind: "role", SubjectName: "member", Select: pred, Insert: pred, UpdateOld: pred, UpdateNew: pred, Delete: pred}}})
	t.Cleanup(func() { rowPolicyRuntimeRegistry = old })
}

func TestProtectedReadAddsPolicyPredicate(t *testing.T) {
	installPolicyTestDefinition(t)
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })
	ctx := NewVerifiedPolicyContext(map[string]string{"workspace_id": "workspace-1"}, []string{"member"})
	query := `SELECT id, workspace_id FROM messages WHERE ((COALESCE(("workspace_id" = $1), FALSE)))`
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs("workspace-1").WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id"}).AddRow("m1", "workspace-1"))
	rows, err := Query[policyTestMessage]("messages").WithPolicyContext(ctx).All()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows", len(rows))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProtectedReadWithoutMatchingContextFailsBeforeSQL(t *testing.T) {
	installPolicyTestDefinition(t)
	_, err := Query[policyTestMessage]("messages").All()
	if err == nil {
		t.Fatal("expected policy context error")
	}
}

func TestProtectedCreateEvaluatesProposedRow(t *testing.T) {
	installPolicyTestDefinition(t)
	ctx := NewVerifiedPolicyContext(map[string]string{"workspace_id": "workspace-1"}, []string{"member"})
	err := Query[policyTestMessage]("messages").WithPolicyContext(ctx).Create(&policyTestMessage{ID: "m1", WorkspaceID: "workspace-2"})
	if err == nil {
		t.Fatal("expected proposed-row denial")
	}
}
