package cooked

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/shortontech/pickle/pkg/generator"
	"github.com/shortontech/pickle/pkg/schema"
)

type postgresPolicyMessage struct {
	ID     string `db:"id"`
	UserID string `db:"user_id"`
}

func TestPostgresRowPolicyThreeLaneConformance(t *testing.T) {
	dsn := os.Getenv("PICKLE_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set PICKLE_POSTGRES_TEST_DSN to run PostgreSQL row-policy conformance")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	table, owner, runtimeRole, password := "pickle_policy_"+suffix, "pickle_owner_"+suffix, "pickle_runtime_"+suffix, "policy-test-"+suffix
	quote := func(v string) string { return `"` + strings.ReplaceAll(v, `"`, `""`) + `"` }
	statements := []string{
		"CREATE ROLE " + quote(owner) + " NOLOGIN",
		"CREATE ROLE " + quote(runtimeRole) + " LOGIN PASSWORD '" + password + "' NOSUPERUSER NOBYPASSRLS",
		"GRANT CREATE ON SCHEMA public TO " + quote(owner),
		"SET ROLE " + quote(owner),
		"CREATE TABLE " + quote(table) + " (id uuid PRIMARY KEY, user_id uuid)",
		"RESET ROLE",
		"GRANT SELECT ON " + quote(table) + " TO " + quote(runtimeRole),
		"INSERT INTO " + quote(table) + " VALUES ('11111111-1111-4111-8111-111111111111','aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'),('22222222-2222-4222-8222-222222222222','bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb')",
	}
	for _, stmt := range statements {
		if _, err := admin.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	t.Cleanup(func() {
		admin.Exec("DROP TABLE IF EXISTS " + quote(table) + " CASCADE")
		admin.Exec("DROP ROLE IF EXISTS " + quote(runtimeRole))
		admin.Exec("REVOKE CREATE ON SCHEMA public FROM " + quote(owner))
		admin.Exec("DROP ROLE IF EXISTS " + quote(owner))
	})
	predicate := schema.Equal(schema.PolicyColumn("user_id"), schema.Identity("user_id"))
	resolved := generator.ResolvedRowPolicy{Protection: schema.RowProtection{Table: table, SubjectCombination: schema.AnyOfSubjects, Rules: []schema.RowRule{{Key: "owner", Subject: schema.RowSubject{Kind: schema.SubjectPublic}, Select: &predicate}}}, EnforcementClass: "portable", Identities: map[string]schema.PolicyIdentityType{"user_id": schema.PolicyIdentityUUID}, PhysicalPlans: map[string]string{"select": "select"}}
	plans, err := generator.LowerPostgresRowPolicies([]generator.ResolvedRowPolicy{resolved})
	if err != nil {
		t.Fatal(err)
	}
	old := rowPolicyRuntimeRegistry
	rowPolicyRuntimeRegistry = map[string]rowPolicyRuntimeDefinition{}
	registerRowPolicyRuntime(rowPolicyRuntimeDefinition{Table: table, SubjectCombination: "any", IdentityTypes: map[string]string{"user_id": "uuid"}, Rules: []rowPolicyRuntimeRule{{Key: "owner", SubjectKind: "public", Select: &rowPolicyRuntimePredicate{Kind: "equal", Children: []rowPolicyRuntimePredicate{{Kind: "column", Name: "user_id"}, {Kind: "identity", Name: "user_id"}}}}}})
	t.Cleanup(func() { rowPolicyRuntimeRegistry = old })
	runtimeDSN := postgresRoleDSN(t, dsn, runtimeRole, password)
	runtimeDB, err := sql.Open("postgres", runtimeDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeDB.Close()
	ctx := NewVerifiedPolicyContext(map[string]string{"user_id": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}, nil)
	mismatch := NewVerifiedPolicyContext(map[string]string{"user_id": "cccccccc-cccc-4ccc-8ccc-cccccccccccc"}, nil)
	if _, err := admin.Exec("ALTER TABLE " + quote(table) + " DISABLE ROW LEVEL SECURITY"); err != nil {
		t.Fatal(err)
	}
	oldDB := DB
	DB = runtimeDB
	t.Cleanup(func() { DB = oldDB })
	rows, err := Query[postgresPolicyMessage](table).WithPolicyContext(ctx).All()
	if err != nil || len(rows) != 1 {
		t.Fatalf("application lane rows=%d err=%v", len(rows), err)
	}
	rows, err = Query[postgresPolicyMessage](table).WithPolicyContext(mismatch).All()
	if err != nil || len(rows) != 0 {
		t.Fatalf("application mismatch rows=%d err=%v", len(rows), err)
	}
	ddl := append([]string{}, generator.PostgresPolicyIdentityHelpers()...)
	ddl = append(ddl, "ALTER TABLE "+quote(table)+" ENABLE ROW LEVEL SECURITY", "ALTER TABLE "+quote(table)+" FORCE ROW LEVEL SECURITY")
	for _, policy := range plans[0].Policies {
		ddl = append(ddl, "CREATE POLICY "+quote(policy.Name)+" ON "+quote(table)+" FOR "+string(policy.Command)+" TO PUBLIC USING ("+policy.Using+")")
	}
	for _, stmt := range ddl {
		if _, err := admin.Exec(stmt); err != nil {
			t.Fatalf("RLS DDL %s: %v", stmt, err)
		}
	}
	var rlsCount int
	if err := withSQLPolicyContext(runtimeDB, ctx, func(tx *sql.Tx) error { return tx.QueryRow("SELECT count(*) FROM " + quote(table)).Scan(&rlsCount) }); err != nil || rlsCount != 1 {
		t.Fatalf("RLS lane count=%d err=%v", rlsCount, err)
	}
	if err := withSQLPolicyContext(runtimeDB, mismatch, func(tx *sql.Tx) error { return tx.QueryRow("SELECT count(*) FROM " + quote(table)).Scan(&rlsCount) }); err != nil || rlsCount != 0 {
		t.Fatalf("RLS mismatch count=%d err=%v", rlsCount, err)
	}
	err = TransactionOn(runtimeDB, func(tx *Tx) error {
		if err := tx.WithPostgresPolicyContext(ctx); err != nil {
			return err
		}
		rows, err := Query[postgresPolicyMessage](table).UseTransaction(tx.conn).WithPolicyContext(ctx).All()
		if err == nil && len(rows) != 1 {
			return fmt.Errorf("dual rows=%d", len(rows))
		}
		return err
	})
	if err != nil {
		t.Fatalf("dual lane: %v", err)
	}
	err = TransactionOn(runtimeDB, func(tx *Tx) error {
		if err := tx.WithPostgresPolicyContext(mismatch); err != nil {
			return err
		}
		rows, err := Query[postgresPolicyMessage](table).UseTransaction(tx.conn).WithPolicyContext(mismatch).All()
		if err == nil && len(rows) != 0 {
			return fmt.Errorf("dual mismatch rows=%d", len(rows))
		}
		return err
	})
	if err != nil {
		t.Fatalf("dual mismatch lane: %v", err)
	}
	if _, err := Query[postgresPolicyMessage](table).All(); err == nil {
		t.Fatal("application lane admitted missing context")
	}
	if err := runtimeDB.QueryRow("SELECT count(*) FROM " + quote(table)).Scan(&rlsCount); err != nil || rlsCount != 0 {
		t.Fatalf("RLS missing-context count=%d err=%v", rlsCount, err)
	}
	var superuser, bypass bool
	if err := runtimeDB.QueryRow("SELECT rolsuper,rolbypassrls FROM pg_roles WHERE rolname=current_user").Scan(&superuser, &bypass); err != nil || superuser || bypass {
		t.Fatalf("runtime privileges superuser=%t bypass=%t err=%v", superuser, bypass, err)
	}
	ownerTx, err := admin.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer ownerTx.Rollback()
	if _, err := ownerTx.Exec("SET LOCAL ROLE " + quote(owner)); err != nil {
		t.Fatal(err)
	}
	if err := ownerTx.QueryRow("SELECT count(*) FROM " + quote(table)).Scan(&rlsCount); err != nil || rlsCount != 0 {
		t.Fatalf("forced owner lane count=%d err=%v", rlsCount, err)
	}
}

func postgresRoleDSN(t *testing.T, dsn, user, password string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme == "" {
		t.Skip("PostgreSQL conformance DSN must be a URL")
	}
	u.User = url.UserPassword(user, password)
	return u.String()
}
func withSQLPolicyContext(db *sql.DB, ctx PolicyContext, fn func(*sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for name, value := range ctx.identities {
		if _, err := tx.Exec("SELECT set_config($1,$2,true)", "pickle.identity."+name, value); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("SELECT set_config($1,$2,true)", "pickle.identity.roles", ctx.encodedRoles()); err != nil {
		return err
	}
	return fn(tx)
}
