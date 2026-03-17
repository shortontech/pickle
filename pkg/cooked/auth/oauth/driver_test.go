package oauth

import (
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	pickle "github.com/shortontech/pickle/pkg/cooked"
)

// --- helpers ---

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

func basicAuthHeader(clientID, clientSecret string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(clientID+":"+clientSecret))
}

func makeContext(r *http.Request) *pickle.Context {
	w := httptest.NewRecorder()
	return pickle.NewContext(w, r)
}

// --- NewDriver ---

func TestNewDriverDefaults(t *testing.T) {
	d, _ := testDriver(t, map[string]string{
		"OAUTH_CLIENT_ID":     "my-client",
		"OAUTH_CLIENT_SECRET": "my-secret",
	})
	if d.clientID != "my-client" {
		t.Errorf("clientID = %q, want %q", d.clientID, "my-client")
	}
	if d.clientSecret != "my-secret" {
		t.Errorf("clientSecret = %q, want %q", d.clientSecret, "my-secret")
	}
	if d.expiry != 3600 {
		t.Errorf("expiry = %d, want 3600", d.expiry)
	}
}

func TestNewDriverCustomExpiry(t *testing.T) {
	d, _ := testDriver(t, map[string]string{
		"OAUTH_TOKEN_EXPIRY": "7200",
	})
	if d.expiry != 7200 {
		t.Errorf("expiry = %d, want 7200", d.expiry)
	}
}

func TestNewDriverInvalidExpiryFallsBackToDefault(t *testing.T) {
	d, _ := testDriver(t, map[string]string{
		"OAUTH_TOKEN_EXPIRY": "abc",
	})
	if d.expiry != 3600 {
		t.Errorf("expiry = %d, want 3600 when OAUTH_TOKEN_EXPIRY is non-numeric", d.expiry)
	}
}

func TestNewDriverZeroExpiryFallsBackToDefault(t *testing.T) {
	d, _ := testDriver(t, map[string]string{
		"OAUTH_TOKEN_EXPIRY": "0",
	})
	if d.expiry != 3600 {
		t.Errorf("expiry = %d, want 3600 when OAUTH_TOKEN_EXPIRY is 0", d.expiry)
	}
}

// --- ValidateToken ---

func TestValidateTokenNilDB(t *testing.T) {
	d := &Driver{db: nil}
	_, err := d.ValidateToken("anytoken")
	if err == nil {
		t.Fatal("expected error for nil db")
	}
	if err.Error() != "oauth: database not configured" {
		t.Errorf("error = %q, want %q", err.Error(), "oauth: database not configured")
	}
}

func TestValidateTokenNotFound(t *testing.T) {
	d, mock := testDriver(t, map[string]string{})
	mock.ExpectQuery("SELECT client_id, expires_at FROM oauth_tokens").
		WillReturnRows(sqlmock.NewRows([]string{"client_id", "expires_at"}))

	_, err := d.ValidateToken("unknowntoken")
	if err == nil {
		t.Fatal("expected error for unknown token")
	}
	if err.Error() != "oauth: invalid token" {
		t.Errorf("error = %q, want %q", err.Error(), "oauth: invalid token")
	}
}

func TestValidateTokenExpired(t *testing.T) {
	d, mock := testDriver(t, map[string]string{})
	expired := time.Now().Add(-1 * time.Hour)
	mock.ExpectQuery("SELECT client_id, expires_at FROM oauth_tokens").
		WillReturnRows(sqlmock.NewRows([]string{"client_id", "expires_at"}).
			AddRow("my-client", expired))

	_, err := d.ValidateToken("expiredtoken")
	if err == nil {
		t.Fatal("expected error for expired token")
	}
	if err.Error() != "oauth: token expired" {
		t.Errorf("error = %q, want %q", err.Error(), "oauth: token expired")
	}
}

func TestValidateTokenValid(t *testing.T) {
	d, mock := testDriver(t, map[string]string{})
	future := time.Now().Add(1 * time.Hour)
	mock.ExpectQuery("SELECT client_id, expires_at FROM oauth_tokens").
		WillReturnRows(sqlmock.NewRows([]string{"client_id", "expires_at"}).
			AddRow("my-client", future))

	info, err := d.ValidateToken("validtoken")
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if info.UserID != "my-client" {
		t.Errorf("UserID = %q, want %q", info.UserID, "my-client")
	}
	if info.Role != "client" {
		t.Errorf("Role = %q, want %q", info.Role, "client")
	}
}

func TestValidateTokenReturnsAuthInfo(t *testing.T) {
	d, mock := testDriver(t, map[string]string{})
	future := time.Now().Add(1 * time.Hour)
	mock.ExpectQuery("SELECT client_id, expires_at FROM oauth_tokens").
		WillReturnRows(sqlmock.NewRows([]string{"client_id", "expires_at"}).
			AddRow("cli-99", future))

	info, _ := d.ValidateToken("tok")
	var _ *pickle.AuthInfo = info
}

// --- Authenticate ---

func TestAuthenticateMissingBearer(t *testing.T) {
	d, _ := testDriver(t, map[string]string{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	_, err := d.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for missing bearer")
	}
	if err.Error() != "missing bearer token" {
		t.Errorf("error = %q, want %q", err.Error(), "missing bearer token")
	}
}

func TestAuthenticateValidToken(t *testing.T) {
	d, mock := testDriver(t, map[string]string{})
	future := time.Now().Add(1 * time.Hour)
	mock.ExpectQuery("SELECT client_id, expires_at FROM oauth_tokens").
		WillReturnRows(sqlmock.NewRows([]string{"client_id", "expires_at"}).
			AddRow("cli-1", future))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer sometoken")

	info, err := d.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if info.UserID != "cli-1" {
		t.Errorf("UserID = %q, want %q", info.UserID, "cli-1")
	}
}

// --- parseBasicAuth ---

func TestParseBasicAuthMissingPrefix(t *testing.T) {
	_, _, ok := parseBasicAuth("Bearer sometoken")
	if ok {
		t.Error("expected ok=false for non-Basic header")
	}
}

func TestParseBasicAuthEmpty(t *testing.T) {
	_, _, ok := parseBasicAuth("")
	if ok {
		t.Error("expected ok=false for empty header")
	}
}

func TestParseBasicAuthInvalidBase64(t *testing.T) {
	_, _, ok := parseBasicAuth("Basic !!!notbase64")
	if ok {
		t.Error("expected ok=false for invalid base64")
	}
}

func TestParseBasicAuthMissingColon(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("nocreds"))
	_, _, ok := parseBasicAuth("Basic " + encoded)
	if ok {
		t.Error("expected ok=false when no colon in credentials")
	}
}

func TestParseBasicAuthValid(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("myid:mysecret"))
	id, secret, ok := parseBasicAuth("Basic " + encoded)
	if !ok {
		t.Fatal("expected ok=true for valid Basic auth")
	}
	if id != "myid" {
		t.Errorf("clientID = %q, want %q", id, "myid")
	}
	if secret != "mysecret" {
		t.Errorf("clientSecret = %q, want %q", secret, "mysecret")
	}
}

func TestParseBasicAuthSecretWithColon(t *testing.T) {
	// secret itself contains a colon — SplitN(2) must preserve it
	encoded := base64.StdEncoding.EncodeToString([]byte("myid:sec:ret"))
	id, secret, ok := parseBasicAuth("Basic " + encoded)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if id != "myid" {
		t.Errorf("clientID = %q", id)
	}
	if secret != "sec:ret" {
		t.Errorf("clientSecret = %q, want %q", secret, "sec:ret")
	}
}

// --- generateToken ---

func TestGenerateToken(t *testing.T) {
	tok, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken: %v", err)
	}
	if len(tok) != 64 {
		t.Errorf("token length = %d, want 64 (32 bytes hex-encoded)", len(tok))
	}
	for _, c := range tok {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("token contains non-hex character %q", c)
			break
		}
	}
}

func TestGenerateTokenUniqueness(t *testing.T) {
	tok1, _ := generateToken()
	tok2, _ := generateToken()
	if tok1 == tok2 {
		t.Error("two generated tokens should not be equal")
	}
}

// --- TokenEndpoint ---

func TestTokenEndpointWrongContentType(t *testing.T) {
	d, _ := testDriver(t, map[string]string{
		"OAUTH_CLIENT_ID":     "id",
		"OAUTH_CLIENT_SECRET": "sec",
	})

	body := strings.NewReader("grant_type=client_credentials")
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", body)
	req.Header.Set("Content-Type", "application/json")

	resp := d.TokenEndpoint(makeContext(req))
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	m := resp.Body.(map[string]string)
	if m["error"] != "invalid_request" {
		t.Errorf("error = %q, want invalid_request", m["error"])
	}
}

func TestTokenEndpointWrongGrantType(t *testing.T) {
	d, _ := testDriver(t, map[string]string{
		"OAUTH_CLIENT_ID":     "id",
		"OAUTH_CLIENT_SECRET": "sec",
	})

	body := strings.NewReader("grant_type=password")
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp := d.TokenEndpoint(makeContext(req))
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	m := resp.Body.(map[string]string)
	if m["error"] != "unsupported_grant_type" {
		t.Errorf("error = %q, want unsupported_grant_type", m["error"])
	}
}

func TestTokenEndpointMissingBasicAuth(t *testing.T) {
	d, _ := testDriver(t, map[string]string{
		"OAUTH_CLIENT_ID":     "id",
		"OAUTH_CLIENT_SECRET": "sec",
	})

	body := strings.NewReader("grant_type=client_credentials")
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// No Authorization header

	resp := d.TokenEndpoint(makeContext(req))
	if resp.StatusCode != 401 {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	m := resp.Body.(map[string]string)
	if m["error"] != "invalid_client" {
		t.Errorf("error = %q, want invalid_client", m["error"])
	}
}

func TestTokenEndpointWrongCredentials(t *testing.T) {
	d, _ := testDriver(t, map[string]string{
		"OAUTH_CLIENT_ID":     "correct-id",
		"OAUTH_CLIENT_SECRET": "correct-secret",
	})

	body := strings.NewReader("grant_type=client_credentials")
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", basicAuthHeader("wrong-id", "wrong-secret"))

	resp := d.TokenEndpoint(makeContext(req))
	if resp.StatusCode != 401 {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	m := resp.Body.(map[string]string)
	if m["error"] != "invalid_client" {
		t.Errorf("error = %q, want invalid_client", m["error"])
	}
}

func TestTokenEndpointSuccess(t *testing.T) {
	d, mock := testDriver(t, map[string]string{
		"OAUTH_CLIENT_ID":     "my-client",
		"OAUTH_CLIENT_SECRET": "my-secret",
	})
	mock.ExpectExec("INSERT INTO oauth_tokens").WillReturnResult(sqlmock.NewResult(0, 1))

	body := strings.NewReader("grant_type=client_credentials")
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", basicAuthHeader("my-client", "my-secret"))

	resp := d.TokenEndpoint(makeContext(req))
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	m := resp.Body.(map[string]any)
	if m["token_type"] != "bearer" {
		t.Errorf("token_type = %q, want bearer", m["token_type"])
	}
	token, ok := m["access_token"].(string)
	if !ok || len(token) != 64 {
		t.Errorf("access_token = %q, expected 64-char hex string", m["access_token"])
	}
	if m["expires_in"] != 3600 {
		t.Errorf("expires_in = %v, want 3600", m["expires_in"])
	}
}

func TestTokenEndpointSuccessCustomExpiry(t *testing.T) {
	d, mock := testDriver(t, map[string]string{
		"OAUTH_CLIENT_ID":     "my-client",
		"OAUTH_CLIENT_SECRET": "my-secret",
		"OAUTH_TOKEN_EXPIRY":  "1800",
	})
	mock.ExpectExec("INSERT INTO oauth_tokens").WillReturnResult(sqlmock.NewResult(0, 1))

	body := strings.NewReader("grant_type=client_credentials")
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", basicAuthHeader("my-client", "my-secret"))

	resp := d.TokenEndpoint(makeContext(req))
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	m := resp.Body.(map[string]any)
	if m["expires_in"] != 1800 {
		t.Errorf("expires_in = %v, want 1800", m["expires_in"])
	}
}
