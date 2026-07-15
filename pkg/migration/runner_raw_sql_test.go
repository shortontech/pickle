//go:build ignore

package migration

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync"
	"testing"
)

var rawSQLDriverState = &rawSQLState{tables: map[string]bool{}}

func init() { sql.Register("pickle-raw-sql-test", rawSQLDriver{state: rawSQLDriverState}) }

type rawSQLState struct {
	sync.Mutex
	tables map[string]bool
}

type rawSQLDriver struct{ state *rawSQLState }

func (d rawSQLDriver) Open(string) (driver.Conn, error) { return &rawSQLConn{state: d.state}, nil }

type rawSQLConn struct{ state *rawSQLState }

func (c *rawSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not supported")
}
func (c *rawSQLConn) Close() error              { return nil }
func (c *rawSQLConn) Begin() (driver.Tx, error) { return rawSQLTx{}, nil }
func (c *rawSQLConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return rawSQLTx{}, nil
}
func (c *rawSQLConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.state.Lock()
	defer c.state.Unlock()
	switch query {
	case "CREATE TABLE raw_sql_probe (id INTEGER PRIMARY KEY)":
		c.state.tables["raw_sql_probe"] = true
	case "DROP TABLE raw_sql_probe":
		delete(c.state.tables, "raw_sql_probe")
	default:
		return nil, errors.New("unexpected raw SQL: " + query)
	}
	return driver.RowsAffected(1), nil
}

type rawSQLTx struct{}

func (rawSQLTx) Commit() error   { return nil }
func (rawSQLTx) Rollback() error { return nil }

type rawSQLMigration struct{ Migration }

func (m *rawSQLMigration) Up()   { m.RawSQL("CREATE TABLE raw_sql_probe (id INTEGER PRIMARY KEY)") }
func (m *rawSQLMigration) Down() { m.RawSQL("DROP TABLE raw_sql_probe") }

func TestRawSQLMigrationExecutesUpAndDown(t *testing.T) {
	rawSQLDriverState.Lock()
	rawSQLDriverState.tables = map[string]bool{}
	rawSQLDriverState.Unlock()

	db, err := sql.Open("pickle-raw-sql-test", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	runner := NewRunner(db, "sqlite")
	migration := &rawSQLMigration{}

	if err := runner.runMigration(migration); err != nil {
		t.Fatalf("run RawSQL migration: %v", err)
	}
	rawSQLDriverState.Lock()
	created := rawSQLDriverState.tables["raw_sql_probe"]
	rawSQLDriverState.Unlock()
	if !created {
		t.Fatal("RawSQL Up was recorded but did not execute")
	}

	if err := runner.rollbackMigration(migration); err != nil {
		t.Fatalf("rollback RawSQL migration: %v", err)
	}
	rawSQLDriverState.Lock()
	created = rawSQLDriverState.tables["raw_sql_probe"]
	rawSQLDriverState.Unlock()
	if created {
		t.Fatal("RawSQL Down was recorded but did not execute")
	}
}
