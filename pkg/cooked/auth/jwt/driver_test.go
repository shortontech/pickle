package jwt

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	pickle "github.com/shortontech/pickle/pkg/cooked"
)

func testEnv(vals map[string]string) func(string, string) string {
	return func(key, fallback string) string {
		if v, ok := vals[key]; ok {
			return v
		}
		return fallback
	}
}

func testDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, mock
}

func testDriver(t *testing.T, env map[string]string) (*Driver, sqlmock.Sqlmock) {
	t.Helper()
	db, mock := testDB(t)
	d := NewDriver(testEnv(env), db)
	return d, mock
}

func expectInsert(mock sqlmock.Sqlmock) {
	mock.ExpectExec("INSERT INTO jwt_tokens").WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectValidToken(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SELECT revoked_at FROM jwt_tokens").
		WillReturnRows(sqlmock.NewRows([]string{"revoked_at"}).AddRow(nil))
}

func TestSignAndValidate(t *testing.T) {
	d, mock := testDriver(t, map[string]string{
		"JWT_SECRET": "test-secret-key",
		"JWT_ISSUER": "test-app",
	})
	expectInsert(mock)

	token, err := d.SignToken(Claims{
		Subject: "user-123",
		Role:    "admin",
	})
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}

	expectValidToken(mock)
	info, err := d.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}

	if info.UserID != "user-123" {
		t.Errorf("UserID = %q, want %q", info.UserID, "user-123")
	}
	if info.Role != "admin" {
		t.Errorf("Role = %q, want %q", info.Role, "admin")
	}
}

func TestExpiredToken(t *testing.T) {
	d, mock := testDriver(t, map[string]string{
		"JWT_SECRET": "test-secret-key",
	})
	expectInsert(mock)

	token, err := d.SignToken(Claims{
		Subject:   "user-123",
		ExpiresAt: time.Now().Add(-1 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}

	// No DB query expected — expiry check happens before DB lookup
	_, err = d.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
	if err.Error() != "jwt: token expired" {
		t.Errorf("error = %q, want %q", err.Error(), "jwt: token expired")
	}
}

func TestInvalidSignature(t *testing.T) {
	d1, mock1 := testDriver(t, map[string]string{"JWT_SECRET": "secret-1"})
	d2, _ := testDriver(t, map[string]string{"JWT_SECRET": "secret-2"})
	expectInsert(mock1)

	token, err := d1.SignToken(Claims{Subject: "user-123"})
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}

	_, err = d2.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
	if err.Error() != "jwt: invalid signature" {
		t.Errorf("error = %q, want %q", err.Error(), "jwt: invalid signature")
	}
}

func TestInvalidIssuer(t *testing.T) {
	d, mock := testDriver(t, map[string]string{
		"JWT_SECRET": "test-secret",
		"JWT_ISSUER": "expected-app",
	})
	expectInsert(mock)

	token, err := d.SignToken(Claims{
		Subject: "user-123",
		Issuer:  "wrong-app",
	})
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}

	_, err = d.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

func TestAuthenticate(t *testing.T) {
	d, mock := testDriver(t, map[string]string{
		"JWT_SECRET": "test-secret",
	})
	expectInsert(mock)

	token, _ := d.SignToken(Claims{Subject: "user-456", Role: "user"})

	expectValidToken(mock)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	info, err := d.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if info.UserID != "user-456" {
		t.Errorf("UserID = %q, want %q", info.UserID, "user-456")
	}
}

func TestAuthenticateMissingBearer(t *testing.T) {
	d, _ := testDriver(t, map[string]string{
		"JWT_SECRET": "test-secret",
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	_, err := d.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for missing bearer token")
	}
}

func TestHS384(t *testing.T) {
	d, mock := testDriver(t, map[string]string{
		"JWT_SECRET":    "test-secret",
		"JWT_ALGORITHM": "HS384",
	})
	expectInsert(mock)

	token, err := d.SignToken(Claims{Subject: "user-789"})
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}

	expectValidToken(mock)
	info, err := d.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if info.UserID != "user-789" {
		t.Errorf("UserID = %q, want %q", info.UserID, "user-789")
	}
}

func TestHS512(t *testing.T) {
	d, mock := testDriver(t, map[string]string{
		"JWT_SECRET":    "test-secret",
		"JWT_ALGORITHM": "HS512",
	})
	expectInsert(mock)

	token, err := d.SignToken(Claims{Subject: "user-000"})
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}

	expectValidToken(mock)
	info, err := d.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if info.UserID != "user-000" {
		t.Errorf("UserID = %q, want %q", info.UserID, "user-000")
	}
}

func TestAuthInfoType(t *testing.T) {
	d, mock := testDriver(t, map[string]string{
		"JWT_SECRET": "test-secret",
	})
	expectInsert(mock)
	expectValidToken(mock)

	token, _ := d.SignToken(Claims{Subject: "u1", Role: "admin"})
	info, _ := d.ValidateToken(token)

	var _ *pickle.AuthInfo = info
}

func TestNoSecret(t *testing.T) {
	d, _ := testDriver(t, map[string]string{})

	_, err := d.SignToken(Claims{Subject: "user"})
	if err == nil {
		t.Fatal("expected error when secret is empty")
	}

	_, err = d.ValidateToken("some.fake.token")
	if err == nil {
		t.Fatal("expected error when secret is empty")
	}
}

// --- Revocation tests ---

func TestRevokedTokenRejected(t *testing.T) {
	d, mock := testDriver(t, map[string]string{
		"JWT_SECRET": "test-secret",
	})
	expectInsert(mock)

	token, err := d.SignToken(Claims{Subject: "user-123"})
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}

	// Return revoked_at as non-null
	revokedAt := time.Now()
	mock.ExpectQuery("SELECT revoked_at FROM jwt_tokens").
		WillReturnRows(sqlmock.NewRows([]string{"revoked_at"}).AddRow(revokedAt))

	_, err = d.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for revoked token")
	}
	if err.Error() != "jwt: token revoked" {
		t.Errorf("error = %q, want %q", err.Error(), "jwt: token revoked")
	}
}

func TestMissingTokenRejected(t *testing.T) {
	d, mock := testDriver(t, map[string]string{
		"JWT_SECRET": "test-secret",
	})
	expectInsert(mock)

	token, err := d.SignToken(Claims{Subject: "user-123"})
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}

	// Return no rows — token not in allowlist
	mock.ExpectQuery("SELECT revoked_at FROM jwt_tokens").
		WillReturnRows(sqlmock.NewRows([]string{"revoked_at"}))

	_, err = d.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for missing token")
	}
	if err.Error() != "jwt: token not found (revoked or never issued)" {
		t.Errorf("error = %q, want %q", err.Error(), "jwt: token not found (revoked or never issued)")
	}
}

func TestRevokeToken(t *testing.T) {
	d, mock := testDriver(t, map[string]string{
		"JWT_SECRET": "test-secret",
	})

	mock.ExpectExec("UPDATE jwt_tokens SET revoked_at").WillReturnResult(sqlmock.NewResult(0, 1))

	err := d.RevokeToken("some-jti")
	if err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
}

func TestRevokeAllForUser(t *testing.T) {
	d, mock := testDriver(t, map[string]string{
		"JWT_SECRET": "test-secret",
	})

	mock.ExpectExec("UPDATE jwt_tokens SET revoked_at").WillReturnResult(sqlmock.NewResult(0, 3))

	err := d.RevokeAllForUser("user-123")
	if err != nil {
		t.Fatalf("RevokeAllForUser: %v", err)
	}
}

func TestSignTokenGeneratesJTI(t *testing.T) {
	d, mock := testDriver(t, map[string]string{
		"JWT_SECRET": "test-secret",
	})
	expectInsert(mock)

	token, err := d.SignToken(Claims{Subject: "user-123"})
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}

	// Decode and check JTI is present
	expectValidToken(mock)
	info, err := d.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	claims := info.Claims.(Claims)
	if claims.JTI == "" {
		t.Error("expected JTI to be generated")
	}
}
