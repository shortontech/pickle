package cooked

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestStaleVersionErrorMessage(t *testing.T) {
	err := &StaleVersionError{
		Table:           "transfers",
		EntityID:        "abc-123",
		ExpectedVersion: "v1",
		ActualVersion:   "v2",
	}
	msg := err.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
	if !contains(msg, "transfers") || !contains(msg, "abc-123") || !contains(msg, "v1") || !contains(msg, "v2") {
		t.Errorf("error message missing expected content: %s", msg)
	}
}

func TestLockTimeoutErrorMessage(t *testing.T) {
	err := &LockTimeoutError{
		Table:    "users",
		LockType: "row",
		Duration: 5 * time.Second,
	}
	msg := err.Error()
	if !contains(msg, "users") || !contains(msg, "row") || !contains(msg, "5s") {
		t.Errorf("error message missing expected content: %s", msg)
	}
}

func TestDeadlockErrorMessage(t *testing.T) {
	err := &DeadlockError{
		Table:  "accounts",
		Detail: "process 123 waits for ...",
	}
	msg := err.Error()
	if !contains(msg, "accounts") || !contains(msg, "deadlock") {
		t.Errorf("error message missing expected content: %s", msg)
	}
}

func TestNoWaitErrorMessage(t *testing.T) {
	err := &NoWaitError{Table: "jobs"}
	msg := err.Error()
	if !contains(msg, "jobs") || !contains(msg, "NOWAIT") {
		t.Errorf("error message missing expected content: %s", msg)
	}
}

func TestLockOutsideTransactionErrorMessage(t *testing.T) {
	err := &LockOutsideTransactionError{Table: "users"}
	msg := err.Error()
	if !contains(msg, "users") || !contains(msg, "outside a transaction") {
		t.Errorf("error message missing expected content: %s", msg)
	}
}

func TestMapLockErrorNil(t *testing.T) {
	if mapLockError("test", nil) != nil {
		t.Error("expected nil for nil error")
	}
}

func TestMapLockErrorPassthrough(t *testing.T) {
	original := fmt.Errorf("some random error")
	result := mapLockError("test", original)
	if result != original {
		t.Error("expected passthrough for unknown error")
	}
}

func TestMapLockErrorDeadlockFromMessage(t *testing.T) {
	original := fmt.Errorf("ERROR: deadlock detected (SQLSTATE 40P01)")
	result := mapLockError("test", original)
	var de *DeadlockError
	if !errors.As(result, &de) {
		t.Error("expected DeadlockError")
	}
}

func TestMapLockErrorDeadlockFromCode(t *testing.T) {
	original := &mockPgError{code: "40P01", msg: "deadlock detected"}
	result := mapLockError("test", original)
	var de *DeadlockError
	if !errors.As(result, &de) {
		t.Error("expected DeadlockError from code")
	}
}

func TestMapLockErrorLockTimeoutFromCode(t *testing.T) {
	original := &mockPgError{code: "55P03", msg: "could not obtain lock"}
	result := mapLockError("test", original)
	var lte *LockTimeoutError
	if !errors.As(result, &lte) {
		t.Error("expected LockTimeoutError from code")
	}
}

func TestMapLockErrorNoWaitFromCode(t *testing.T) {
	original := &mockPgError{code: "55P03", msg: "could not obtain lock NOWAIT"}
	result := mapLockError("test", original)
	var nwe *NoWaitError
	if !errors.As(result, &nwe) {
		t.Error("expected NoWaitError from code with NOWAIT")
	}
}

// mockPgError implements the pgx error interface for testing.
type mockPgError struct {
	code string
	msg  string
}

func (e *mockPgError) Error() string { return e.msg }
func (e *mockPgError) Code() string  { return e.code }

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
