package cooked

import (
	"fmt"
	"time"
)

// LockStatus describes the current lock state for a table.
type LockStatus struct {
	Table      string       `json:"table"`
	LockedRows int          `json:"locked_rows"`
	Waiters    int          `json:"waiters"`
	Holders    []LockHolder `json:"holders"`
}

// LockHolder describes a single lock holder from pg_locks / pg_stat_activity.
type LockHolder struct {
	PID       int           `json:"pid"`
	Duration  time.Duration `json:"duration"`
	Query     string        `json:"query"`
	WaitCount int           `json:"wait_count"`
}

// queryLockInfo queries pg_locks and pg_stat_activity to return the current
// lock state for a table. This is a diagnostic tool for monitoring dashboards.
func queryLockInfo(db dbExecutor, table string) (*LockStatus, error) {
	status := &LockStatus{Table: table}

	// Count locked rows for this table
	row := db.QueryRow(`
		SELECT COUNT(*)
		FROM pg_locks l
		JOIN pg_class c ON l.relation = c.oid
		WHERE c.relname = $1
		  AND l.granted = true
		  AND l.locktype = 'relation'
	`, table)
	if err := row.Scan(&status.LockedRows); err != nil {
		return nil, fmt.Errorf("querying lock count for %s: %w", table, err)
	}

	// Count waiters
	row = db.QueryRow(`
		SELECT COUNT(*)
		FROM pg_locks l
		JOIN pg_class c ON l.relation = c.oid
		WHERE c.relname = $1
		  AND l.granted = false
	`, table)
	if err := row.Scan(&status.Waiters); err != nil {
		return nil, fmt.Errorf("querying waiter count for %s: %w", table, err)
	}

	// Get holder details
	rows, err := db.Query(`
		SELECT
			l.pid,
			COALESCE(EXTRACT(EPOCH FROM (NOW() - a.state_change))::int, 0),
			COALESCE(a.query, ''),
			(SELECT COUNT(*) FROM pg_locks w WHERE w.relation = l.relation AND w.granted = false)
		FROM pg_locks l
		JOIN pg_class c ON l.relation = c.oid
		LEFT JOIN pg_stat_activity a ON l.pid = a.pid
		WHERE c.relname = $1
		  AND l.granted = true
		  AND l.locktype = 'relation'
		ORDER BY a.state_change ASC
	`, table)
	if err != nil {
		return nil, fmt.Errorf("querying lock holders for %s: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var h LockHolder
		var durSec int
		if err := rows.Scan(&h.PID, &durSec, &h.Query, &h.WaitCount); err != nil {
			return nil, err
		}
		h.Duration = time.Duration(durSec) * time.Second
		status.Holders = append(status.Holders, h)
	}

	return status, rows.Err()
}
