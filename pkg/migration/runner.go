//go:build ignore

package migration

import (
	"database/sql"
	"fmt"
)

// MigrationIface is implemented by all migration structs via embedded Migration.
type MigrationIface interface {
	Reset()
	Up()
	Down()
	GetOperations() []Operation
	Transactional() bool
}

// MigrationEntry pairs a string ID with a migration instance.
type MigrationEntry struct {
	ID        string
	Migration MigrationIface
}

// MigrationStatus describes a migration's current state.
type MigrationStatus struct {
	ID      string
	Batch   int
	Applied bool
}

// SQLGenerator converts schema operations to SQL for a specific driver.
type SQLGenerator interface {
	CreateTable(t *Table) string
	DropTableIfExists(name string) string
	AddColumn(table string, col *Column) string
	DropColumn(table, column string) string
	RenameColumn(table, oldName, newName string) string
	AddIndex(idx *Index) string
	RenameTable(oldName, newName string) string
}

// Runner executes migrations against a database.
type Runner struct {
	DB        *sql.DB
	Driver    string
	Generator SQLGenerator
}

// NewRunner creates a Runner configured for the given driver.
func NewRunner(db *sql.DB, driver string) *Runner {
	var gen SQLGenerator
	switch driver {
	case "pgsql", "postgres":
		gen = &postgresGenerator{}
	case "mysql":
		gen = &mysqlGenerator{}
	default:
		gen = &sqliteGenerator{}
	}
	return &Runner{DB: db, Driver: driver, Generator: gen}
}

func (r *Runner) ensureMigrationsTable() error {
	var q string
	switch r.Driver {
	case "pgsql", "postgres":
		q = `CREATE TABLE IF NOT EXISTS migrations (
			id        SERIAL PRIMARY KEY,
			migration VARCHAR(255) NOT NULL,
			batch     INTEGER NOT NULL
		)`
	default:
		q = `CREATE TABLE IF NOT EXISTS migrations (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			migration VARCHAR(255) NOT NULL,
			batch     INTEGER NOT NULL
		)`
	}
	_, err := r.DB.Exec(q)
	return err
}

func (r *Runner) acquireLock() error {
	if r.Driver == "pgsql" || r.Driver == "postgres" {
		_, err := r.DB.Exec("SELECT pg_advisory_lock(20260101)")
		return err
	}
	return nil
}

func (r *Runner) releaseLock() {
	if r.Driver == "pgsql" || r.Driver == "postgres" {
		r.DB.Exec("SELECT pg_advisory_unlock(20260101)") //nolint:errcheck
	}
}

func (r *Runner) applied() (map[string]int, error) {
	rows, err := r.DB.Query("SELECT migration, batch FROM migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]int{}
	for rows.Next() {
		var id string
		var batch int
		if err := rows.Scan(&id, &batch); err != nil {
			return nil, err
		}
		m[id] = batch
	}
	return m, rows.Err()
}

func (r *Runner) nextBatch(applied map[string]int) int {
	max := 0
	for _, b := range applied {
		if b > max {
			max = b
		}
	}
	return max + 1
}

func (r *Runner) placeholder(n int) string {
	if r.Driver == "pgsql" || r.Driver == "postgres" {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

func (r *Runner) execOps(ops []Operation, tx *sql.Tx) error {
	for _, op := range ops {
		sqls := r.opsToSQL(op)
		for _, q := range sqls {
			if q == "" {
				continue
			}
			if tx != nil {
				if _, err := tx.Exec(q); err != nil {
					return fmt.Errorf("executing %q: %w", q, err)
				}
			} else {
				if _, err := r.DB.Exec(q); err != nil {
					return fmt.Errorf("executing %q: %w", q, err)
				}
			}
		}
	}
	return nil
}

func (r *Runner) opsToSQL(op Operation) []string {
	switch op.Type {
	case OpCreateTable:
		return []string{r.Generator.CreateTable(op.TableDef)}
	case OpDropTableIfExists:
		return []string{r.Generator.DropTableIfExists(op.Table)}
	case OpRenameTable:
		return []string{r.Generator.RenameTable(op.OldName, op.NewName)}
	case OpAddColumn:
		tmp := &Table{}
		op.ColumnDef(tmp)
		var out []string
		for _, col := range tmp.Columns {
			out = append(out, r.Generator.AddColumn(op.Table, col))
		}
		return out
	case OpDropColumn:
		return []string{r.Generator.DropColumn(op.Table, op.ColumnName)}
	case OpRenameColumn:
		return []string{r.Generator.RenameColumn(op.Table, op.OldName, op.NewName)}
	case OpAddIndex, OpAddUniqueIndex:
		return []string{r.Generator.AddIndex(op.Index)}
	}
	return nil
}

func (r *Runner) runMigration(m MigrationIface) error {
	m.Reset()
	m.Up()
	ops := m.GetOperations()
	if m.Transactional() {
		tx, err := r.DB.Begin()
		if err != nil {
			return err
		}
		if err := r.execOps(ops, tx); err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
		return tx.Commit()
	}
	return r.execOps(ops, nil)
}

func (r *Runner) rollbackMigration(m MigrationIface) error {
	m.Reset()
	m.Down()
	ops := m.GetOperations()
	if m.Transactional() {
		tx, err := r.DB.Begin()
		if err != nil {
			return err
		}
		if err := r.execOps(ops, tx); err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
		return tx.Commit()
	}
	return r.execOps(ops, nil)
}

// Migrate runs all pending migrations in order.
func (r *Runner) Migrate(entries []MigrationEntry) error {
	if err := r.ensureMigrationsTable(); err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}
	if err := r.acquireLock(); err != nil {
		return fmt.Errorf("acquiring lock: %w", err)
	}
	defer r.releaseLock()

	applied, err := r.applied()
	if err != nil {
		return err
	}
	batch := r.nextBatch(applied)

	ran := 0
	for _, entry := range entries {
		if _, ok := applied[entry.ID]; ok {
			continue
		}
		fmt.Printf("  migrating: %s\n", entry.ID)
		if err := r.runMigration(entry.Migration); err != nil {
			return fmt.Errorf("migrating %s: %w", entry.ID, err)
		}
		q := fmt.Sprintf(
			"INSERT INTO migrations (migration, batch) VALUES (%s, %s)",
			r.placeholder(1), r.placeholder(2),
		)
		if _, err := r.DB.Exec(q, entry.ID, batch); err != nil {
			return fmt.Errorf("recording %s: %w", entry.ID, err)
		}
		fmt.Printf("  migrated:  %s\n", entry.ID)
		ran++
	}
	if ran == 0 {
		fmt.Println("  nothing to migrate")
	}
	return nil
}

// Rollback reverses the last batch of migrations.
func (r *Runner) Rollback(entries []MigrationEntry) error {
	if err := r.ensureMigrationsTable(); err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}
	if err := r.acquireLock(); err != nil {
		return fmt.Errorf("acquiring lock: %w", err)
	}
	defer r.releaseLock()

	applied, err := r.applied()
	if err != nil {
		return err
	}

	maxBatch := 0
	for _, b := range applied {
		if b > maxBatch {
			maxBatch = b
		}
	}
	if maxBatch == 0 {
		fmt.Println("  nothing to roll back")
		return nil
	}

	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		b, ok := applied[entry.ID]
		if !ok || b != maxBatch {
			continue
		}
		fmt.Printf("  rolling back: %s\n", entry.ID)
		if err := r.rollbackMigration(entry.Migration); err != nil {
			return fmt.Errorf("rolling back %s: %w", entry.ID, err)
		}
		q := fmt.Sprintf("DELETE FROM migrations WHERE migration = %s", r.placeholder(1))
		if _, err := r.DB.Exec(q, entry.ID); err != nil {
			return fmt.Errorf("removing %s: %w", entry.ID, err)
		}
		fmt.Printf("  rolled back: %s\n", entry.ID)
	}
	return nil
}

// Fresh drops all tables and re-runs all migrations.
func (r *Runner) Fresh(entries []MigrationEntry) error {
	if err := r.ensureMigrationsTable(); err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}
	if err := r.acquireLock(); err != nil {
		return fmt.Errorf("acquiring lock: %w", err)
	}
	defer r.releaseLock()

	fmt.Println("  dropping all tables...")
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		entry.Migration.Reset()
		entry.Migration.Down()
		// Best-effort — ignore errors (tables may not exist)
		r.execOps(entry.Migration.GetOperations(), nil) //nolint:errcheck
		entry.Migration.Reset()
	}
	r.DB.Exec("DROP TABLE IF EXISTS migrations") //nolint:errcheck

	// Release lock before calling Migrate, which acquires its own
	r.releaseLock()
	return r.Migrate(entries)
}

// Status returns the status of all known migrations.
func (r *Runner) Status(entries []MigrationEntry) ([]MigrationStatus, error) {
	if err := r.ensureMigrationsTable(); err != nil {
		return nil, fmt.Errorf("creating migrations table: %w", err)
	}
	applied, err := r.applied()
	if err != nil {
		return nil, err
	}
	var result []MigrationStatus
	for _, entry := range entries {
		s := MigrationStatus{ID: entry.ID}
		if batch, ok := applied[entry.ID]; ok {
			s.Applied = true
			s.Batch = batch
		}
		result = append(result, s)
	}
	return result, nil
}

// PrintStatus prints the migration status table to stdout.
func PrintStatus(statuses []MigrationStatus) {
	maxLen := 0
	for _, s := range statuses {
		if len(s.ID) > maxLen {
			maxLen = len(s.ID)
		}
	}
	for _, s := range statuses {
		state := "Pending"
		batch := ""
		if s.Applied {
			state = "Applied"
			batch = fmt.Sprintf(" (batch %d)", s.Batch)
		}
		fmt.Printf("  %-*s  %s%s\n", maxLen, s.ID, state, batch)
	}
}
