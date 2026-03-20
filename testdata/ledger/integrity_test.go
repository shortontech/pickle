package main

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/shortontech/ledger/app/models"
	"github.com/shortontech/ledger/config"
)

func TestMain(m *testing.M) {
	config.Init()
	models.DB = config.Database.Open()
	os.Exit(m.Run())
}

func resetDB(t *testing.T) {
	t.Helper()
	// Clean tables in reverse FK order
	for _, table := range []string{"transactions", "accounts", "users", "sessions", "jwt_tokens", "oauth_tokens"} {
		_, err := models.DB.Exec(fmt.Sprintf("DELETE FROM %s", table))
		if err != nil {
			t.Fatalf("cleaning %s: %v", table, err)
		}
	}
}

func createTestUser(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := models.DB.Exec(
		`INSERT INTO users (id, name, email, password_hash, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, NOW(), NOW())`,
		id, "Test User", fmt.Sprintf("test-%s@example.com", id.String()[:8]), "hash",
	)
	if err != nil {
		t.Fatalf("creating test user: %v", err)
	}
	return id
}

// --- Append-Only (transactions) tests ---

func TestAppendOnlyCreateComputesHash(t *testing.T) {
	resetDB(t)
	userID := createTestUser(t)

	// Create an account first (immutable table)
	account := &models.Account{
		OwnerID:  userID,
		Name:     "Test Account",
		Currency: "USD",
		Type:     "checking",
		Active:   true,
	}
	if err := models.QueryAccount().SelectAll().Create(account); err != nil {
		t.Fatalf("creating account: %v", err)
	}

	// Create append-only transaction
	tx := &models.Transaction{
		AccountID: account.ID,
		Type:      "credit",
		Amount:    decimal.NewFromFloat(100.00),
		Currency:  "USD",
	}
	if err := models.QueryTransaction().SelectAll().Create(tx); err != nil {
		t.Fatalf("creating transaction: %v", err)
	}

	if len(tx.RowHash) != 32 {
		t.Fatalf("expected 32-byte row_hash, got %d bytes", len(tx.RowHash))
	}
	if len(tx.PrevHash) != 32 {
		t.Fatalf("expected 32-byte prev_hash, got %d bytes", len(tx.PrevHash))
	}

	// First row should have genesis (all zeros) as prev_hash
	allZero := true
	for _, b := range tx.PrevHash {
		if b != 0 {
			allZero = false
			break
		}
	}
	if !allZero {
		t.Error("first row's prev_hash should be genesis (all zeros)")
	}
}

func TestAppendOnlyChainLinks(t *testing.T) {
	resetDB(t)
	userID := createTestUser(t)

	account := &models.Account{
		OwnerID: userID, Name: "Chain Test", Currency: "USD", Type: "checking", Active: true,
	}
	if err := models.QueryAccount().SelectAll().Create(account); err != nil {
		t.Fatalf("creating account: %v", err)
	}

	// Create 3 transactions
	var txns []*models.Transaction
	for i := 0; i < 3; i++ {
		tx := &models.Transaction{
			AccountID: account.ID,
			Type:      "credit",
			Amount:    decimal.NewFromFloat(float64(i+1) * 10),
			Currency:  "USD",
		}
		if err := models.QueryTransaction().SelectAll().Create(tx); err != nil {
			t.Fatalf("creating transaction %d: %v", i, err)
		}
		txns = append(txns, tx)
	}

	// Verify chain: each tx's prev_hash = previous tx's row_hash
	for i := 1; i < len(txns); i++ {
		if !bytesEqual(txns[i].PrevHash, txns[i-1].RowHash) {
			t.Errorf("tx[%d].PrevHash != tx[%d].RowHash — chain is broken", i, i-1)
		}
	}

	// All hashes should be unique
	seen := map[string]bool{}
	for i, tx := range txns {
		key := fmt.Sprintf("%x", tx.RowHash)
		if seen[key] {
			t.Errorf("duplicate row_hash at position %d", i)
		}
		seen[key] = true
	}
}

func TestAppendOnlyVerifyChain(t *testing.T) {
	resetDB(t)
	userID := createTestUser(t)

	account := &models.Account{
		OwnerID: userID, Name: "Verify Test", Currency: "USD", Type: "checking", Active: true,
	}
	if err := models.QueryAccount().SelectAll().Create(account); err != nil {
		t.Fatalf("creating account: %v", err)
	}

	for i := 0; i < 5; i++ {
		tx := &models.Transaction{
			AccountID: account.ID,
			Type:      "credit",
			Amount:    decimal.NewFromFloat(float64(i+1) * 25),
			Currency:  "USD",
		}
		if err := models.QueryTransaction().SelectAll().Create(tx); err != nil {
			t.Fatalf("creating transaction %d: %v", i, err)
		}
	}

	// First verify each row individually by reading back from DB
	txns, err := models.QueryTransaction().SelectAll().OrderBy("id", "ASC").All()
	if err != nil {
		t.Fatalf("reading transactions: %v", err)
	}
	t.Logf("read back %d transactions", len(txns))
	for i, tx := range txns {
		t.Logf("  row[%d] ID=%s prev_hash=%x row_hash=%x amount=%s", i, tx.ID, tx.PrevHash[:4], tx.RowHash[:4], tx.Amount.String())
		if err := models.QueryTransaction().VerifyRow(&tx); err != nil {
			t.Errorf("VerifyRow[%d] failed: %v", i, err)
		}
	}

	// Chain should verify
	if err := models.QueryTransaction().VerifyChain(); err != nil {
		t.Fatalf("VerifyChain failed on untampered chain: %v", err)
	}
}

func TestAppendOnlyVerifyChainDetectsTampering(t *testing.T) {
	resetDB(t)
	userID := createTestUser(t)

	account := &models.Account{
		OwnerID: userID, Name: "Tamper Test", Currency: "USD", Type: "checking", Active: true,
	}
	if err := models.QueryAccount().SelectAll().Create(account); err != nil {
		t.Fatalf("creating account: %v", err)
	}

	for i := 0; i < 3; i++ {
		tx := &models.Transaction{
			AccountID: account.ID,
			Type:      "credit",
			Amount:    decimal.NewFromFloat(float64(i+1) * 50),
			Currency:  "USD",
		}
		if err := models.QueryTransaction().SelectAll().Create(tx); err != nil {
			t.Fatalf("creating transaction %d: %v", i, err)
		}
	}

	// Tamper with the second row's amount via raw SQL
	_, err := models.DB.Exec(`UPDATE transactions SET amount = 99999 WHERE id = (
		SELECT id FROM transactions ORDER BY id ASC OFFSET 1 LIMIT 1
	)`)
	if err != nil {
		t.Fatalf("tampering: %v", err)
	}

	// Chain should now fail
	err = models.QueryTransaction().VerifyChain()
	if err == nil {
		t.Fatal("VerifyChain should have detected tampering")
	}
	t.Logf("correctly detected tampering: %v", err)
}

func TestAppendOnlyVerifyRow(t *testing.T) {
	resetDB(t)
	userID := createTestUser(t)

	account := &models.Account{
		OwnerID: userID, Name: "VerifyRow Test", Currency: "USD", Type: "checking", Active: true,
	}
	if err := models.QueryAccount().SelectAll().Create(account); err != nil {
		t.Fatalf("creating account: %v", err)
	}

	tx := &models.Transaction{
		AccountID: account.ID,
		Type:      "credit",
		Amount:    decimal.NewFromFloat(100),
		Currency:  "USD",
	}
	if err := models.QueryTransaction().SelectAll().Create(tx); err != nil {
		t.Fatalf("creating transaction: %v", err)
	}

	// Verify the row we just created
	if err := models.QueryTransaction().VerifyRow(tx); err != nil {
		t.Fatalf("VerifyRow failed on untampered row: %v", err)
	}

	// Tamper with the struct and verify it fails
	tx.Amount = decimal.NewFromFloat(99999)
	if err := models.QueryTransaction().VerifyRow(tx); err == nil {
		t.Fatal("VerifyRow should have detected tampered amount")
	}
}

// --- Immutable (accounts) tests ---

func TestImmutableCreateComputesHash(t *testing.T) {
	resetDB(t)
	userID := createTestUser(t)

	account := &models.Account{
		OwnerID:  userID,
		Name:     "Hash Test",
		Currency: "EUR",
		Type:     "savings",
		Active:   true,
	}
	if err := models.QueryAccount().SelectAll().Create(account); err != nil {
		t.Fatalf("creating account: %v", err)
	}

	if len(account.RowHash) != 32 {
		t.Fatalf("expected 32-byte row_hash, got %d", len(account.RowHash))
	}
	if len(account.PrevHash) != 32 {
		t.Fatalf("expected 32-byte prev_hash, got %d", len(account.PrevHash))
	}
}

func TestImmutableUpdateExtendsChain(t *testing.T) {
	resetDB(t)
	userID := createTestUser(t)

	account := &models.Account{
		OwnerID: userID, Name: "Update Chain", Currency: "USD", Type: "checking", Active: true,
	}
	if err := models.QueryAccount().SelectAll().Create(account); err != nil {
		t.Fatalf("creating account: %v", err)
	}

	origHash := make([]byte, len(account.RowHash))
	copy(origHash, account.RowHash)

	// Update creates a new version
	account.Name = "Updated Name"
	if err := models.QueryAccount().SelectAll().Update(account); err != nil {
		t.Fatalf("updating account: %v", err)
	}

	// New version should chain to the original
	if bytesEqual(account.RowHash, origHash) {
		t.Error("updated row_hash should differ from original")
	}
	if !bytesEqual(account.PrevHash, origHash) {
		t.Error("updated prev_hash should equal original row_hash")
	}
}

func TestImmutableVerifyChain(t *testing.T) {
	resetDB(t)
	userID := createTestUser(t)

	for i := 0; i < 3; i++ {
		account := &models.Account{
			OwnerID: userID, Name: fmt.Sprintf("Account %d", i), Currency: "USD", Type: "checking", Active: true,
		}
		if err := models.QueryAccount().SelectAll().Create(account); err != nil {
			t.Fatalf("creating account %d: %v", i, err)
		}
	}

	if err := models.QueryAccount().VerifyChain(); err != nil {
		t.Fatalf("VerifyChain failed: %v", err)
	}
}

func TestImmutableStaleVersionError(t *testing.T) {
	resetDB(t)
	userID := createTestUser(t)

	account := &models.Account{
		OwnerID: userID, Name: "Stale Test", Currency: "USD", Type: "checking", Active: true,
	}
	if err := models.QueryAccount().SelectAll().Create(account); err != nil {
		t.Fatalf("creating account: %v", err)
	}

	// Simulate a concurrent update by reading the account twice
	copy1 := *account
	copy2 := *account

	// First update succeeds
	copy1.Name = "Updated by copy1"
	if err := models.QueryAccount().SelectAll().Update(&copy1); err != nil {
		t.Fatalf("first update: %v", err)
	}

	// Second update should fail — copy2 has a stale version_id
	copy2.Name = "Updated by copy2"
	err := models.QueryAccount().SelectAll().Update(&copy2)
	if err == nil {
		t.Fatal("expected StaleVersionError, got nil")
	}

	var stale *models.StaleVersionError
	if !isStaleVersionError(err, &stale) {
		t.Fatalf("expected StaleVersionError, got: %T: %v", err, err)
	}
	t.Logf("correctly got StaleVersionError: %v", stale)
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func isStaleVersionError(err error, target **models.StaleVersionError) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(*models.StaleVersionError); ok {
		*target = e
		return true
	}
	// Check wrapped errors
	type unwrapper interface{ Unwrap() error }
	if u, ok := err.(unwrapper); ok {
		return isStaleVersionError(u.Unwrap(), target)
	}
	return false
}

// Ensure sql import is used (for potential future use)
var _ *sql.DB
