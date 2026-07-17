package cooked

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRuntimePredicateCompilesResolvedRelationship(t *testing.T) {
	pred := rowPolicyRuntimePredicate{Kind: "exists", RelatedTable: "memberships", LocalColumn: "id", ForeignColumn: "user_id", Children: []rowPolicyRuntimePredicate{{Kind: "equal", Children: []rowPolicyRuntimePredicate{{Kind: "column", Name: "workspace_id"}, {Kind: "identity", Name: "workspace_id"}}}}}
	sql, args, err := compileRuntimePredicate(pred, `"users"`, PolicyContext{identities: map[string]string{"workspace_id": "01900000-0000-7000-8000-000000000001"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`EXISTS (SELECT 1 FROM "memberships" pickle_rel`, `pickle_rel."user_id" = "users"."id"`, `pickle_rel."workspace_id"`} {
		if !strings.Contains(sql, want) {
			t.Errorf("missing %q in %s", want, sql)
		}
	}
	if len(args) != 1 {
		t.Fatalf("args=%#v", args)
	}
}

type policyTestMessage struct {
	ID          string `db:"id"`
	WorkspaceID string `db:"workspace_id"`
}

type nullablePolicyMessage struct {
	WorkspaceID *string `db:"workspace_id"`
}

func installPolicyTestDefinition(t *testing.T) {
	t.Helper()
	old := rowPolicyRuntimeRegistry
	rowPolicyRuntimeRegistry = map[string]rowPolicyRuntimeDefinition{}
	pred := &rowPolicyRuntimePredicate{Kind: "equal", Children: []rowPolicyRuntimePredicate{{Kind: "column", Name: "workspace_id"}, {Kind: "identity", Name: "workspace_id"}}}
	registerRowPolicyRuntime(rowPolicyRuntimeDefinition{Table: "messages", SubjectCombination: "any", IdentityTypes: map[string]string{"workspace_id": "string"}, Rules: []rowPolicyRuntimeRule{{Key: "member", SubjectKind: "role", SubjectName: "member", Select: pred, Insert: pred, UpdateOld: pred, UpdateNew: pred, Delete: pred}}})
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

func TestProtectedReadTerminalMatrixAddsPolicyPredicate(t *testing.T) {
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
	clause := `WHERE ((COALESCE(("workspace_id" = $1), FALSE)))`
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, workspace_id FROM messages ` + clause + ` LIMIT 1`)).WithArgs("workspace-1").WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id"}).AddRow("m1", "workspace-1"))
	if _, err := Query[policyTestMessage]("messages").WithPolicyContext(ctx).First(); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM messages ` + clause)).WithArgs("workspace-1").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	if count, err := Query[policyTestMessage]("messages").WithPolicyContext(ctx).Count(); err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT SUM(workspace_id) FROM messages ` + clause)).WithArgs("workspace-1").WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow(1.0))
	if value, err := Query[policyTestMessage]("messages").WithPolicyContext(ctx).aggregate("SUM", "workspace_id"); err != nil || value == nil || *value != 1 {
		t.Fatalf("aggregate=%v err=%v", value, err)
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, workspace_id FROM messages ` + clause)).WithArgs("workspace-1").WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id"}).AddRow("m1", "workspace-1"))
	if rows, err := Query[policyTestMessage]("messages").WithPolicyContext(ctx).EagerLoad("comments").All(); err != nil || len(rows) != 1 {
		t.Fatalf("eager rows=%d err=%v", len(rows), err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProtectedLockSurfacesRetainPolicyPredicate(t *testing.T) {
	installPolicyTestDefinition(t)
	ctx := NewVerifiedPolicyContext(map[string]string{"workspace_id": "workspace-1"}, []string{"member"})
	for _, lock := range []func(*QueryBuilder[policyTestMessage]) *QueryBuilder[policyTestMessage]{
		func(q *QueryBuilder[policyTestMessage]) *QueryBuilder[policyTestMessage] { return q.LockForUpdate() },
		func(q *QueryBuilder[policyTestMessage]) *QueryBuilder[policyTestMessage] { return q.LockForShare() },
	} {
		q := lock(Query[policyTestMessage]("messages").WithPolicyContext(ctx))
		if err := q.preparePolicy("select"); err != nil {
			t.Fatal(err)
		}
		sql, args := q.buildSelect()
		if !strings.Contains(sql, `COALESCE(("workspace_id" = $1), FALSE)`) || len(args) != 1 {
			t.Fatalf("lock query lost policy: %s args=%#v", sql, args)
		}
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

func TestProposedPolicyUsesSQLNullComparisonSemantics(t *testing.T) {
	old := rowPolicyRuntimeRegistry
	pred := &rowPolicyRuntimePredicate{Kind: "not_equal", Children: []rowPolicyRuntimePredicate{{Kind: "column", Name: "workspace_id"}, {Kind: "identity", Name: "workspace_id"}}}
	rowPolicyRuntimeRegistry = map[string]rowPolicyRuntimeDefinition{"messages": {Table: "messages", SubjectCombination: "any", IdentityTypes: map[string]string{"workspace_id": "string"}, Rules: []rowPolicyRuntimeRule{{SubjectKind: "public", Insert: pred}}}}
	t.Cleanup(func() { rowPolicyRuntimeRegistry = old })
	ctx := NewVerifiedPolicyContext(map[string]string{"workspace_id": "workspace-1"}, nil)
	if err := evaluateRowPolicyRecord("messages", "insert", &ctx, &nullablePolicyMessage{}); err == nil {
		t.Fatal("NULL <> identity must normalize to false")
	}
}

func TestAllSubjectCombinationRequiresEveryMatchingRule(t *testing.T) {
	old := rowPolicyRuntimeRegistry
	allow, deny := &rowPolicyRuntimePredicate{Kind: "allow"}, &rowPolicyRuntimePredicate{Kind: "deny"}
	rowPolicyRuntimeRegistry = map[string]rowPolicyRuntimeDefinition{"messages": {Table: "messages", SubjectCombination: "all", Rules: []rowPolicyRuntimeRule{{SubjectKind: "public", Insert: allow}, {SubjectKind: "role", SubjectName: "member", Insert: deny}}}}
	t.Cleanup(func() { rowPolicyRuntimeRegistry = old })
	ctx := NewVerifiedPolicyContext(nil, []string{"member"})
	if err := evaluateRowPolicyRecord("messages", "insert", &ctx, &policyTestMessage{}); err == nil {
		t.Fatal("all subject combination must deny when one matching rule denies")
	}
}

func TestVerifiedPolicyContextCanonicalizesTypedIdentities(t *testing.T) {
	old := rowPolicyRuntimeRegistry
	rowPolicyRuntimeRegistry = map[string]rowPolicyRuntimeDefinition{"messages": {Table: "messages", IdentityTypes: map[string]string{"user_id": "uuid"}}}
	t.Cleanup(func() { rowPolicyRuntimeRegistry = old })
	valid := NewVerifiedPolicyContext(map[string]string{"user_id": "11111111-1111-4111-8111-111111111111", "undeclared": "value"}, nil)
	if value, ok := valid.identity("user_id"); !ok || value != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("unexpected UUID: %q %t", value, ok)
	}
	if _, ok := valid.identity("undeclared"); ok {
		t.Fatal("undeclared identity was admitted")
	}
	invalid := NewVerifiedPolicyContext(map[string]string{"user_id": "not-a-uuid"}, nil)
	if _, ok := invalid.identity("user_id"); ok {
		t.Fatal("malformed UUID was admitted")
	}
}

func TestRuntimePredicateDereferencesNullablePolicyColumn(t *testing.T) {
	old := rowPolicyRuntimeRegistry
	rowPolicyRuntimeRegistry = map[string]rowPolicyRuntimeDefinition{"users": {Table: "users", IdentityTypes: map[string]string{"user_id": "uuid"}}}
	t.Cleanup(func() { rowPolicyRuntimeRegistry = old })
	value := "11111111-1111-4111-8111-111111111111"
	record := struct {
		WorkspaceID *string `db:"user_id"`
	}{WorkspaceID: &value}
	ctx := NewVerifiedPolicyContext(map[string]string{"user_id": value}, nil)
	predicate := rowPolicyRuntimePredicate{Kind: "equal", Children: []rowPolicyRuntimePredicate{{Kind: "column", Name: "user_id"}, {Kind: "identity", Name: "user_id"}}}
	allowed, err := evaluateRuntimePredicate(predicate, ctx, record)
	if err != nil || !allowed {
		t.Fatalf("nullable policy column allowed=%t err=%v", allowed, err)
	}
	record.WorkspaceID = nil
	allowed, err = evaluateRuntimePredicate(predicate, ctx, record)
	if err != nil || allowed {
		t.Fatalf("null policy column allowed=%t err=%v", allowed, err)
	}
}

func TestVerifiedPolicyContextCanonicalizesInt64AndSets(t *testing.T) {
	old := rowPolicyRuntimeRegistry
	rowPolicyRuntimeRegistry = map[string]rowPolicyRuntimeDefinition{"movements": {Table: "movements", IdentityTypes: map[string]string{"organization_id": "int64", "allowed_company_ids": "int64s"}}}
	t.Cleanup(func() { rowPolicyRuntimeRegistry = old })
	ctx := NewVerifiedPolicyContext(map[string]string{"organization_id": "-42", "allowed_company_ids": `[102,101,102,-4]`}, nil)
	if value, ok := ctx.identity("organization_id"); !ok || value != "-42" {
		t.Fatalf("organization identity=%q %t", value, ok)
	}
	if value, ok := ctx.identity("allowed_company_ids"); !ok || value != `[-4,101,102]` {
		t.Fatalf("company identities=%q %t", value, ok)
	}
	for name, value := range map[string]string{"leading_zero": "01", "plus": "+1", "overflow": "9223372036854775808"} {
		invalid := NewVerifiedPolicyContext(map[string]string{"organization_id": value}, nil)
		if _, ok := invalid.identity("organization_id"); ok {
			t.Fatalf("%s int64 was admitted", name)
		}
	}
}

func TestRuntimeInt64MembershipCompilesAndEvaluates(t *testing.T) {
	old := rowPolicyRuntimeRegistry
	rowPolicyRuntimeRegistry = map[string]rowPolicyRuntimeDefinition{"movements": {Table: "movements", IdentityTypes: map[string]string{"organization_id": "int64", "allowed_company_ids": "int64s"}}}
	t.Cleanup(func() { rowPolicyRuntimeRegistry = old })
	ctx := NewVerifiedPolicyContext(map[string]string{"organization_id": "10", "allowed_company_ids": `[101,102]`}, nil)
	predicate := rowPolicyRuntimePredicate{Kind: "and", Children: []rowPolicyRuntimePredicate{
		{Kind: "equal", Children: []rowPolicyRuntimePredicate{{Kind: "column", Name: "organization_id"}, {Kind: "identity", Name: "organization_id"}}},
		{Kind: "in", Children: []rowPolicyRuntimePredicate{{Kind: "column", Name: "suborganization_id"}, {Kind: "identity", Name: "allowed_company_ids"}}},
	}}
	record := struct {
		OrganizationID    int64 `db:"organization_id"`
		SuborganizationID int64 `db:"suborganization_id"`
	}{10, 102}
	allowed, err := evaluateRuntimePredicate(predicate, ctx, record)
	if err != nil || !allowed {
		t.Fatalf("allowed=%t err=%v", allowed, err)
	}
	record.SuborganizationID = 103
	allowed, err = evaluateRuntimePredicate(predicate, ctx, record)
	if err != nil || allowed {
		t.Fatalf("denied=%t err=%v", allowed, err)
	}
	sql, args, err := compileRuntimePredicate(predicate, "t", ctx)
	if err != nil || !strings.Contains(sql, `t."suborganization_id" IN (?,?)`) || len(args) != 3 || args[0] != int64(10) {
		t.Fatalf("sql=%s args=%#v err=%v", sql, args, err)
	}
}

func TestProposedIntegerPolicyColumnRejectsOutOfRangeValue(t *testing.T) {
	old := rowPolicyRuntimeRegistry
	rowPolicyRuntimeRegistry = map[string]rowPolicyRuntimeDefinition{"items": {Table: "items", IdentityTypes: map[string]string{"company_id": "int64"}}}
	t.Cleanup(func() { rowPolicyRuntimeRegistry = old })
	ctx := NewVerifiedPolicyContext(map[string]string{"company_id": "2147483648"}, nil)
	predicate := rowPolicyRuntimePredicate{Kind: "equal", Children: []rowPolicyRuntimePredicate{
		{Kind: "column", Name: "company_id", ColumnType: "integer"},
		{Kind: "identity", Name: "company_id"},
	}}
	record := struct {
		CompanyID int64 `db:"company_id"`
	}{CompanyID: 2147483648}
	allowed, err := evaluateRuntimePredicate(predicate, ctx, record)
	if err != nil || allowed {
		t.Fatalf("out-of-range integer allowed=%t err=%v", allowed, err)
	}
}
