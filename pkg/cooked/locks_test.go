package cooked

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestQueryBuilderLockForUpdate(t *testing.T) {
	q := Query[testModel]("users")
	q.Lock()
	query, _ := q.buildSelect()
	if !strings.HasSuffix(query, "FOR UPDATE") {
		t.Errorf("expected FOR UPDATE in query, got: %s", query)
	}
}

func TestQueryBuilderLockForShare(t *testing.T) {
	q := Query[testModel]("users")
	q.LockForShare()
	query, _ := q.buildSelect()
	if !strings.HasSuffix(query, "FOR SHARE") {
		t.Errorf("expected FOR SHARE in query, got: %s", query)
	}
}

func TestQueryBuilderLockSkipLocked(t *testing.T) {
	q := Query[testModel]("users")
	q.Lock().SkipLocked()
	query, _ := q.buildSelect()
	if !strings.HasSuffix(query, "FOR UPDATE SKIP LOCKED") {
		t.Errorf("expected FOR UPDATE SKIP LOCKED, got: %s", query)
	}
}

func TestQueryBuilderLockNoWait(t *testing.T) {
	q := Query[testModel]("users")
	q.Lock().NoWait()
	query, _ := q.buildSelect()
	if !strings.HasSuffix(query, "FOR UPDATE NOWAIT") {
		t.Errorf("expected FOR UPDATE NOWAIT, got: %s", query)
	}
}

func TestQueryBuilderLockOutsideTransactionFirst(t *testing.T) {
	q := Query[testModel]("users")
	q.Lock()
	_, err := q.First()
	var lockErr *LockOutsideTransactionError
	if !errors.As(err, &lockErr) {
		t.Errorf("expected LockOutsideTransactionError, got: %v", err)
	}
}

func TestQueryBuilderLockOutsideTransactionAll(t *testing.T) {
	q := Query[testModel]("users")
	q.Lock()
	_, err := q.All()
	var lockErr *LockOutsideTransactionError
	if !errors.As(err, &lockErr) {
		t.Errorf("expected LockOutsideTransactionError, got: %v", err)
	}
}

func TestQueryBuilderNoLockNoError(t *testing.T) {
	// Without Lock(), First/All should not check for transaction
	q := Query[testModel]("users")
	err := q.checkLockRequiresTransaction()
	if err != nil {
		t.Errorf("expected no error without Lock, got: %v", err)
	}
}

func TestQueryBuilderTimeout(t *testing.T) {
	q := Query[testModel]("users")
	q.Timeout(3 * time.Second)
	if q.lockTimeout != 3*time.Second {
		t.Errorf("expected 3s timeout, got: %v", q.lockTimeout)
	}
}

func TestImmutableQueryBuilderLockForUpdate(t *testing.T) {
	q := ImmutableQuery[testModel]("accounts", false)
	q.Lock()
	query, _ := q.buildSelect(0)
	if !strings.HasSuffix(query, "FOR UPDATE") {
		t.Errorf("expected FOR UPDATE in immutable query, got: %s", query)
	}
}

func TestImmutableQueryBuilderLockOutsideTransaction(t *testing.T) {
	q := ImmutableQuery[testModel]("accounts", false)
	q.Lock()
	_, err := q.First()
	var lockErr *LockOutsideTransactionError
	if !errors.As(err, &lockErr) {
		t.Errorf("expected LockOutsideTransactionError, got: %v", err)
	}
}
