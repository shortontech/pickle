package cooked

import (
	"fmt"
	"strings"
	"time"
)

// StaleVersionError is returned when an immutable table Update() detects that
// the entity's version_id has changed since the caller read it. This means
// another write occurred between the read and update — the caller must re-read
// and decide how to proceed.
type StaleVersionError struct {
	Table           string
	EntityID        string
	ExpectedVersion string
	ActualVersion   string
}

func (e *StaleVersionError) HTTPStatus() int { return 409 }
func (e *StaleVersionError) Error() string {
	return fmt.Sprintf(
		"stale version on %s (id=%s): expected version %s but found %s — "+
			"another write occurred between your read and update",
		e.Table, e.EntityID, e.ExpectedVersion, e.ActualVersion,
	)
}

// LockTimeoutError is returned when a lock acquisition exceeds the configured timeout.
type LockTimeoutError struct {
	Table    string
	LockType string // "row", "share", "advisory"
	Duration time.Duration
}

func (e *LockTimeoutError) HTTPStatus() int { return 503 }
func (e *LockTimeoutError) Error() string {
	return fmt.Sprintf(
		"%s lock on %s timed out after %s — another transaction is holding the lock",
		e.LockType, e.Table, e.Duration,
	)
}

// DeadlockError is returned when Postgres detects a deadlock and kills the transaction.
type DeadlockError struct {
	Table  string
	Detail string
}

func (e *DeadlockError) HTTPStatus() int { return 503 }
func (e *DeadlockError) Error() string {
	return fmt.Sprintf("deadlock detected on %s: %s", e.Table, e.Detail)
}

// NoWaitError is returned when NoWait() is used and the target row is already locked.
type NoWaitError struct {
	Table string
}

func (e *NoWaitError) HTTPStatus() int { return 409 }
func (e *NoWaitError) Error() string {
	return fmt.Sprintf("row in %s is locked by another transaction (NOWAIT)", e.Table)
}

// LockOutsideTransactionError is returned when Lock() is called on a query
// builder that is not associated with a transaction. Locks are released
// immediately after the query without a transaction, which is never correct.
type LockOutsideTransactionError struct {
	Table string
}

func (e *LockOutsideTransactionError) HTTPStatus() int { return 500 }
func (e *LockOutsideTransactionError) Error() string {
	return fmt.Sprintf(
		"Lock() on %s called outside a transaction — locks are released immediately "+
			"after the query without a transaction, which is never what you want",
		e.Table,
	)
}

// mapLockError inspects a database error and wraps it in a typed lock error
// if it matches a known Postgres error code. The detection is interface-based:
// both pgx and lib/pq expose a method or field for the SQLSTATE code.
func mapLockError(table string, err error) error {
	if err == nil {
		return nil
	}

	// Try the pgx interface: .Code() string
	type pgxErr interface {
		Code() string
	}
	// Try the lib/pq interface: .Get(byte) string where 'C' = Code
	type pqErr interface {
		Get(byte) string
	}

	var code string
	if e, ok := err.(pgxErr); ok {
		code = e.Code()
	} else if e, ok := err.(pqErr); ok {
		code = e.Get('C')
	} else {
		// Fallback: search the error message for known patterns
		msg := err.Error()
		if strings.Contains(msg, "deadlock detected") {
			return &DeadlockError{Table: table, Detail: msg}
		}
		if strings.Contains(msg, "lock timeout") || strings.Contains(msg, "could not obtain lock") {
			return &LockTimeoutError{Table: table, LockType: "row", Duration: 0}
		}
		if strings.Contains(msg, "could not obtain lock") && strings.Contains(msg, "NOWAIT") {
			return &NoWaitError{Table: table}
		}
		return err
	}

	switch code {
	case "40P01": // deadlock_detected
		return &DeadlockError{Table: table, Detail: err.Error()}
	case "55P03": // lock_not_available (timeout or NOWAIT)
		msg := err.Error()
		if strings.Contains(msg, "NOWAIT") || strings.Contains(strings.ToLower(msg), "nowait") {
			return &NoWaitError{Table: table}
		}
		return &LockTimeoutError{Table: table, LockType: "row", Duration: 0}
	default:
		return err
	}
}
