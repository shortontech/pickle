package jwt

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestSignAndValidate(t *testing.T) {
	d := NewDriver(testEnv(map[string]string{
		"JWT_SECRET": "test-secret-key",
		"JWT_ISSUER": "test-app",
	}), nil)

	token, err := d.SignToken(Claims{
		Subject: "user-123",
		Role:    "admin",
	})
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}

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
	d := NewDriver(testEnv(map[string]string{
		"JWT_SECRET": "test-secret-key",
	}), nil)

	token, err := d.SignToken(Claims{
		Subject:   "user-123",
		ExpiresAt: time.Now().Add(-1 * time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}

	_, err = d.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
	if err.Error() != "jwt: token expired" {
		t.Errorf("error = %q, want %q", err.Error(), "jwt: token expired")
	}
}

func TestInvalidSignature(t *testing.T) {
	d1 := NewDriver(testEnv(map[string]string{"JWT_SECRET": "secret-1"}), nil)
	d2 := NewDriver(testEnv(map[string]string{"JWT_SECRET": "secret-2"}), nil)

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
	d := NewDriver(testEnv(map[string]string{
		"JWT_SECRET": "test-secret",
		"JWT_ISSUER": "expected-app",
	}), nil)

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
	d := NewDriver(testEnv(map[string]string{
		"JWT_SECRET": "test-secret",
	}), nil)

	token, _ := d.SignToken(Claims{Subject: "user-456", Role: "user"})

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
	d := NewDriver(testEnv(map[string]string{
		"JWT_SECRET": "test-secret",
	}), nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	_, err := d.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for missing bearer token")
	}
}

func TestHS384(t *testing.T) {
	d := NewDriver(testEnv(map[string]string{
		"JWT_SECRET":    "test-secret",
		"JWT_ALGORITHM": "HS384",
	}), nil)

	token, err := d.SignToken(Claims{Subject: "user-789"})
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}

	info, err := d.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if info.UserID != "user-789" {
		t.Errorf("UserID = %q, want %q", info.UserID, "user-789")
	}
}

func TestHS512(t *testing.T) {
	d := NewDriver(testEnv(map[string]string{
		"JWT_SECRET":    "test-secret",
		"JWT_ALGORITHM": "HS512",
	}), nil)

	token, err := d.SignToken(Claims{Subject: "user-000"})
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}

	info, err := d.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if info.UserID != "user-000" {
		t.Errorf("UserID = %q, want %q", info.UserID, "user-000")
	}
}

// Verify the return type satisfies what AuthInfo expects
func TestAuthInfoType(t *testing.T) {
	d := NewDriver(testEnv(map[string]string{
		"JWT_SECRET": "test-secret",
	}), nil)

	token, _ := d.SignToken(Claims{Subject: "u1", Role: "admin"})
	info, _ := d.ValidateToken(token)

	// Verify it's a *pickle.AuthInfo
	var _ *pickle.AuthInfo = info
}

func TestNoSecret(t *testing.T) {
	d := NewDriver(testEnv(map[string]string{}), nil)

	_, err := d.SignToken(Claims{Subject: "user"})
	if err == nil {
		t.Fatal("expected error when secret is empty")
	}

	_, err = d.ValidateToken("some.fake.token")
	if err == nil {
		t.Fatal("expected error when secret is empty")
	}
}
