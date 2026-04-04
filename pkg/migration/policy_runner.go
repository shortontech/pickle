//go:build ignore

package migration

import (
	"database/sql"
	"fmt"
)

// PolicyIface is implemented by all role policy structs via embedded Policy.
type PolicyIface interface {
	Reset()
	Up()
	Down()
	GetOperations() []RoleOperation
	Transactional() bool
}

// PolicyEntry pairs a string ID with a policy instance.
type PolicyEntry struct {
	ID     string
	Policy PolicyIface
}

// PolicyStatus describes a policy's current state in rbac_changelog.
type PolicyStatus struct {
	ID      string
	Batch   int
	State   string // pending, running, applied, failed, rolling_back, rolled_back
	Applied bool
}

// PolicyRunner executes role policies against the database.
type PolicyRunner struct {
	DB     *sql.DB
	Driver string
}

// NewPolicyRunner creates a PolicyRunner for the given driver.
func NewPolicyRunner(db *sql.DB, driver string) *PolicyRunner {
	return &PolicyRunner{DB: db, Driver: driver}
}

func (r *PolicyRunner) placeholder(n int) string {
	if r.Driver == "pgsql" || r.Driver == "postgres" {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

func (r *PolicyRunner) ensureChangelogTable() error {
	var q string
	switch r.Driver {
	case "pgsql", "postgres":
		q = `CREATE TABLE IF NOT EXISTS rbac_changelog (
			id         VARCHAR(255) PRIMARY KEY,
			batch      INTEGER NOT NULL,
			state      VARCHAR(20) NOT NULL DEFAULT 'pending',
			error      TEXT,
			started_at TIMESTAMPTZ,
			completed_at TIMESTAMPTZ
		)`
	default:
		q = `CREATE TABLE IF NOT EXISTS rbac_changelog (
			id         VARCHAR(255) PRIMARY KEY,
			batch      INTEGER NOT NULL,
			state      VARCHAR(20) NOT NULL DEFAULT 'pending',
			error      TEXT,
			started_at DATETIME,
			completed_at DATETIME
		)`
	}
	_, err := r.DB.Exec(q)
	return err
}

func (r *PolicyRunner) acquireLock() error {
	if r.Driver == "pgsql" || r.Driver == "postgres" {
		_, err := r.DB.Exec("SELECT pg_advisory_lock(20260301)")
		return err
	}
	return nil
}

func (r *PolicyRunner) releaseLock() {
	if r.Driver == "pgsql" || r.Driver == "postgres" {
		r.DB.Exec("SELECT pg_advisory_unlock(20260301)") //nolint:errcheck
	}
}

func (r *PolicyRunner) applied() (map[string]int, error) {
	rows, err := r.DB.Query("SELECT id, batch FROM rbac_changelog WHERE state = 'applied'")
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

func (r *PolicyRunner) nextBatch(applied map[string]int) int {
	max := 0
	for _, b := range applied {
		if b > max {
			max = b
		}
	}
	return max + 1
}

func (r *PolicyRunner) execRoleOps(ops []RoleOperation, tx *sql.Tx) error {
	for _, op := range ops {
		if err := r.execRoleOp(op, tx); err != nil {
			return err
		}
	}
	return nil
}

func (r *PolicyRunner) execRoleOp(op RoleOperation, tx *sql.Tx) error {
	exec := func(q string, args ...interface{}) error {
		if tx != nil {
			_, err := tx.Exec(q, args...)
			return err
		}
		_, err := r.DB.Exec(q, args...)
		return err
	}

	query := func(q string, args ...interface{}) (*sql.Rows, error) {
		if tx != nil {
			return tx.Query(q, args...)
		}
		return r.DB.Query(q, args...)
	}

	switch op.Type {
	case "create":
		q := fmt.Sprintf(
			"INSERT INTO roles (slug, name, manages, is_default) VALUES (%s, %s, %s, %s)",
			r.placeholder(1), r.placeholder(2), r.placeholder(3), r.placeholder(4),
		)
		if err := exec(q, op.Role.Slug, op.Role.DisplayName, op.Role.IsManages, op.Role.IsDefault); err != nil {
			return fmt.Errorf("creating role %q: %w", op.Role.Slug, err)
		}
		// Insert actions
		for _, action := range op.Role.Actions {
			q := fmt.Sprintf(
				"INSERT INTO role_actions (role_slug, action) VALUES (%s, %s)",
				r.placeholder(1), r.placeholder(2),
			)
			if err := exec(q, op.Role.Slug, action); err != nil {
				return fmt.Errorf("granting action %q to role %q: %w", action, op.Role.Slug, err)
			}
		}

	case "alter":
		// Update fields if set
		if op.Role.DisplayName != "" {
			q := fmt.Sprintf("UPDATE roles SET name = %s WHERE slug = %s",
				r.placeholder(1), r.placeholder(2))
			if err := exec(q, op.Role.DisplayName, op.Role.Slug); err != nil {
				return fmt.Errorf("altering role %q: %w", op.Role.Slug, err)
			}
		}
		if op.Role.IsManages {
			q := fmt.Sprintf("UPDATE roles SET manages = %s WHERE slug = %s",
				r.placeholder(1), r.placeholder(2))
			if err := exec(q, true, op.Role.Slug); err != nil {
				return err
			}
		}
		if op.Role.RemoveManages {
			q := fmt.Sprintf("UPDATE roles SET manages = %s WHERE slug = %s",
				r.placeholder(1), r.placeholder(2))
			if err := exec(q, false, op.Role.Slug); err != nil {
				return err
			}
		}
		if op.Role.IsDefault {
			q := fmt.Sprintf("UPDATE roles SET is_default = %s WHERE slug = %s",
				r.placeholder(1), r.placeholder(2))
			if err := exec(q, true, op.Role.Slug); err != nil {
				return err
			}
		}
		if op.Role.RemoveDefault {
			q := fmt.Sprintf("UPDATE roles SET is_default = %s WHERE slug = %s",
				r.placeholder(1), r.placeholder(2))
			if err := exec(q, false, op.Role.Slug); err != nil {
				return err
			}
		}
		// Add new actions
		for _, action := range op.Role.Actions {
			q := fmt.Sprintf(
				"INSERT INTO role_actions (role_slug, action) VALUES (%s, %s)",
				r.placeholder(1), r.placeholder(2),
			)
			if err := exec(q, op.Role.Slug, action); err != nil {
				return fmt.Errorf("granting action %q to role %q: %w", action, op.Role.Slug, err)
			}
		}
		// Revoke actions
		for _, action := range op.Role.RevokeActions {
			q := fmt.Sprintf(
				"DELETE FROM role_actions WHERE role_slug = %s AND action = %s",
				r.placeholder(1), r.placeholder(2),
			)
			if err := exec(q, op.Role.Slug, action); err != nil {
				return fmt.Errorf("revoking action %q from role %q: %w", action, op.Role.Slug, err)
			}
		}

	case "drop":
		// Delete actions first
		q := fmt.Sprintf("DELETE FROM role_actions WHERE role_slug = %s", r.placeholder(1))
		if err := exec(q, op.Role.Slug); err != nil {
			return fmt.Errorf("removing actions for role %q: %w", op.Role.Slug, err)
		}
		// Check role exists
		checkQ := fmt.Sprintf("SELECT slug FROM roles WHERE slug = %s", r.placeholder(1))
		rows, err := query(checkQ, op.Role.Slug)
		if err != nil {
			return fmt.Errorf("checking role %q: %w", op.Role.Slug, err)
		}
		found := rows.Next()
		rows.Close()
		if !found {
			return fmt.Errorf("role %q does not exist", op.Role.Slug)
		}
		q = fmt.Sprintf("DELETE FROM roles WHERE slug = %s", r.placeholder(1))
		if err := exec(q, op.Role.Slug); err != nil {
			return fmt.Errorf("dropping role %q: %w", op.Role.Slug, err)
		}
	}
	return nil
}

// Migrate runs all pending role policies in order.
func (r *PolicyRunner) Migrate(entries []PolicyEntry) error {
	if err := r.ensureChangelogTable(); err != nil {
		return fmt.Errorf("creating rbac_changelog table: %w", err)
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
		fmt.Printf("  applying policy: %s\n", entry.ID)

		// Mark as running
		q := fmt.Sprintf(
			"INSERT INTO rbac_changelog (id, batch, state, started_at) VALUES (%s, %s, 'running', NOW())",
			r.placeholder(1), r.placeholder(2),
		)
		if _, err := r.DB.Exec(q, entry.ID, batch); err != nil {
			return fmt.Errorf("recording start of %s: %w", entry.ID, err)
		}

		entry.Policy.Reset()
		entry.Policy.Up()
		ops := entry.Policy.GetOperations()

		var execErr error
		if entry.Policy.Transactional() {
			tx, err := r.DB.Begin()
			if err != nil {
				execErr = err
			} else {
				if err := r.execRoleOps(ops, tx); err != nil {
					tx.Rollback() //nolint:errcheck
					execErr = err
				} else {
					execErr = tx.Commit()
				}
			}
		} else {
			execErr = r.execRoleOps(ops, nil)
		}

		if execErr != nil {
			// Mark as failed
			uq := fmt.Sprintf(
				"UPDATE rbac_changelog SET state = 'failed', error = %s, completed_at = NOW() WHERE id = %s",
				r.placeholder(1), r.placeholder(2),
			)
			r.DB.Exec(uq, execErr.Error(), entry.ID) //nolint:errcheck
			return fmt.Errorf("applying policy %s: %w", entry.ID, execErr)
		}

		// Mark as applied
		uq := fmt.Sprintf(
			"UPDATE rbac_changelog SET state = 'applied', completed_at = NOW() WHERE id = %s",
			r.placeholder(1),
		)
		if _, err := r.DB.Exec(uq, entry.ID); err != nil {
			return fmt.Errorf("recording %s: %w", entry.ID, err)
		}
		fmt.Printf("  applied policy: %s\n", entry.ID)
		ran++
	}
	if ran == 0 {
		fmt.Println("  no pending role policies")
	}
	return nil
}

// Rollback reverses the last batch of role policies.
func (r *PolicyRunner) Rollback(entries []PolicyEntry) error {
	if err := r.ensureChangelogTable(); err != nil {
		return fmt.Errorf("creating rbac_changelog table: %w", err)
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
		fmt.Printf("  rolling back policy: %s\n", entry.ID)

		// Mark as rolling back
		uq := fmt.Sprintf("UPDATE rbac_changelog SET state = 'rolling_back' WHERE id = %s", r.placeholder(1))
		r.DB.Exec(uq, entry.ID) //nolint:errcheck

		entry.Policy.Reset()
		entry.Policy.Down()
		ops := entry.Policy.GetOperations()

		var execErr error
		if entry.Policy.Transactional() {
			tx, err := r.DB.Begin()
			if err != nil {
				execErr = err
			} else {
				if err := r.execRoleOps(ops, tx); err != nil {
					tx.Rollback() //nolint:errcheck
					execErr = err
				} else {
					execErr = tx.Commit()
				}
			}
		} else {
			execErr = r.execRoleOps(ops, nil)
		}

		if execErr != nil {
			uq := fmt.Sprintf(
				"UPDATE rbac_changelog SET state = 'failed', error = %s WHERE id = %s",
				r.placeholder(1), r.placeholder(2),
			)
			r.DB.Exec(uq, execErr.Error(), entry.ID) //nolint:errcheck
			return fmt.Errorf("rolling back policy %s: %w", entry.ID, execErr)
		}

		// Mark as rolled back
		uq = fmt.Sprintf("UPDATE rbac_changelog SET state = 'rolled_back', completed_at = NOW() WHERE id = %s", r.placeholder(1))
		if _, err := r.DB.Exec(uq, entry.ID); err != nil {
			return fmt.Errorf("recording rollback of %s: %w", entry.ID, err)
		}
		fmt.Printf("  rolled back policy: %s\n", entry.ID)
	}
	return nil
}

// Status returns the status of all known role policies.
func (r *PolicyRunner) Status(entries []PolicyEntry) ([]PolicyStatus, error) {
	if err := r.ensureChangelogTable(); err != nil {
		return nil, fmt.Errorf("creating rbac_changelog table: %w", err)
	}
	rows, err := r.DB.Query("SELECT id, batch, state FROM rbac_changelog")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	states := map[string]PolicyStatus{}
	for rows.Next() {
		var s PolicyStatus
		if err := rows.Scan(&s.ID, &s.Batch, &s.State); err != nil {
			return nil, err
		}
		s.Applied = s.State == "applied"
		states[s.ID] = s
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var result []PolicyStatus
	for _, entry := range entries {
		if s, ok := states[entry.ID]; ok {
			result = append(result, s)
		} else {
			result = append(result, PolicyStatus{ID: entry.ID, State: "pending"})
		}
	}
	return result, nil
}

// DeriveRoles replays all applied role policies in order and returns the
// current role set. Each role carries the ID of the policy that created it
// as BirthTimestamp.
func DeriveRoles(entries []PolicyEntry) []DerivedRole {
	roles := map[string]*DerivedRole{}
	var order []string

	for _, entry := range entries {
		entry.Policy.Reset()
		entry.Policy.Up()
		for _, op := range entry.Policy.GetOperations() {
			switch op.Type {
			case "create":
				roles[op.Role.Slug] = &DerivedRole{
					Slug:           op.Role.Slug,
					DisplayName:    op.Role.DisplayName,
					IsManages:      op.Role.IsManages,
					IsDefault:      op.Role.IsDefault,
					Actions:        append([]string{}, op.Role.Actions...),
					BirthTimestamp: entry.ID,
				}
				order = append(order, op.Role.Slug)

			case "alter":
				r := roles[op.Role.Slug]
				if r == nil {
					continue
				}
				if op.Role.DisplayName != "" {
					r.DisplayName = op.Role.DisplayName
				}
				if op.Role.IsManages {
					r.IsManages = true
				}
				if op.Role.RemoveManages {
					r.IsManages = false
				}
				if op.Role.IsDefault {
					r.IsDefault = true
				}
				if op.Role.RemoveDefault {
					r.IsDefault = false
				}
				r.Actions = append(r.Actions, op.Role.Actions...)
				for _, revoke := range op.Role.RevokeActions {
					filtered := r.Actions[:0]
					for _, a := range r.Actions {
						if a != revoke {
							filtered = append(filtered, a)
						}
					}
					r.Actions = filtered
				}

			case "drop":
				delete(roles, op.Role.Slug)
				// Remove from order
				for i, s := range order {
					if s == op.Role.Slug {
						order = append(order[:i], order[i+1:]...)
						break
					}
				}
			}
		}
	}

	var result []DerivedRole
	for _, slug := range order {
		if r, ok := roles[slug]; ok {
			result = append(result, *r)
		}
	}
	return result
}

// DerivedRole represents the computed state of a role after replaying all policies.
type DerivedRole struct {
	Slug           string
	DisplayName    string
	IsManages      bool
	IsDefault      bool
	Actions        []string
	BirthTimestamp string // policy ID that created this role
}
