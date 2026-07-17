package cooked

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
type postgresTextPolicyMessage struct {
	ID        string `db:"id"`
	TenantKey string `db:"tenant_key"`
}
type postgresNullablePolicyMessage struct {
	ID     string  `db:"id"`
	UserID *string `db:"user_id"`
	Marker int     `db:"marker"`
}

type postgresTenantMovement struct {
	ID                int64  `db:"id"`
	OrganizationID    *int64 `db:"organization_id"`
	SuborganizationID *int64 `db:"suborganization_id"`
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
		"GRANT SELECT, INSERT, UPDATE, DELETE ON " + quote(table) + " TO " + quote(runtimeRole),
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
	resolved := generator.ResolvedRowPolicy{Protection: schema.RowProtection{Table: table, SubjectCombination: schema.AnyOfSubjects, Rules: []schema.RowRule{{Key: "owner", Subject: schema.RowSubject{Kind: schema.SubjectPublic}, Select: &predicate, Insert: &predicate, UpdateOld: &predicate, UpdateNew: &predicate, Delete: &predicate}}}, EnforcementClass: "portable", Identities: map[string]schema.PolicyIdentityType{"user_id": schema.PolicyIdentityUUID}, PhysicalPlans: map[string]string{"select": "select", "insert": "insert", "update": "update", "delete": "delete"}}
	plans, err := generator.LowerPostgresRowPolicies([]generator.ResolvedRowPolicy{resolved})
	if err != nil {
		t.Fatal(err)
	}
	old := rowPolicyRuntimeRegistry
	rowPolicyRuntimeRegistry = map[string]rowPolicyRuntimeDefinition{}
	runtimePredicate := &rowPolicyRuntimePredicate{Kind: "equal", Children: []rowPolicyRuntimePredicate{{Kind: "column", Name: "user_id"}, {Kind: "identity", Name: "user_id"}}}
	registerRowPolicyRuntime(rowPolicyRuntimeDefinition{Table: table, SubjectCombination: "any", IdentityTypes: map[string]string{"user_id": "uuid"}, Rules: []rowPolicyRuntimeRule{{Key: "owner", SubjectKind: "public", Select: runtimePredicate, Insert: runtimePredicate, UpdateOld: runtimePredicate, UpdateNew: runtimePredicate, Delete: runtimePredicate}}})
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
	appRecord := &postgresPolicyMessage{ID: "33333333-3333-4333-8333-333333333333", UserID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}
	if err := Query[postgresPolicyMessage](table).WithPolicyContext(ctx).Create(appRecord); err != nil {
		t.Fatalf("application insert: %v", err)
	}
	deniedRecord := &postgresPolicyMessage{ID: "44444444-4444-4444-8444-444444444444", UserID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"}
	if err := Query[postgresPolicyMessage](table).WithPolicyContext(ctx).Create(deniedRecord); err == nil {
		t.Fatal("application insert admitted mismatched row")
	}
	appRecord.UserID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	if err := Query[postgresPolicyMessage](table).WithPolicyContext(ctx).where("id", appRecord.ID).Update(appRecord); err == nil {
		t.Fatal("application update admitted ownership transfer")
	}
	appRecord.UserID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if err := Query[postgresPolicyMessage](table).WithPolicyContext(ctx).where("id", appRecord.ID).Delete(appRecord); err != nil {
		t.Fatalf("application delete: %v", err)
	}
	ddl := append([]string{}, generator.PostgresPolicyIdentityHelpers()...)
	ddl = append(ddl, "ALTER TABLE "+quote(table)+" ENABLE ROW LEVEL SECURITY", "ALTER TABLE "+quote(table)+" FORCE ROW LEVEL SECURITY")
	for _, policy := range plans[0].Policies {
		statement := "CREATE POLICY " + quote(policy.Name) + " ON " + quote(table) + " FOR " + string(policy.Command) + " TO PUBLIC"
		if policy.Using != "" {
			statement += " USING (" + policy.Using + ")"
		}
		if policy.WithCheck != "" {
			statement += " WITH CHECK (" + policy.WithCheck + ")"
		}
		ddl = append(ddl, statement)
	}
	for _, stmt := range ddl {
		if _, err := admin.Exec(stmt); err != nil {
			t.Fatalf("RLS DDL %s: %v", stmt, err)
		}
	}
	var rlsCount int
	if err := withSQLPolicyContext(runtimeDB, ctx, func(tx *sql.Tx) error { return tx.QueryRow("SELECT count(*) FROM " + quote(table)).Scan(&rlsCount) }); err != nil || rlsCount != 1 {
		t.Fatalf("RLS lane count=%d err=%v policies=%+v", rlsCount, err, plans[0].Policies)
	}
	if err := withSQLPolicyContext(runtimeDB, mismatch, func(tx *sql.Tx) error { return tx.QueryRow("SELECT count(*) FROM " + quote(table)).Scan(&rlsCount) }); err != nil || rlsCount != 0 {
		t.Fatalf("RLS mismatch count=%d err=%v", rlsCount, err)
	}
	if err := withSQLPolicyContext(runtimeDB, ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO " + quote(table) + " VALUES ('55555555-5555-4555-8555-555555555555','aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa')")
		return err
	}); err != nil {
		t.Fatalf("RLS insert: %v", err)
	}
	if err := withSQLPolicyContext(runtimeDB, ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("UPDATE " + quote(table) + " SET user_id='bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb' WHERE id='11111111-1111-4111-8111-111111111111'")
		if err == nil {
			return fmt.Errorf("RLS admitted ownership transfer")
		}
		return nil
	}); err != nil {
		t.Fatalf("RLS update check: %v", err)
	}
	if err := withSQLPolicyContext(runtimeDB, ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("DELETE FROM " + quote(table) + " WHERE id='11111111-1111-4111-8111-111111111111'")
		return err
	}); err != nil {
		t.Fatalf("RLS delete: %v", err)
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
	dualRecord := &postgresPolicyMessage{ID: "66666666-6666-4666-8666-666666666666", UserID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}
	err = TransactionOn(runtimeDB, func(tx *Tx) error {
		if err := tx.WithPostgresPolicyContext(ctx); err != nil {
			return err
		}
		q := Query[postgresPolicyMessage](table).UseTransaction(tx.conn).WithPolicyContext(ctx)
		if err := q.Create(dualRecord); err != nil {
			return err
		}
		if err := Query[postgresPolicyMessage](table).UseTransaction(tx.conn).WithPolicyContext(ctx).where("id", dualRecord.ID).Update(dualRecord); err != nil {
			return err
		}
		return Query[postgresPolicyMessage](table).UseTransaction(tx.conn).WithPolicyContext(ctx).where("id", dualRecord.ID).Delete(dualRecord)
	})
	if err != nil {
		t.Fatalf("dual write matrix: %v", err)
	}
	dualDenied := &postgresPolicyMessage{ID: "77777777-7777-4777-8777-777777777777", UserID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"}
	err = TransactionOn(runtimeDB, func(tx *Tx) error {
		if err := tx.WithPostgresPolicyContext(ctx); err != nil {
			return err
		}
		return Query[postgresPolicyMessage](table).UseTransaction(tx.conn).WithPolicyContext(ctx).Create(dualDenied)
	})
	if err == nil {
		t.Fatal("dual lane admitted mismatched insert")
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
	if err := ownerTx.Rollback(); err != nil {
		t.Fatal(err)
	}
	runPostgresPredicateCorpus(t, admin, runtimeDB, table, quote)
	runPostgresOperationPositionCorpus(t, admin, runtimeDB, table, quote)
	runPostgresSubjectCorpus(t, admin, runtimeDB, table, quote)
	runPostgresPolicyTransitionConformance(t, admin, runtimeDB, table, quote)
	runPostgresTextIdentityConformance(t, admin, runtimeDB, table+"_text", quote)
	runPostgresDillTenantConformance(t, admin, runtimeDB, table+"_tenant", quote)
}

func runPostgresDillTenantConformance(t *testing.T, admin, runtimeDB *sql.DB, table string, quote func(string) string) {
	t.Helper()
	for _, stmt := range []string{
		"CREATE TABLE " + quote(table) + " (id bigint PRIMARY KEY, organization_id bigint, suborganization_id bigint)",
		"GRANT SELECT, INSERT, UPDATE, DELETE ON " + quote(table) + " TO PUBLIC",
		"INSERT INTO " + quote(table) + " VALUES (1,10,101),(2,10,102),(3,10,103),(4,11,101),(5,11,103),(6,NULL,101),(7,10,NULL)",
	} {
		if _, err := admin.Exec(stmt); err != nil {
			t.Fatalf("tenant fixture %s: %v", stmt, err)
		}
	}
	t.Cleanup(func() { admin.Exec("DROP TABLE IF EXISTS " + quote(table) + " CASCADE") })

	organization := schema.Equal(schema.PolicyColumn("organization_id"), schema.Identity("organization_id"))
	companies := schema.In(schema.PolicyColumn("suborganization_id"), schema.Identity("allowed_company_ids"))
	predicate := schema.And(organization, companies)
	resolved := generator.ResolvedRowPolicy{
		Protection:       schema.RowProtection{Table: table, SubjectCombination: schema.AnyOfSubjects, Rules: []schema.RowRule{{Key: "tenant", Subject: schema.RowSubject{Kind: schema.SubjectPublic}, Select: &predicate, Insert: &predicate, UpdateOld: &predicate, UpdateNew: &predicate, Delete: &predicate}}},
		EnforcementClass: "portable",
		Identities:       map[string]schema.PolicyIdentityType{"organization_id": schema.PolicyIdentityInt64, "allowed_company_ids": schema.PolicyIdentityInt64s},
		PhysicalPlans:    map[string]string{"select": "select", "insert": "insert", "update": "update", "delete": "delete"},
	}
	plans, err := generator.LowerPostgresRowPolicies([]generator.ResolvedRowPolicy{resolved})
	if err != nil {
		t.Fatal(err)
	}
	runtimePredicate := postgresTestRuntimePredicate(predicate)
	registerRowPolicyRuntime(rowPolicyRuntimeDefinition{Table: table, SubjectCombination: "any", IdentityTypes: map[string]string{"organization_id": "int64", "allowed_company_ids": "int64s"}, Rules: []rowPolicyRuntimeRule{{Key: "tenant", SubjectKind: "public", Select: runtimePredicate, Insert: runtimePredicate, UpdateOld: runtimePredicate, UpdateNew: runtimePredicate, Delete: runtimePredicate}}})
	ctx := NewVerifiedPolicyContext(map[string]string{"organization_id": "10", "allowed_company_ids": `[102,101,101]`}, nil)

	if _, err := admin.Exec("ALTER TABLE " + quote(table) + " DISABLE ROW LEVEL SECURITY"); err != nil {
		t.Fatal(err)
	}
	rows, err := Query[postgresTenantMovement](table).WithPolicyContext(ctx).All()
	if err != nil || len(rows) != 2 {
		t.Fatalf("tenant application lane rows=%d err=%v", len(rows), err)
	}
	if count, err := Query[postgresTenantMovement](table).WithPolicyContext(ctx).Count(); err != nil || count != 2 {
		t.Fatalf("tenant application count=%d err=%v", count, err)
	}
	if row, err := Query[postgresTenantMovement](table).WithPolicyContext(ctx).where("id", int64(1)).First(); err != nil || row == nil || row.ID != 1 {
		t.Fatalf("tenant application first=%#v err=%v", row, err)
	}
	org, allowed, denied := int64(10), int64(101), int64(103)
	appRecord := &postgresTenantMovement{ID: 8, OrganizationID: &org, SuborganizationID: &allowed}
	if err := Query[postgresTenantMovement](table).WithPolicyContext(ctx).Create(appRecord); err != nil {
		t.Fatalf("tenant application insert: %v", err)
	}
	if err := Query[postgresTenantMovement](table).WithPolicyContext(ctx).Create(&postgresTenantMovement{ID: 9, OrganizationID: &org, SuborganizationID: &denied}); err == nil {
		t.Fatal("tenant application admitted disallowed insert")
	}
	company102 := int64(102)
	appRecord.SuborganizationID = &company102
	if err := Query[postgresTenantMovement](table).WithPolicyContext(ctx).where("id", appRecord.ID).Update(appRecord); err != nil {
		t.Fatalf("tenant application allowed update: %v", err)
	}
	appRecord.SuborganizationID = &denied
	if err := Query[postgresTenantMovement](table).WithPolicyContext(ctx).where("id", appRecord.ID).Update(appRecord); err == nil {
		t.Fatal("tenant application admitted disallowed transfer")
	}
	appRecord.SuborganizationID = &company102
	if err := Query[postgresTenantMovement](table).WithPolicyContext(ctx).where("id", appRecord.ID).Delete(appRecord); err != nil {
		t.Fatalf("tenant application delete: %v", err)
	}
	for _, malformed := range []map[string]string{
		{"organization_id": "+10", "allowed_company_ids": `[101]`},
		{"organization_id": "10", "allowed_company_ids": `[101,1e2]`},
		{"organization_id": "9223372036854775808", "allowed_company_ids": `[101]`},
	} {
		if _, err := Query[postgresTenantMovement](table).WithPolicyContext(NewVerifiedPolicyContext(malformed, nil)).All(); err == nil {
			t.Fatalf("malformed tenant context did not fail closed: %#v", malformed)
		}
	}

	for _, stmt := range append(generator.PostgresPolicyIdentityHelpers(), "ALTER TABLE "+quote(table)+" ENABLE ROW LEVEL SECURITY", "ALTER TABLE "+quote(table)+" FORCE ROW LEVEL SECURITY") {
		if _, err := admin.Exec(stmt); err != nil {
			t.Fatalf("tenant RLS DDL %s: %v", stmt, err)
		}
	}
	var numericCorpus struct {
		Cases []struct {
			ID, Kind, Input, Canonical string
			Valid                      bool
		} `json:"cases"`
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "row-policy-conformance", "numeric_identities.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &numericCorpus); err != nil {
		t.Fatal(err)
	}
	for _, test := range numericCorpus.Cases {
		t.Run("postgres_canonical_"+test.ID, func(t *testing.T) {
			setting, expression := "pickle.identity.organization_id", "pickle_identity_int64('organization_id')::text"
			if test.Kind == "int64s" {
				setting, expression = "pickle.identity.allowed_company_ids", "array_to_json(pickle_identity_int64s('allowed_company_ids'))::text"
			}
			tx, err := runtimeDB.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err := tx.Exec("SELECT set_config($1,$2,true)", setting, test.Input); err != nil {
				t.Fatal(err)
			}
			var got sql.NullString
			if err := tx.QueryRow("SELECT " + expression).Scan(&got); err != nil {
				t.Fatalf("helper raised for input classification %s: %v", test.ID, err)
			}
			if got.Valid != test.Valid || (got.Valid && got.String != test.Canonical) {
				t.Fatalf("helper canonical=%q valid=%t want=%q/%t", got.String, got.Valid, test.Canonical, test.Valid)
			}
		})
	}
	for _, policy := range plans[0].Policies {
		stmt := "CREATE POLICY " + quote(policy.Name) + " ON " + quote(table) + " FOR " + string(policy.Command) + " TO PUBLIC"
		if policy.Using != "" {
			stmt += " USING (" + policy.Using + ")"
		}
		if policy.WithCheck != "" {
			stmt += " WITH CHECK (" + policy.WithCheck + ")"
		}
		if _, err := admin.Exec(stmt); err != nil {
			t.Fatalf("tenant policy %s: %v", stmt, err)
		}
	}
	var count int
	if err := withSQLPolicyContext(runtimeDB, ctx, func(tx *sql.Tx) error { return tx.QueryRow("SELECT count(*) FROM " + quote(table)).Scan(&count) }); err != nil || count != 2 {
		t.Fatalf("tenant RLS lane count=%d err=%v", count, err)
	}
	contexts := []struct {
		organization, companies string
		want                    int
	}{{"10", `[101]`, 1}, {"11", `[103]`, 1}}
	var wg sync.WaitGroup
	errors := make(chan error, len(contexts))
	for _, test := range contexts {
		test := test
		wg.Add(1)
		go func() {
			defer wg.Done()
			isolated := NewVerifiedPolicyContext(map[string]string{"organization_id": test.organization, "allowed_company_ids": test.companies}, nil)
			var got int
			err := withSQLPolicyContext(runtimeDB, isolated, func(tx *sql.Tx) error {
				return tx.QueryRow("SELECT count(*) FROM " + quote(table)).Scan(&got)
			})
			if err != nil || got != test.want {
				errors <- fmt.Errorf("context %s/%s count=%d want=%d err=%v", test.organization, test.companies, got, test.want, err)
			}
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	if err := runtimeDB.QueryRow("SELECT count(*) FROM " + quote(table)).Scan(&count); err != nil || count != 0 {
		t.Fatalf("pooled integer policy context leaked count=%d err=%v", count, err)
	}
	if err := withSQLPolicyContext(runtimeDB, ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO " + quote(table) + " VALUES (10,10,101)")
		return err
	}); err != nil {
		t.Fatalf("tenant RLS allowed insert: %v", err)
	}
	if err := withSQLPolicyContext(runtimeDB, ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO " + quote(table) + " VALUES (11,10,103)")
		return err
	}); err == nil {
		t.Fatal("RLS admitted disallowed tenant insert")
	}
	if err := withSQLPolicyContext(runtimeDB, ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("UPDATE " + quote(table) + " SET suborganization_id=102 WHERE id=1")
		return err
	}); err != nil {
		t.Fatalf("tenant RLS allowed update: %v", err)
	}
	if err := withSQLPolicyContext(runtimeDB, ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("UPDATE " + quote(table) + " SET suborganization_id=103 WHERE id=1")
		return err
	}); err == nil {
		t.Fatal("RLS admitted disallowed tenant transfer")
	}
	if err := withSQLPolicyContext(runtimeDB, ctx, func(tx *sql.Tx) error { _, err := tx.Exec("DELETE FROM " + quote(table) + " WHERE id=2"); return err }); err != nil {
		t.Fatalf("tenant RLS delete: %v", err)
	}
	for _, malformed := range [][2]string{{"+10", `[101]`}, {"10", `[101,1e2]`}, {"9223372036854775808", `[101]`}} {
		tx, err := runtimeDB.Begin()
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec("SELECT set_config('pickle.identity.organization_id',$1,true), set_config('pickle.identity.allowed_company_ids',$2,true)", malformed[0], malformed[1])
		if err == nil {
			err = tx.QueryRow("SELECT count(*) FROM " + quote(table)).Scan(&count)
		}
		rollbackErr := tx.Rollback()
		if err != nil || rollbackErr != nil || count != 0 {
			t.Fatalf("malformed direct RLS context=%q/%q count=%d err=%v rollback=%v", malformed[0], malformed[1], count, err, rollbackErr)
		}
	}
	if err := TransactionOn(runtimeDB, func(tx *Tx) error {
		if err := tx.WithPostgresPolicyContext(ctx); err != nil {
			return err
		}
		rows, err := Query[postgresTenantMovement](table).UseTransaction(tx.conn).WithPolicyContext(ctx).All()
		if err == nil && len(rows) != 2 {
			return fmt.Errorf("tenant dual rows=%d", len(rows))
		}
		return err
	}); err != nil {
		t.Fatalf("tenant dual lane: %v", err)
	}
	dualRecord := &postgresTenantMovement{ID: 12, OrganizationID: &org, SuborganizationID: &allowed}
	if err := TransactionOn(runtimeDB, func(tx *Tx) error {
		if err := tx.WithPostgresPolicyContext(ctx); err != nil {
			return err
		}
		q := Query[postgresTenantMovement](table).UseTransaction(tx.conn).WithPolicyContext(ctx)
		if err := q.Create(dualRecord); err != nil {
			return err
		}
		dualRecord.SuborganizationID = &company102
		if err := Query[postgresTenantMovement](table).UseTransaction(tx.conn).WithPolicyContext(ctx).where("id", dualRecord.ID).Update(dualRecord); err != nil {
			return err
		}
		return Query[postgresTenantMovement](table).UseTransaction(tx.conn).WithPolicyContext(ctx).where("id", dualRecord.ID).Delete(dualRecord)
	}); err != nil {
		t.Fatalf("tenant dual write matrix: %v", err)
	}

	if err := evaluateRowPolicyRecord(table, "insert", &ctx, &postgresTenantMovement{ID: 8, OrganizationID: &org, SuborganizationID: &allowed}); err != nil {
		t.Fatalf("tenant proposed row denied: %v", err)
	}
	if err := evaluateRowPolicyRecord(table, "insert", &ctx, &postgresTenantMovement{ID: 9, OrganizationID: &org, SuborganizationID: &denied}); err == nil {
		t.Fatal("tenant proposed row admitted disallowed company")
	}
}

func runPostgresOperationPositionCorpus(t *testing.T, admin, runtimeDB *sql.DB, table string, quote func(string) string) {
	t.Helper()
	if _, err := admin.Exec("ALTER TABLE " + quote(table) + " ADD COLUMN marker integer NOT NULL DEFAULT 0"); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec("ALTER TABLE " + quote(table) + " DROP COLUMN marker")
	type corpusCase struct {
		ID, Predicate, Identity, Row string
		Decision                     bool
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "row-policy-conformance", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []corpusCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	related := table + "_operation_memberships"
	if _, err := admin.Exec("CREATE TABLE " + quote(related) + " (parent_id uuid, user_id uuid)"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("GRANT SELECT ON " + quote(related) + " TO PUBLIC"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec("DROP TABLE IF EXISTS " + quote(related) + " CASCADE") })
	matching := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	different := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	rowID := "77777777-7777-4777-8777-777777777777"
	insertID := "66666666-6666-4666-8666-666666666666"
	positions := []string{"insert", "update_old", "update_new", "delete"}

	for _, tc := range cases {
		for _, position := range positions {
			if tc.Predicate == "exists" && (position == "insert" || position == "update_new") {
				continue
			}
			t.Run("position_"+tc.ID+"_"+position, func(t *testing.T) {
				if _, err := admin.Exec("ALTER TABLE " + quote(table) + " DISABLE ROW LEVEL SECURITY"); err != nil {
					t.Fatal(err)
				}
				rows, err := admin.Query("SELECT policyname FROM pg_policies WHERE schemaname='public' AND tablename=$1", table)
				if err != nil {
					t.Fatal(err)
				}
				var names []string
				for rows.Next() {
					var name string
					if err := rows.Scan(&name); err != nil {
						t.Fatal(err)
					}
					names = append(names, name)
				}
				rows.Close()
				for _, name := range names {
					if _, err := admin.Exec("DROP POLICY " + quote(name) + " ON " + quote(table)); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := admin.Exec("DELETE FROM " + quote(table)); err != nil {
					t.Fatal(err)
				}
				if _, err := admin.Exec("DELETE FROM " + quote(related)); err != nil {
					t.Fatal(err)
				}
				rowValue := matching
				if tc.Row == "different" {
					rowValue = different
				}
				var stored any = rowValue
				if tc.Row == "null" {
					stored = nil
				}
				existingValue := stored
				if position == "update_new" {
					existingValue = matching
				}
				if position != "insert" {
					if _, err := admin.Exec("INSERT INTO "+quote(table)+" (id,user_id) VALUES ($1,$2)", rowID, existingValue); err != nil {
						t.Fatal(err)
					}
				}
				if tc.Predicate == "exists" && tc.Row == "related_matching" {
					if _, err := admin.Exec("INSERT INTO "+quote(related)+" VALUES ($1,$2)", rowID, matching); err != nil {
						t.Fatal(err)
					}
				}

				base := schema.Equal(schema.PolicyColumn("user_id"), schema.Identity("user_id"))
				predicate := base
				switch tc.Predicate {
				case "allow":
					predicate = schema.Allow()
				case "deny":
					predicate = schema.Deny()
				case "not_equal":
					predicate = schema.NotEqual(schema.PolicyColumn("user_id"), schema.Identity("user_id"))
				case "and":
					predicate = schema.And(base, schema.Allow())
				case "or":
					predicate = schema.Or(base, schema.Deny())
				case "not":
					predicate = schema.Not(base)
				case "exists":
					predicate = schema.Exists("memberships", base)
					predicate.RelatedTable, predicate.LocalColumn, predicate.ForeignColumn = related, "id", "parent_id"
				}
				ctxIDs := map[string]string{"user_id": matching}
				if tc.Identity == "missing" {
					ctxIDs = nil
				}
				ctx := NewVerifiedPolicyContext(ctxIDs, nil)
				runtimePredicate := postgresTestRuntimePredicate(predicate)
				runtimeRule := rowPolicyRuntimeRule{Key: "matrix", SubjectKind: "public"}
				schemaRule := schema.RowRule{Key: "matrix", Subject: schema.RowSubject{Kind: schema.SubjectPublic}}
				switch position {
				case "insert":
					runtimeRule.Insert, schemaRule.Insert = runtimePredicate, &predicate
				case "update_old":
					allowRuntime := postgresTestRuntimePredicate(schema.Allow())
					allowSchema := schema.Allow()
					runtimeRule.UpdateOld, runtimeRule.UpdateNew = runtimePredicate, allowRuntime
					schemaRule.UpdateOld, schemaRule.UpdateNew = &predicate, &allowSchema
				case "update_new":
					allowRuntime := postgresTestRuntimePredicate(schema.Allow())
					allowSchema := schema.Allow()
					runtimeRule.UpdateOld, runtimeRule.UpdateNew = allowRuntime, runtimePredicate
					schemaRule.UpdateOld, schemaRule.UpdateNew = &allowSchema, &predicate
				case "delete":
					runtimeRule.Delete, schemaRule.Delete = runtimePredicate, &predicate
				}
				rowPolicyRuntimeRegistry = map[string]rowPolicyRuntimeDefinition{}
				registerRowPolicyRuntime(rowPolicyRuntimeDefinition{Table: table, SubjectCombination: "any", IdentityTypes: map[string]string{"user_id": "uuid"}, Rules: []rowPolicyRuntimeRule{runtimeRule}})
				applicationAllowed := runApplicationPolicyOperation(runtimeDB, table, position, rowID, insertID, stored, ctx)
				if applicationAllowed != tc.Decision {
					t.Fatalf("application decision=%t want %t", applicationAllowed, tc.Decision)
				}

				operation := position
				if strings.HasPrefix(position, "update_") {
					operation = "update"
				}
				resolved := generator.ResolvedRowPolicy{Protection: schema.RowProtection{Table: table, SubjectCombination: schema.AnyOfSubjects, Rules: []schema.RowRule{schemaRule}}, EnforcementClass: "portable", Identities: map[string]schema.PolicyIdentityType{"user_id": schema.PolicyIdentityUUID}, PhysicalPlans: map[string]string{operation: operation}}
				plans, err := generator.LowerPostgresRowPolicies([]generator.ResolvedRowPolicy{resolved})
				if err != nil {
					t.Fatal(err)
				}
				for _, policy := range plans[0].Policies {
					statement := "CREATE POLICY " + quote(policy.Name) + " ON " + quote(table) + " FOR " + string(policy.Command) + " TO PUBLIC"
					if policy.Using != "" {
						statement += " USING (" + policy.Using + ")"
					}
					if policy.WithCheck != "" {
						statement += " WITH CHECK (" + policy.WithCheck + ")"
					}
					if _, err := admin.Exec(statement); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := admin.Exec("CREATE POLICY " + quote("pickle_conformance_select") + " ON " + quote(table) + " FOR SELECT TO PUBLIC USING (TRUE)"); err != nil {
					t.Fatal(err)
				}
				if _, err := admin.Exec("ALTER TABLE " + quote(table) + " ENABLE ROW LEVEL SECURITY"); err != nil {
					t.Fatal(err)
				}
				if _, err := admin.Exec("ALTER TABLE " + quote(table) + " FORCE ROW LEVEL SECURITY"); err != nil {
					t.Fatal(err)
				}
				if got := runDirectPolicyOperation(runtimeDB, table, position, rowID, insertID, stored, ctx); got != tc.Decision {
					t.Fatalf("RLS decision=%t want %t", got, tc.Decision)
				}
				if got := runDualPolicyOperation(runtimeDB, table, position, rowID, insertID, stored, ctx); got != tc.Decision {
					t.Fatalf("dual decision=%t want %t", got, tc.Decision)
				}
			})
		}
	}
}

func runApplicationPolicyOperation(db *sql.DB, table, position, rowID, insertID string, value any, ctx PolicyContext) bool {
	tx, err := db.Begin()
	if err != nil {
		return false
	}
	defer tx.Rollback()
	q := Query[postgresNullablePolicyMessage](table).UseTransaction(tx).WithPolicyContext(ctx)
	record := &postgresNullablePolicyMessage{ID: rowID}
	if text, ok := value.(string); ok {
		record.UserID = &text
	}
	switch position {
	case "insert":
		return evaluateRowPolicyRecord(table, "insert", &ctx, record) == nil
	case "update_new":
		return evaluateRowPolicyRecord(table, "update_new", &ctx, record) == nil
	case "update_old", "delete":
		operation := position
		if err := q.preparePolicy(operation); err != nil {
			return false
		}
		query, args := q.where("id", rowID).Limit(1).buildSelect()
		var found postgresNullablePolicyMessage
		return tx.QueryRow(query, args...).Scan(dbScanDest(&found)...) == nil
	}
	return false
}

func runDirectPolicyOperation(db *sql.DB, table, position, rowID, insertID string, value any, ctx PolicyContext) bool {
	tx, err := db.Begin()
	if err != nil {
		return false
	}
	defer tx.Rollback()
	if err := setSQLPolicyContext(tx, ctx); err != nil {
		return false
	}
	var result sql.Result
	switch position {
	case "insert":
		result, err = tx.Exec("INSERT INTO "+quoteRuntimeQualifiedIdent(table)+" (id,user_id) VALUES ($1,$2)", insertID, value)
	case "update_old", "update_new":
		result, err = tx.Exec("UPDATE "+quoteRuntimeQualifiedIdent(table)+" SET user_id=$1, marker=1 WHERE id=$2", value, rowID)
	case "delete":
		result, err = tx.Exec("DELETE FROM "+quoteRuntimeQualifiedIdent(table)+" WHERE id=$1", rowID)
	}
	if err != nil {
		return false
	}
	rows, _ := result.RowsAffected()
	return rows > 0
}

func runDualPolicyOperation(db *sql.DB, table, position, rowID, insertID string, value any, ctx PolicyContext) bool {
	tx, err := db.Begin()
	if err != nil {
		return false
	}
	defer tx.Rollback()
	wrapped := &Tx{conn: tx}
	if err := wrapped.WithPostgresPolicyContext(ctx); err != nil {
		return false
	}
	q := Query[postgresNullablePolicyMessage](table).UseTransaction(tx).WithPolicyContext(ctx)
	record := &postgresNullablePolicyMessage{ID: rowID, Marker: 1}
	if text, ok := value.(string); ok {
		record.UserID = &text
	}
	switch position {
	case "insert":
		record.ID = insertID
		return q.Create(record) == nil
	case "update_old", "update_new":
		if q.where("id", rowID).Update(record) != nil {
			return false
		}
		var marker int
		return tx.QueryRow("SELECT marker FROM "+quoteRuntimeQualifiedIdent(table)+" WHERE id=$1", rowID).Scan(&marker) == nil && marker == 1
	case "delete":
		if q.where("id", rowID).Delete(record) != nil {
			return false
		}
		var count int
		return tx.QueryRow("SELECT count(*) FROM "+quoteRuntimeQualifiedIdent(table)+" WHERE id=$1", rowID).Scan(&count) == nil && count == 0
	}
	return false
}

func runPostgresTextIdentityConformance(t *testing.T, admin, runtimeDB *sql.DB, table string, quote func(string) string) {
	t.Helper()
	if _, err := admin.Exec("CREATE TABLE " + quote(table) + " (id uuid PRIMARY KEY, tenant_key text)"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec("DROP TABLE IF EXISTS " + quote(table) + " CASCADE") })
	if _, err := admin.Exec("GRANT SELECT ON " + quote(table) + " TO PUBLIC"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("INSERT INTO " + quote(table) + " VALUES ('aaaaaaaa-1111-4111-8111-111111111111','tenant-a'),('bbbbbbbb-2222-4222-8222-222222222222','tenant-b')"); err != nil {
		t.Fatal(err)
	}
	predicate := schema.Equal(schema.PolicyColumn("tenant_key"), schema.Identity("tenant_key"))
	runtimePredicate := postgresTestRuntimePredicate(predicate)
	rowPolicyRuntimeRegistry = map[string]rowPolicyRuntimeDefinition{}
	registerRowPolicyRuntime(rowPolicyRuntimeDefinition{Table: table, SubjectCombination: "any", IdentityTypes: map[string]string{"tenant_key": "string"}, Rules: []rowPolicyRuntimeRule{{Key: "tenant", SubjectKind: "public", Select: runtimePredicate}}})
	ctx := NewVerifiedPolicyContext(map[string]string{"tenant_key": "tenant-a"}, nil)
	rows, err := Query[postgresTextPolicyMessage](table).WithPolicyContext(ctx).All()
	if err != nil || len(rows) != 1 {
		t.Fatalf("text application rows=%d err=%v", len(rows), err)
	}
	resolved := generator.ResolvedRowPolicy{Protection: schema.RowProtection{Table: table, SubjectCombination: schema.AnyOfSubjects, Rules: []schema.RowRule{{Key: "tenant", Subject: schema.RowSubject{Kind: schema.SubjectPublic}, Select: &predicate}}}, EnforcementClass: "portable", Identities: map[string]schema.PolicyIdentityType{"tenant_key": schema.PolicyIdentityString}, PhysicalPlans: map[string]string{"select": "select"}}
	plans, err := generator.LowerPostgresRowPolicies([]generator.ResolvedRowPolicy{resolved})
	if err != nil {
		t.Fatal(err)
	}
	policy := plans[0].Policies[0]
	for _, statement := range generator.PostgresPolicyIdentityHelpers() {
		if _, err := admin.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := admin.Exec("ALTER TABLE " + quote(table) + " ENABLE ROW LEVEL SECURITY"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("ALTER TABLE " + quote(table) + " FORCE ROW LEVEL SECURITY"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("CREATE POLICY " + quote(policy.Name) + " ON " + quote(table) + " FOR SELECT TO PUBLIC USING (" + policy.Using + ")"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := withSQLPolicyContext(runtimeDB, ctx, func(tx *sql.Tx) error { return tx.QueryRow("SELECT count(*) FROM " + quote(table)).Scan(&count) }); err != nil || count != 1 {
		t.Fatalf("text RLS count=%d err=%v", count, err)
	}
	err = TransactionOn(runtimeDB, func(tx *Tx) error {
		if err := tx.WithPostgresPolicyContext(ctx); err != nil {
			return err
		}
		rows, err := Query[postgresTextPolicyMessage](table).UseTransaction(tx.conn).WithPolicyContext(ctx).All()
		if err == nil && len(rows) != 1 {
			return fmt.Errorf("text dual rows=%d", len(rows))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func runPostgresPolicyTransitionConformance(t *testing.T, admin, runtimeDB *sql.DB, table string, quote func(string) string) {
	t.Helper()
	var names []string
	rows, err := admin.Query("SELECT policyname FROM pg_policies WHERE schemaname='public' AND tablename=$1", table)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	rows.Close()
	for _, name := range names {
		if _, err := admin.Exec("DROP POLICY " + quote(name) + " ON " + quote(table)); err != nil {
			t.Fatal(err)
		}
	}
	policyName := "pickle_transition_" + table
	if len(policyName) > 63 {
		policyName = policyName[:63]
	}
	usingTrue := "pickle_identity_uuid('user_id') IS NOT NULL"
	if _, err := admin.Exec("CREATE POLICY " + quote(policyName) + " ON " + quote(table) + " FOR SELECT TO PUBLIC USING (" + usingTrue + ")"); err != nil {
		t.Fatal(err)
	}
	ctx := NewVerifiedPolicyContext(map[string]string{"user_id": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}, nil)
	countRowsResult := func() (int, error) {
		var count int
		err := withSQLPolicyContext(runtimeDB, ctx, func(tx *sql.Tx) error { return tx.QueryRow("SELECT count(*) FROM " + quote(table)).Scan(&count) })
		return count, err
	}
	countRows := func() int {
		count, err := countRowsResult()
		if err != nil {
			t.Fatal(err)
		}
		return count
	}
	if got := countRows(); got != 1 {
		t.Fatalf("initial transition visibility=%d", got)
	}
	transition, err := admin.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transition.Exec("DROP POLICY " + quote(policyName) + " ON " + quote(table)); err != nil {
		t.Fatal(err)
	}
	if _, err := transition.Exec("CREATE POLICY " + quote(policyName) + " ON " + quote(table) + " FOR SELECT TO PUBLIC USING (FALSE)"); err != nil {
		t.Fatal(err)
	}
	// PostgreSQL blocks a concurrent statement while transactional policy DDL
	// is pending; it cannot observe a dropped or partially-created policy.
	type result struct {
		count int
		err   error
	}
	done := make(chan result, 1)
	go func() { count, err := countRowsResult(); done <- result{count, err} }()
	select {
	case got := <-done:
		transition.Rollback()
		t.Fatalf("concurrent query did not wait for transition: %+v", got)
	case <-time.After(100 * time.Millisecond):
	}
	if err := transition.Commit(); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil || got.count != 0 {
		t.Fatalf("concurrent committed transition=%+v", got)
	}
	if got := countRows(); got != 0 {
		t.Fatalf("committed transition visibility=%d", got)
	}
	rollback, err := admin.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rollback.Exec("DROP POLICY " + quote(policyName) + " ON " + quote(table)); err != nil {
		t.Fatal(err)
	}
	if _, err := rollback.Exec("CREATE POLICY " + quote(policyName) + " ON " + quote(table) + " FOR SELECT TO PUBLIC USING (" + usingTrue + ")"); err != nil {
		t.Fatal(err)
	}
	if err := rollback.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := countRows(); got != 0 {
		t.Fatalf("rolled-back transition changed visibility=%d", got)
	}
	// An unregistered permissive manual policy demonstrably broadens PostgreSQL
	// composition, which is why generation/status reject it.
	manual := "manual_broadening_" + table
	if len(manual) > 63 {
		manual = manual[:63]
	}
	if _, err := admin.Exec("CREATE POLICY " + quote(manual) + " ON " + quote(table) + " FOR SELECT TO PUBLIC USING (TRUE)"); err != nil {
		t.Fatal(err)
	}
	if got := countRows(); got != 1 {
		t.Fatalf("manual permissive policy did not expose broadening: %d", got)
	}
	if _, err := admin.Exec("DROP POLICY " + quote(manual) + " ON " + quote(table)); err != nil {
		t.Fatal(err)
	}
	if got := countRows(); got != 0 {
		t.Fatalf("manual policy cleanup visibility=%d", got)
	}
}

func runPostgresSubjectCorpus(t *testing.T, admin, runtimeDB *sql.DB, table string, quote func(string) string) {
	t.Helper()
	if _, err := admin.Exec("ALTER TABLE " + quote(table) + " DISABLE ROW LEVEL SECURITY"); err != nil {
		t.Fatal(err)
	}
	var names []string
	rows, err := admin.Query("SELECT policyname FROM pg_policies WHERE schemaname='public' AND tablename=$1", table)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	rows.Close()
	for _, name := range names {
		if _, err := admin.Exec("DROP POLICY " + quote(name) + " ON " + quote(table)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := admin.Exec("DELETE FROM " + quote(table)); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("INSERT INTO " + quote(table) + " VALUES ('99999999-9999-4999-8999-999999999999','aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa')"); err != nil {
		t.Fatal(err)
	}
	type subjectCase struct {
		name     string
		subject  schema.RowSubject
		context  PolicyContext
		decision bool
	}
	cases := []subjectCase{{"public", schema.RowSubject{Kind: schema.SubjectPublic}, NewVerifiedPolicyContext(nil, nil), true}, {"authenticated", schema.RowSubject{Kind: schema.SubjectAuthenticated}, NewVerifiedPolicyContext(map[string]string{"user_id": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}, nil), true}, {"authenticated_missing", schema.RowSubject{Kind: schema.SubjectAuthenticated}, NewVerifiedPolicyContext(nil, nil), false}, {"role", schema.RowSubject{Kind: schema.SubjectRole, Name: "admin"}, NewVerifiedPolicyContext(nil, []string{"admin"}), true}, {"role_missing", schema.RowSubject{Kind: schema.SubjectRole, Name: "admin"}, NewVerifiedPolicyContext(nil, []string{"viewer"}), false}}
	for _, tc := range cases {
		t.Run("subject_"+tc.name, func(t *testing.T) {
			var old []string
			rows, err := admin.Query("SELECT policyname FROM pg_policies WHERE schemaname='public' AND tablename=$1", table)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var name string
				if err := rows.Scan(&name); err != nil {
					t.Fatal(err)
				}
				old = append(old, name)
			}
			rows.Close()
			for _, name := range old {
				if _, err := admin.Exec("DROP POLICY " + quote(name) + " ON " + quote(table)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := admin.Exec("ALTER TABLE " + quote(table) + " DISABLE ROW LEVEL SECURITY"); err != nil {
				t.Fatal(err)
			}
			rowPolicyRuntimeRegistry = map[string]rowPolicyRuntimeDefinition{}
			registerRowPolicyRuntime(rowPolicyRuntimeDefinition{Table: table, SubjectCombination: "any", IdentityTypes: map[string]string{"user_id": "uuid"}, Rules: []rowPolicyRuntimeRule{{Key: "subject", SubjectKind: string(tc.subject.Kind), SubjectName: tc.subject.Name, Select: &rowPolicyRuntimePredicate{Kind: "allow"}}}})
			appRows, appErr := Query[postgresPolicyMessage](table).WithPolicyContext(tc.context).All()
			if appErr != nil && tc.decision {
				t.Fatal(appErr)
			}
			if got := len(appRows) > 0; got != tc.decision {
				t.Fatalf("application=%t want %t", got, tc.decision)
			}
			allow := schema.Allow()
			resolved := generator.ResolvedRowPolicy{Protection: schema.RowProtection{Table: table, SubjectCombination: schema.AnyOfSubjects, Rules: []schema.RowRule{{Key: "subject", Subject: tc.subject, Select: &allow}}}, EnforcementClass: "portable", Identities: map[string]schema.PolicyIdentityType{"user_id": schema.PolicyIdentityUUID}, PhysicalPlans: map[string]string{"select": "select"}}
			plans, err := generator.LowerPostgresRowPolicies([]generator.ResolvedRowPolicy{resolved})
			if err != nil {
				t.Fatal(err)
			}
			policy := plans[0].Policies[0]
			if _, err := admin.Exec("CREATE POLICY " + quote(policy.Name) + " ON " + quote(table) + " FOR SELECT TO PUBLIC USING (" + policy.Using + ")"); err != nil {
				t.Fatal(err)
			}
			if _, err := admin.Exec("ALTER TABLE " + quote(table) + " ENABLE ROW LEVEL SECURITY"); err != nil {
				t.Fatal(err)
			}
			if _, err := admin.Exec("ALTER TABLE " + quote(table) + " FORCE ROW LEVEL SECURITY"); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := withSQLPolicyContext(runtimeDB, tc.context, func(tx *sql.Tx) error { return tx.QueryRow("SELECT count(*) FROM " + quote(table)).Scan(&count) }); err != nil {
				t.Fatal(err)
			}
			if got := count > 0; got != tc.decision {
				t.Fatalf("RLS=%t want %t", got, tc.decision)
			}
			err = TransactionOn(runtimeDB, func(tx *Tx) error {
				if err := tx.WithPostgresPolicyContext(tc.context); err != nil {
					return err
				}
				rows, err := Query[postgresPolicyMessage](table).UseTransaction(tx.conn).WithPolicyContext(tc.context).All()
				if err != nil {
					if !tc.decision {
						return nil
					}
					return err
				}
				if got := len(rows) > 0; got != tc.decision {
					return fmt.Errorf("dual=%t want %t", got, tc.decision)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	// A malformed role encoding is fail-closed and does not leak across the
	// pooled connection after the transaction ends.
	var count int
	tx, err := runtimeDB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("SELECT set_config('pickle.identity.roles','not-json',true)"); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow("SELECT count(*) FROM " + quote(table)).Scan(&count); err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	if count != 0 {
		t.Fatalf("malformed roles admitted %d rows", count)
	}
	if err := runtimeDB.QueryRow("SELECT count(*) FROM " + quote(table)).Scan(&count); err != nil || count != 0 {
		t.Fatalf("pooled role setting leaked count=%d err=%v", count, err)
	}
}

func runPostgresPredicateCorpus(t *testing.T, admin, runtimeDB *sql.DB, table string, quote func(string) string) {
	t.Helper()
	type corpusCase struct {
		ID, Predicate, Identity, Row string
		Decision                     bool
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "row-policy-conformance", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []corpusCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	related := table + "_memberships"
	if _, err := admin.Exec("CREATE TABLE " + quote(related) + " (parent_id uuid, user_id uuid)"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("GRANT SELECT ON " + quote(related) + " TO PUBLIC"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec("DROP TABLE IF EXISTS " + quote(related) + " CASCADE") })
	matching := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	different := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	rowID := "88888888-8888-4888-8888-888888888888"
	var previous []generator.GeneratedPostgresRowPolicy
	var existing []string
	rows, err := admin.Query("SELECT policyname FROM pg_policies WHERE schemaname='public' AND tablename=$1", table)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		existing = append(existing, name)
	}
	rows.Close()
	for _, name := range existing {
		if _, err := admin.Exec("DROP POLICY " + quote(name) + " ON " + quote(table)); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range cases {
		t.Run("corpus_"+tc.ID, func(t *testing.T) {
			if _, err := admin.Exec("ALTER TABLE " + quote(table) + " DISABLE ROW LEVEL SECURITY"); err != nil {
				t.Fatal(err)
			}
			for _, policy := range previous {
				if _, err := admin.Exec("DROP POLICY IF EXISTS " + quote(policy.Name) + " ON " + quote(table)); err != nil {
					t.Fatal(err)
				}
			}
			previous = nil
			if _, err := admin.Exec("DELETE FROM " + quote(table)); err != nil {
				t.Fatal(err)
			}
			if _, err := admin.Exec("DELETE FROM " + quote(related)); err != nil {
				t.Fatal(err)
			}
			rowValue := "'" + matching + "'"
			if tc.Row == "different" {
				rowValue = "'" + different + "'"
			}
			if tc.Row == "null" {
				rowValue = "NULL"
			}
			if _, err := admin.Exec("INSERT INTO " + quote(table) + " VALUES ('" + rowID + "'," + rowValue + ")"); err != nil {
				t.Fatal(err)
			}
			base := schema.Equal(schema.PolicyColumn("user_id"), schema.Identity("user_id"))
			var predicate schema.RowPredicate
			switch tc.Predicate {
			case "allow":
				predicate = schema.Allow()
			case "deny":
				predicate = schema.Deny()
			case "equal":
				predicate = base
			case "not_equal":
				predicate = schema.NotEqual(schema.PolicyColumn("user_id"), schema.Identity("user_id"))
			case "and":
				predicate = schema.And(base, schema.Allow())
			case "or":
				predicate = schema.Or(base, schema.Deny())
			case "not":
				predicate = schema.Not(base)
			case "exists":
				predicate = schema.Exists("memberships", base)
				predicate.RelatedTable = related
				predicate.LocalColumn = "id"
				predicate.ForeignColumn = "parent_id"
				if tc.Row == "related_matching" {
					if _, err := admin.Exec("INSERT INTO " + quote(related) + " VALUES ('" + rowID + "','" + matching + "')"); err != nil {
						t.Fatal(err)
					}
				}
			default:
				t.Fatalf("unknown predicate %s", tc.Predicate)
			}
			ctxIdentities := map[string]string{"user_id": matching}
			if tc.Identity == "missing" {
				ctxIdentities = map[string]string{}
			}
			ctx := NewVerifiedPolicyContext(ctxIdentities, nil)
			runtimePredicate := postgresTestRuntimePredicate(predicate)
			rowPolicyRuntimeRegistry = map[string]rowPolicyRuntimeDefinition{}
			registerRowPolicyRuntime(rowPolicyRuntimeDefinition{Table: table, SubjectCombination: "any", IdentityTypes: map[string]string{"user_id": "uuid"}, Rules: []rowPolicyRuntimeRule{{Key: "corpus", SubjectKind: "public", Select: runtimePredicate}}})
			rows, appErr := Query[postgresPolicyMessage](table).WithPolicyContext(ctx).All()
			if appErr != nil && tc.Decision {
				t.Fatal(appErr)
			}
			if got := len(rows) > 0; got != tc.Decision {
				t.Fatalf("application decision=%t want %t", got, tc.Decision)
			}
			resolved := generator.ResolvedRowPolicy{Protection: schema.RowProtection{Table: table, SubjectCombination: schema.AnyOfSubjects, Rules: []schema.RowRule{{Key: "corpus", Subject: schema.RowSubject{Kind: schema.SubjectPublic}, Select: &predicate}}}, EnforcementClass: "portable", Identities: map[string]schema.PolicyIdentityType{"user_id": schema.PolicyIdentityUUID}, PhysicalPlans: map[string]string{"select": "select"}}
			plans, err := generator.LowerPostgresRowPolicies([]generator.ResolvedRowPolicy{resolved})
			if err != nil {
				t.Fatal(err)
			}
			previous = plans[0].Policies
			for _, policy := range previous {
				if _, err := admin.Exec("CREATE POLICY " + quote(policy.Name) + " ON " + quote(table) + " FOR SELECT TO PUBLIC USING (" + policy.Using + ")"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := admin.Exec("ALTER TABLE " + quote(table) + " ENABLE ROW LEVEL SECURITY"); err != nil {
				t.Fatal(err)
			}
			if _, err := admin.Exec("ALTER TABLE " + quote(table) + " FORCE ROW LEVEL SECURITY"); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := withSQLPolicyContext(runtimeDB, ctx, func(tx *sql.Tx) error { return tx.QueryRow("SELECT count(*) FROM " + quote(table)).Scan(&count) }); err != nil {
				t.Fatal(err)
			}
			if got := count > 0; got != tc.Decision {
				t.Fatalf("RLS decision=%t want %t", got, tc.Decision)
			}
			err = TransactionOn(runtimeDB, func(tx *Tx) error {
				if err := tx.WithPostgresPolicyContext(ctx); err != nil {
					return err
				}
				rows, err := Query[postgresPolicyMessage](table).UseTransaction(tx.conn).WithPolicyContext(ctx).All()
				if err != nil {
					if !tc.Decision {
						return nil
					}
					return err
				}
				if got := len(rows) > 0; got != tc.Decision {
					return fmt.Errorf("dual decision=%t want %t", got, tc.Decision)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func postgresTestRuntimePredicate(predicate schema.RowPredicate) *rowPolicyRuntimePredicate {
	out := &rowPolicyRuntimePredicate{Kind: string(predicate.Kind), Name: predicate.Name, RelatedTable: predicate.RelatedTable, LocalColumn: predicate.LocalColumn, ForeignColumn: predicate.ForeignColumn}
	for _, child := range predicate.Children {
		out.Children = append(out.Children, *postgresTestRuntimePredicate(child))
	}
	return out
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
	if err := setSQLPolicyContext(tx, ctx); err != nil {
		return err
	}
	return fn(tx)
}

func setSQLPolicyContext(tx *sql.Tx, ctx PolicyContext) error {
	for name, value := range ctx.identities {
		if _, err := tx.Exec("SELECT set_config($1,$2,true)", "pickle.identity."+name, value); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("SELECT set_config($1,$2,true)", "pickle.identity.roles", ctx.encodedRoles()); err != nil {
		return err
	}
	return nil
}
