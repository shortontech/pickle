package cooked

import (
	"context"
	"database/sql"
	"fmt"
)

// dbExecutor is the common interface between *sql.DB and *sql.Tx.
// Both satisfy this interface, allowing query builders to work with either.
type dbExecutor interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
	Exec(query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Tx wraps a database transaction and provides query builder constructors
// scoped to that transaction. Generated Query<Model>() methods are emitted
// by the scope generator for each model.
type Tx struct {
	conn  *sql.Tx
	depth int // savepoint nesting depth
}

// Conn returns the underlying *sql.Tx for advanced use cases.
func (tx *Tx) Conn() *sql.Tx {
	return tx.conn
}

// WithTransaction runs fn inside a database transaction. If fn returns nil, the
// transaction is committed. If fn returns an error or panics, the transaction
// is rolled back.
//
// Usage:
//
//	WithTransaction(func(tx *Tx) error { ... })
//
// The Tx object provides query builders scoped to the transaction via generated
// methods like tx.QueryUser(), tx.QueryTransfer(), etc.
func WithTransaction(fn func(tx *Tx) error) error {
	return TransactionOn(DB, fn)
}

// TransactionOn runs fn inside a transaction on the specified database connection.
func TransactionOn(db *sql.DB, fn func(tx *Tx) error) error {
	sqlTx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	tx := &Tx{conn: sqlTx, depth: 0}

	defer func() {
		if r := recover(); r != nil {
			_ = sqlTx.Rollback()
			panic(r) // re-panic after rollback
		}
	}()

	if err := fn(tx); err != nil {
		_ = sqlTx.Rollback()
		return err
	}

	return sqlTx.Commit()
}

// Transaction runs fn inside a nested savepoint. If fn returns nil, the
// savepoint is released. If fn returns an error, only the savepoint is
// rolled back — the outer transaction continues.
func (tx *Tx) Transaction(fn func(tx *Tx) error) error {
	tx.depth++
	sp := fmt.Sprintf("sp_%d", tx.depth)

	if _, err := tx.conn.Exec("SAVEPOINT " + sp); err != nil {
		tx.depth--
		return fmt.Errorf("savepoint %s: %w", sp, err)
	}

	nested := &Tx{conn: tx.conn, depth: tx.depth}

	defer func() {
		if r := recover(); r != nil {
			_, _ = tx.conn.Exec("ROLLBACK TO SAVEPOINT " + sp)
			tx.depth--
			panic(r)
		}
	}()

	if err := fn(nested); err != nil {
		_, _ = tx.conn.Exec("ROLLBACK TO SAVEPOINT " + sp)
		tx.depth--
		return err
	}

	_, err := tx.conn.Exec("RELEASE SAVEPOINT " + sp)
	tx.depth--
	return err
}
