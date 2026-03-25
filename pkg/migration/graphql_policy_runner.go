//go:build ignore

package migration

import (
	"database/sql"
	"fmt"
)

// GraphQLPolicyIface is implemented by all GraphQL policy structs via embedded GraphQLPolicy.
type GraphQLPolicyIface interface {
	Reset()
	Up()
	Down()
	GetOperations() []GraphQLOperation
	Transactional() bool
}

// GraphQLPolicyEntry pairs a string ID with a GraphQL policy instance.
type GraphQLPolicyEntry struct {
	ID     string
	Policy GraphQLPolicyIface
}

// GraphQLPolicyStatus describes a GraphQL policy's current state in graphql_changelog.
type GraphQLPolicyStatus struct {
	ID      string
	Batch   int
	State   string
	Applied bool
}

// GraphQLPolicyRunner executes GraphQL exposure policies against the database.
type GraphQLPolicyRunner struct {
	DB     *sql.DB
	Driver string
}

// NewGraphQLPolicyRunner creates a GraphQLPolicyRunner for the given driver.
func NewGraphQLPolicyRunner(db *sql.DB, driver string) *GraphQLPolicyRunner {
	return &GraphQLPolicyRunner{DB: db, Driver: driver}
}

func (r *GraphQLPolicyRunner) placeholder(n int) string {
	if r.Driver == "pgsql" || r.Driver == "postgres" {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

func (r *GraphQLPolicyRunner) ensureChangelogTable() error {
	var q string
	switch r.Driver {
	case "pgsql", "postgres":
		q = `CREATE TABLE IF NOT EXISTS graphql_changelog (
			id         VARCHAR(255) PRIMARY KEY,
			batch      INTEGER NOT NULL,
			state      VARCHAR(20) NOT NULL DEFAULT 'pending',
			error      TEXT,
			started_at TIMESTAMPTZ,
			completed_at TIMESTAMPTZ
		)`
	default:
		q = `CREATE TABLE IF NOT EXISTS graphql_changelog (
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

func (r *GraphQLPolicyRunner) acquireLock() error {
	if r.Driver == "pgsql" || r.Driver == "postgres" {
		_, err := r.DB.Exec("SELECT pg_advisory_lock(20260302)")
		return err
	}
	return nil
}

func (r *GraphQLPolicyRunner) releaseLock() {
	if r.Driver == "pgsql" || r.Driver == "postgres" {
		r.DB.Exec("SELECT pg_advisory_unlock(20260302)") //nolint:errcheck
	}
}

func (r *GraphQLPolicyRunner) applied() (map[string]int, error) {
	rows, err := r.DB.Query("SELECT id, batch FROM graphql_changelog WHERE state = 'applied'")
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

func (r *GraphQLPolicyRunner) nextBatch(applied map[string]int) int {
	max := 0
	for _, b := range applied {
		if b > max {
			max = b
		}
	}
	return max + 1
}

func (r *GraphQLPolicyRunner) execGraphQLOps(ops []GraphQLOperation, tx *sql.Tx) error {
	for _, op := range ops {
		if err := r.execGraphQLOp(op, tx); err != nil {
			return err
		}
	}
	return nil
}

func (r *GraphQLPolicyRunner) execGraphQLOp(op GraphQLOperation, tx *sql.Tx) error {
	exec := func(q string, args ...interface{}) error {
		if tx != nil {
			_, err := tx.Exec(q, args...)
			return err
		}
		_, err := r.DB.Exec(q, args...)
		return err
	}

	switch op.Type {
	case "expose":
		for _, exposed := range op.Ops {
			q := fmt.Sprintf(
				"INSERT INTO graphql_exposures (model, operation) VALUES (%s, %s)",
				r.placeholder(1), r.placeholder(2),
			)
			if err := exec(q, op.Model, exposed.Type); err != nil {
				return fmt.Errorf("exposing %s.%s: %w", op.Model, exposed.Type, err)
			}
		}

	case "alter_expose":
		for _, exposed := range op.Ops {
			switch {
			case isRemoveOp(exposed.Type):
				baseOp := exposed.Type[len("remove_"):]
				q := fmt.Sprintf(
					"DELETE FROM graphql_exposures WHERE model = %s AND operation = %s",
					r.placeholder(1), r.placeholder(2),
				)
				if err := exec(q, op.Model, baseOp); err != nil {
					return fmt.Errorf("removing %s.%s: %w", op.Model, baseOp, err)
				}
			default:
				q := fmt.Sprintf(
					"INSERT INTO graphql_exposures (model, operation) VALUES (%s, %s)",
					r.placeholder(1), r.placeholder(2),
				)
				if err := exec(q, op.Model, exposed.Type); err != nil {
					return fmt.Errorf("adding %s.%s: %w", op.Model, exposed.Type, err)
				}
			}
		}

	case "unexpose":
		q := fmt.Sprintf("DELETE FROM graphql_exposures WHERE model = %s", r.placeholder(1))
		if err := exec(q, op.Model); err != nil {
			return fmt.Errorf("unexposing %s: %w", op.Model, err)
		}

	case "controller_action":
		if op.Action == nil {
			return fmt.Errorf("controller_action requires action definition")
		}
		q := fmt.Sprintf(
			"INSERT INTO graphql_actions (name) VALUES (%s)",
			r.placeholder(1),
		)
		if err := exec(q, op.Action.Name); err != nil {
			return fmt.Errorf("registering action %q: %w", op.Action.Name, err)
		}

	case "remove_action":
		if op.Action == nil {
			return fmt.Errorf("remove_action requires action definition")
		}
		q := fmt.Sprintf("DELETE FROM graphql_actions WHERE name = %s", r.placeholder(1))
		if err := exec(q, op.Action.Name); err != nil {
			return fmt.Errorf("removing action %q: %w", op.Action.Name, err)
		}
	}
	return nil
}

func isRemoveOp(t string) bool {
	return len(t) > 7 && t[:7] == "remove_"
}

// Migrate runs all pending GraphQL policies in order.
func (r *GraphQLPolicyRunner) Migrate(entries []GraphQLPolicyEntry) error {
	if err := r.ensureChangelogTable(); err != nil {
		return fmt.Errorf("creating graphql_changelog table: %w", err)
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
		fmt.Printf("  applying graphql policy: %s\n", entry.ID)

		// Mark as running
		q := fmt.Sprintf(
			"INSERT INTO graphql_changelog (id, batch, state, started_at) VALUES (%s, %s, 'running', NOW())",
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
				if err := r.execGraphQLOps(ops, tx); err != nil {
					tx.Rollback() //nolint:errcheck
					execErr = err
				} else {
					execErr = tx.Commit()
				}
			}
		} else {
			execErr = r.execGraphQLOps(ops, nil)
		}

		if execErr != nil {
			uq := fmt.Sprintf(
				"UPDATE graphql_changelog SET state = 'failed', error = %s, completed_at = NOW() WHERE id = %s",
				r.placeholder(1), r.placeholder(2),
			)
			r.DB.Exec(uq, execErr.Error(), entry.ID) //nolint:errcheck
			return fmt.Errorf("applying graphql policy %s: %w", entry.ID, execErr)
		}

		uq := fmt.Sprintf(
			"UPDATE graphql_changelog SET state = 'applied', completed_at = NOW() WHERE id = %s",
			r.placeholder(1),
		)
		if _, err := r.DB.Exec(uq, entry.ID); err != nil {
			return fmt.Errorf("recording %s: %w", entry.ID, err)
		}
		fmt.Printf("  applied graphql policy: %s\n", entry.ID)
		ran++
	}
	if ran == 0 {
		fmt.Println("  no pending graphql policies")
	}
	return nil
}

// Rollback reverses the last batch of GraphQL policies.
func (r *GraphQLPolicyRunner) Rollback(entries []GraphQLPolicyEntry) error {
	if err := r.ensureChangelogTable(); err != nil {
		return fmt.Errorf("creating graphql_changelog table: %w", err)
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
		fmt.Printf("  rolling back graphql policy: %s\n", entry.ID)

		uq := fmt.Sprintf("UPDATE graphql_changelog SET state = 'rolling_back' WHERE id = %s", r.placeholder(1))
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
				if err := r.execGraphQLOps(ops, tx); err != nil {
					tx.Rollback() //nolint:errcheck
					execErr = err
				} else {
					execErr = tx.Commit()
				}
			}
		} else {
			execErr = r.execGraphQLOps(ops, nil)
		}

		if execErr != nil {
			uq := fmt.Sprintf(
				"UPDATE graphql_changelog SET state = 'failed', error = %s WHERE id = %s",
				r.placeholder(1), r.placeholder(2),
			)
			r.DB.Exec(uq, execErr.Error(), entry.ID) //nolint:errcheck
			return fmt.Errorf("rolling back graphql policy %s: %w", entry.ID, execErr)
		}

		uq = fmt.Sprintf("UPDATE graphql_changelog SET state = 'rolled_back', completed_at = NOW() WHERE id = %s", r.placeholder(1))
		if _, err := r.DB.Exec(uq, entry.ID); err != nil {
			return fmt.Errorf("recording rollback of %s: %w", entry.ID, err)
		}
		fmt.Printf("  rolled back graphql policy: %s\n", entry.ID)
	}
	return nil
}

// Status returns the status of all known GraphQL policies.
func (r *GraphQLPolicyRunner) Status(entries []GraphQLPolicyEntry) ([]GraphQLPolicyStatus, error) {
	if err := r.ensureChangelogTable(); err != nil {
		return nil, fmt.Errorf("creating graphql_changelog table: %w", err)
	}
	rows, err := r.DB.Query("SELECT id, batch, state FROM graphql_changelog")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	states := map[string]GraphQLPolicyStatus{}
	for rows.Next() {
		var s GraphQLPolicyStatus
		if err := rows.Scan(&s.ID, &s.Batch, &s.State); err != nil {
			return nil, err
		}
		s.Applied = s.State == "applied"
		states[s.ID] = s
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var result []GraphQLPolicyStatus
	for _, entry := range entries {
		if s, ok := states[entry.ID]; ok {
			result = append(result, s)
		} else {
			result = append(result, GraphQLPolicyStatus{ID: entry.ID, State: "pending"})
		}
	}
	return result, nil
}

// DerivedExposure represents the computed GraphQL exposure set after replaying all policies.
type DerivedExposure struct {
	Model      string
	Operations []string // "list", "show", "create", "update", "delete"
}

// DerivedAction represents a registered custom controller action.
type DerivedAction struct {
	Name string
}

// DerivedGraphQLState is the full computed GraphQL exposure state.
type DerivedGraphQLState struct {
	Exposures []DerivedExposure
	Actions   []DerivedAction
}

// DeriveGraphQLState replays all GraphQL policies in order and returns the
// current exposure set.
func DeriveGraphQLState(entries []GraphQLPolicyEntry) DerivedGraphQLState {
	exposures := map[string]map[string]bool{} // model → set of operations
	actions := map[string]bool{}
	var modelOrder []string

	for _, entry := range entries {
		entry.Policy.Reset()
		entry.Policy.Up()
		for _, op := range entry.Policy.GetOperations() {
			switch op.Type {
			case "expose":
				if exposures[op.Model] == nil {
					exposures[op.Model] = map[string]bool{}
					modelOrder = append(modelOrder, op.Model)
				}
				for _, e := range op.Ops {
					exposures[op.Model][e.Type] = true
				}

			case "alter_expose":
				if exposures[op.Model] == nil {
					exposures[op.Model] = map[string]bool{}
					modelOrder = append(modelOrder, op.Model)
				}
				for _, e := range op.Ops {
					if isRemoveOp(e.Type) {
						delete(exposures[op.Model], e.Type[len("remove_"):])
					} else {
						exposures[op.Model][e.Type] = true
					}
				}

			case "unexpose":
				delete(exposures, op.Model)
				for i, m := range modelOrder {
					if m == op.Model {
						modelOrder = append(modelOrder[:i], modelOrder[i+1:]...)
						break
					}
				}

			case "controller_action":
				if op.Action != nil {
					actions[op.Action.Name] = true
				}

			case "remove_action":
				if op.Action != nil {
					delete(actions, op.Action.Name)
				}
			}
		}
	}

	var state DerivedGraphQLState
	for _, model := range modelOrder {
		if ops, ok := exposures[model]; ok && len(ops) > 0 {
			var opList []string
			for _, o := range []string{"list", "show", "create", "update", "delete"} {
				if ops[o] {
					opList = append(opList, o)
				}
			}
			state.Exposures = append(state.Exposures, DerivedExposure{
				Model:      model,
				Operations: opList,
			})
		}
	}
	for name := range actions {
		state.Actions = append(state.Actions, DerivedAction{Name: name})
	}
	return state
}
