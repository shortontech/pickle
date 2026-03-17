package session

import (
	"database/sql"
	sqldriver "database/sql/driver"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pickle "github.com/shortontech/pickle/pkg/cooked"
)

// --- Minimal sql/driver mock ---

type mockDriver struct {
	open func(name string) (sqldriver.Conn, error)
}

func (d *mockDriver) Open(name string) (sqldriver.Conn, error) {
	return d.open(name)
}

type mockConn struct {
	prepare func(query string) (sqldriver.Stmt, error)
	exec    func(query string, args []sqldriver.Value) (sqldriver.Result, error)
	query   func(query string, args []sqldriver.Value) (sqldriver.Rows, error)
}

func (c *mockConn) Prepare(query string) (sqldriver.Stmt, error) {
	if c.prepare != nil {
		return c.prepare(query)
	}
	return &mockStmt{conn: c, query: query}, nil
}
func (c *mockConn) Close() error                    { return nil }
func (c *mockConn) Begin() (sqldriver.Tx, error) {
	return &mockTx{}, nil
}

type mockTx struct{}

func (t *mockTx) Commit() error   { return nil }
func (t *mockTx) Rollback() error { return nil }

type mockStmt struct {
	conn  *mockConn
	query string
}

func (s *mockStmt) Close() error    { return nil }
func (s *mockStmt) NumInput() int   { return -1 }
func (s *mockStmt) Exec(args []sqldriver.Value) (sqldriver.Result, error) {
	if s.conn.exec != nil {
		return s.conn.exec(s.query, args)
	}
	return &mockResult{}, nil
}
func (s *mockStmt) Query(args []sqldriver.Value) (sqldriver.Rows, error) {
	if s.conn.query != nil {
		return s.conn.query(s.query, args)
	}
	return &mockRows{}, nil
}

type mockResult struct{}

func (r *mockResult) LastInsertId() (int64, error) { return 0, nil }
func (r *mockResult) RowsAffected() (int64, error) { return 1, nil }

type mockRows struct {
	cols   []string
	data   [][]sqldriver.Value
	pos    int
	closed bool
}

func (r *mockRows) Columns() []string { return r.cols }
func (r *mockRows) Close() error      { r.closed = true; return nil }
func (r *mockRows) Next(dest []sqldriver.Value) error {
	if r.pos >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.pos])
	r.pos++
	return nil
}

// openDB returns a *sql.DB backed by the given mockConn.
func openDB(t *testing.T, conn *mockConn) *sql.DB {
	t.Helper()
	driverName := fmt.Sprintf("mock_%d", time.Now().UnixNano())
	sql.Register(driverName, &mockDriver{
		open: func(_ string) (sqldriver.Conn, error) { return conn, nil },
	})
	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// --- NewDriver / Driver accessors ---

func TestNewDriver_DefaultValues(t *testing.T) {
	db := openDB(t, &mockConn{})
	d := NewDriver(testEnv(map[string]string{
		"SESSION_SECRET": "s3cr3t",
	}), db)

	if d.CookieName() != "session_id" {
		t.Errorf("CookieName = %q, want session_id", d.CookieName())
	}
	if d.TTL() != 86400 {
		t.Errorf("TTL = %d, want 86400", d.TTL())
	}
}

func TestNewDriver_CustomValues(t *testing.T) {
	db := openDB(t, &mockConn{})
	d := NewDriver(testEnv(map[string]string{
		"SESSION_SECRET": "s3cr3t",
		"SESSION_COOKIE": "my_sess",
		"SESSION_TTL":    "3600",
	}), db)

	if d.CookieName() != "my_sess" {
		t.Errorf("CookieName = %q, want my_sess", d.CookieName())
	}
	if d.TTL() != 3600 {
		t.Errorf("TTL = %d, want 3600", d.TTL())
	}
}

func TestNewDriver_InvalidTTLKeepsDefault(t *testing.T) {
	db := openDB(t, &mockConn{})
	d := NewDriver(testEnv(map[string]string{
		"SESSION_SECRET": "s3cr3t",
		"SESSION_TTL":    "0",
	}), db)

	if d.TTL() != 86400 {
		t.Errorf("TTL = %d, want 86400 for zero value", d.TTL())
	}
}

func TestNewDriver_SetsActiveDriver(t *testing.T) {
	saved := activeDriver
	defer func() { activeDriver = saved }()

	db := openDB(t, &mockConn{})
	d := NewDriver(testEnv(map[string]string{"SESSION_SECRET": "s3cr3t"}), db)

	if activeDriver != d {
		t.Error("NewDriver should set activeDriver")
	}
}

func TestNewDriver_ConfiguresSessionCookieName(t *testing.T) {
	db := openDB(t, &mockConn{})
	NewDriver(testEnv(map[string]string{
		"SESSION_SECRET": "s3cr3t",
		"SESSION_COOKIE": "custom_sess",
	}), db)

	if sessionCookieName != "custom_sess" {
		t.Errorf("sessionCookieName = %q, want custom_sess", sessionCookieName)
	}
}

// --- Authenticate ---

func TestAuthenticate_MissingCookie(t *testing.T) {
	db := openDB(t, &mockConn{})
	d := &Driver{db: db, cookieName: "session_id", ttl: 86400}

	req := httptest.NewRequest("GET", "/", nil)
	_, err := d.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for missing cookie")
	}
}

func TestAuthenticate_NilDB(t *testing.T) {
	d := &Driver{db: nil, cookieName: "session_id", ttl: 86400}
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "sess-abc"})
	_, err := d.Authenticate(req)
	if err == nil {
		t.Fatal("expected error when db is nil")
	}
}

func TestAuthenticate_ValidSession(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour)

	conn := &mockConn{}
	conn.query = func(query string, args []sqldriver.Value) (sqldriver.Rows, error) {
		return &mockRows{
			cols: []string{"user_id", "role", "expires_at"},
			data: [][]sqldriver.Value{
				{"user-42", "admin", expiresAt},
			},
		}, nil
	}

	db := openDB(t, conn)
	d := &Driver{db: db, cookieName: "session_id", ttl: 86400}

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "sess-abc"})

	auth, err := d.Authenticate(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth.UserID != "user-42" {
		t.Errorf("UserID = %q, want user-42", auth.UserID)
	}
	if auth.Role != "admin" {
		t.Errorf("Role = %q, want admin", auth.Role)
	}
}

func TestAuthenticate_NoRows(t *testing.T) {
	conn := &mockConn{}
	conn.query = func(query string, args []sqldriver.Value) (sqldriver.Rows, error) {
		return &mockRows{cols: []string{"user_id", "role", "expires_at"}}, nil
	}

	db := openDB(t, conn)
	d := &Driver{db: db, cookieName: "session_id", ttl: 86400}

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "expired-sess"})

	_, err := d.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for no rows")
	}
}

func TestAuthenticate_DBError(t *testing.T) {
	conn := &mockConn{}
	conn.query = func(query string, args []sqldriver.Value) (sqldriver.Rows, error) {
		return nil, fmt.Errorf("db connection failed")
	}

	db := openDB(t, conn)
	d := &Driver{db: db, cookieName: "session_id", ttl: 86400}

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "sess-abc"})

	_, err := d.Authenticate(req)
	if err == nil {
		t.Fatal("expected error on db failure")
	}
}

// --- Create helper ---

func TestCreate_Success(t *testing.T) {
	saved := activeDriver
	defer func() { activeDriver = saved }()

	conn := &mockConn{}
	conn.exec = func(query string, args []sqldriver.Value) (sqldriver.Result, error) {
		return &mockResult{}, nil
	}
	db := openDB(t, conn)

	initCSRF(testEnv(map[string]string{"SESSION_SECRET": "test-secret"}))
	activeDriver = &Driver{db: db, cookieName: "session_id", ttl: 3600}

	req := httptest.NewRequest("POST", "/login", nil)
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	cookies, err := Create(ctx, "user-1", "member")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cookies.Session == nil {
		t.Fatal("Session cookie should be set")
	}
	if cookies.Session.Name != "session_id" {
		t.Errorf("Session.Name = %q, want session_id", cookies.Session.Name)
	}
	if !cookies.Session.HttpOnly {
		t.Error("Session cookie should be HttpOnly")
	}
	if !cookies.Session.Secure {
		t.Error("Session cookie should be Secure")
	}
	if cookies.CSRF == nil {
		t.Fatal("CSRF cookie should be set when SESSION_SECRET is configured")
	}
	if cookies.CSRF.HttpOnly {
		t.Error("CSRF cookie should not be HttpOnly")
	}
}

func TestCreate_WithoutCSRFSecret(t *testing.T) {
	saved := activeDriver
	defer func() { activeDriver = saved }()

	conn := &mockConn{}
	conn.exec = func(query string, args []sqldriver.Value) (sqldriver.Result, error) {
		return &mockResult{}, nil
	}
	db := openDB(t, conn)

	// No SESSION_SECRET — explicitly clear any previously set secret
	csrfConfig.secret = nil
	initCSRF(testEnv(map[string]string{}))
	activeDriver = &Driver{db: db, cookieName: "session_id", ttl: 3600}

	req := httptest.NewRequest("POST", "/login", nil)
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	cookies, err := Create(ctx, "user-1", "member")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cookies.CSRF != nil {
		t.Error("CSRF cookie should be nil when SESSION_SECRET not set")
	}
}

func TestCreate_DBError(t *testing.T) {
	saved := activeDriver
	defer func() { activeDriver = saved }()

	conn := &mockConn{}
	conn.exec = func(query string, args []sqldriver.Value) (sqldriver.Result, error) {
		return nil, fmt.Errorf("insert failed")
	}
	db := openDB(t, conn)
	activeDriver = &Driver{db: db, cookieName: "session_id", ttl: 3600}

	req := httptest.NewRequest("POST", "/login", nil)
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	_, err := Create(ctx, "user-1", "member")
	if err == nil {
		t.Fatal("expected error on DB failure")
	}
}

// --- Destroy helper ---

func TestDestroy_Success(t *testing.T) {
	saved := activeDriver
	defer func() { activeDriver = saved }()

	conn := &mockConn{}
	conn.exec = func(query string, args []sqldriver.Value) (sqldriver.Result, error) {
		return &mockResult{}, nil
	}
	db := openDB(t, conn)
	initCSRF(testEnv(map[string]string{"SESSION_SECRET": "test-secret"}))
	activeDriver = &Driver{db: db, cookieName: "session_id", ttl: 3600}

	req := httptest.NewRequest("POST", "/logout", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "sess-to-delete"})
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	resp, err := Destroy(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 204 {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}

	names := map[string]bool{}
	for _, c := range resp.Cookies {
		names[c.Name] = true
		if c.MaxAge != -1 {
			t.Errorf("cookie %q MaxAge = %d, want -1", c.Name, c.MaxAge)
		}
	}
	if !names["session_id"] {
		t.Error("should have expiring session_id cookie")
	}
	if !names["csrf_token"] {
		t.Error("should have expiring csrf_token cookie")
	}
}

func TestDestroy_DBError(t *testing.T) {
	saved := activeDriver
	defer func() { activeDriver = saved }()

	conn := &mockConn{}
	conn.exec = func(query string, args []sqldriver.Value) (sqldriver.Result, error) {
		return nil, fmt.Errorf("delete failed")
	}
	db := openDB(t, conn)
	activeDriver = &Driver{db: db, cookieName: "session_id", ttl: 3600}

	req := httptest.NewRequest("POST", "/logout", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "sess-abc"})
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	_, err := Destroy(ctx)
	if err == nil {
		t.Fatal("expected error on DB failure")
	}
}

// --- Get helper ---

func TestGet_Success(t *testing.T) {
	saved := activeDriver
	defer func() { activeDriver = saved }()

	conn := &mockConn{}
	conn.query = func(query string, args []sqldriver.Value) (sqldriver.Rows, error) {
		return &mockRows{
			cols: []string{"payload"},
			data: [][]sqldriver.Value{
				{[]byte(`{"cart_id":"abc-123","count":42}`)},
			},
		}, nil
	}
	db := openDB(t, conn)
	activeDriver = &Driver{db: db, cookieName: "session_id", ttl: 3600}

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "sess-abc"})
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	val, err := Get(ctx, "cart_id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "abc-123" {
		t.Errorf("val = %q, want abc-123", val)
	}
}

func TestGet_NonStringValue(t *testing.T) {
	saved := activeDriver
	defer func() { activeDriver = saved }()

	conn := &mockConn{}
	conn.query = func(query string, args []sqldriver.Value) (sqldriver.Rows, error) {
		return &mockRows{
			cols: []string{"payload"},
			data: [][]sqldriver.Value{
				{[]byte(`{"count":42}`)},
			},
		}, nil
	}
	db := openDB(t, conn)
	activeDriver = &Driver{db: db, cookieName: "session_id", ttl: 3600}

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "sess-abc"})
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	val, err := Get(ctx, "count")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "42" {
		t.Errorf("val = %q, want 42", val)
	}
}

func TestGet_MissingKey(t *testing.T) {
	saved := activeDriver
	defer func() { activeDriver = saved }()

	conn := &mockConn{}
	conn.query = func(query string, args []sqldriver.Value) (sqldriver.Rows, error) {
		return &mockRows{
			cols: []string{"payload"},
			data: [][]sqldriver.Value{
				{[]byte(`{"other":"value"}`)},
			},
		}, nil
	}
	db := openDB(t, conn)
	activeDriver = &Driver{db: db, cookieName: "session_id", ttl: 3600}

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "sess-abc"})
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	val, err := Get(ctx, "missing_key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "" {
		t.Errorf("val = %q, want empty string for missing key", val)
	}
}

func TestGet_NullPayload(t *testing.T) {
	saved := activeDriver
	defer func() { activeDriver = saved }()

	conn := &mockConn{}
	conn.query = func(query string, args []sqldriver.Value) (sqldriver.Rows, error) {
		return &mockRows{
			cols: []string{"payload"},
			data: [][]sqldriver.Value{
				{nil},
			},
		}, nil
	}
	db := openDB(t, conn)
	activeDriver = &Driver{db: db, cookieName: "session_id", ttl: 3600}

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "sess-abc"})
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	val, err := Get(ctx, "any_key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "" {
		t.Errorf("val = %q, want empty string for null payload", val)
	}
}

func TestGet_DBError(t *testing.T) {
	saved := activeDriver
	defer func() { activeDriver = saved }()

	conn := &mockConn{}
	conn.query = func(query string, args []sqldriver.Value) (sqldriver.Rows, error) {
		return nil, fmt.Errorf("query failed")
	}
	db := openDB(t, conn)
	activeDriver = &Driver{db: db, cookieName: "session_id", ttl: 3600}

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "sess-abc"})
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	_, err := Get(ctx, "key")
	if err == nil {
		t.Fatal("expected error on DB failure")
	}
}

func TestGet_InvalidJSON(t *testing.T) {
	saved := activeDriver
	defer func() { activeDriver = saved }()

	conn := &mockConn{}
	conn.query = func(query string, args []sqldriver.Value) (sqldriver.Rows, error) {
		return &mockRows{
			cols: []string{"payload"},
			data: [][]sqldriver.Value{
				{[]byte(`not-valid-json`)},
			},
		}, nil
	}
	db := openDB(t, conn)
	activeDriver = &Driver{db: db, cookieName: "session_id", ttl: 3600}

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "sess-abc"})
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	_, err := Get(ctx, "key")
	if err == nil {
		t.Fatal("expected error for invalid JSON payload")
	}
}

// --- Put helper ---

func TestPut_Success(t *testing.T) {
	saved := activeDriver
	defer func() { activeDriver = saved }()

	var capturedArgs []sqldriver.Value
	conn := &mockConn{}
	conn.exec = func(query string, args []sqldriver.Value) (sqldriver.Result, error) {
		capturedArgs = args
		return &mockResult{}, nil
	}
	db := openDB(t, conn)
	activeDriver = &Driver{db: db, cookieName: "session_id", ttl: 3600}

	req := httptest.NewRequest("POST", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "sess-abc"})
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	err := Put(ctx, "cart_id", "item-999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(capturedArgs) != 2 {
		t.Fatalf("expected 2 args, got %d", len(capturedArgs))
	}
	if capturedArgs[1] != "sess-abc" {
		t.Errorf("second arg = %v, want sess-abc", capturedArgs[1])
	}
}

func TestPut_DBError(t *testing.T) {
	saved := activeDriver
	defer func() { activeDriver = saved }()

	conn := &mockConn{}
	conn.exec = func(query string, args []sqldriver.Value) (sqldriver.Result, error) {
		return nil, fmt.Errorf("update failed")
	}
	db := openDB(t, conn)
	activeDriver = &Driver{db: db, cookieName: "session_id", ttl: 3600}

	req := httptest.NewRequest("POST", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "sess-abc"})
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	err := Put(ctx, "key", "value")
	if err == nil {
		t.Fatal("expected error on DB failure")
	}
}
