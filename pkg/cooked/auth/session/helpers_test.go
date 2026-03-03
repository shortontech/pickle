package session

import (
	"net/http"
	"net/http/httptest"
	"testing"

	pickle "github.com/shortontech/pickle/pkg/cooked"
)

func TestDriverPanicsWhenNotInitialized(t *testing.T) {
	saved := activeDriver
	activeDriver = nil
	defer func() { activeDriver = saved }()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic when driver not initialized")
		}
	}()

	driver()
}

func TestSessionCookiesApply(t *testing.T) {
	session := &http.Cookie{Name: "session_id", Value: "sess-123"}
	csrf := &http.Cookie{Name: "csrf_token", Value: "tok-456"}

	sc := &SessionCookies{Session: session, CSRF: csrf}
	resp := pickle.Response{StatusCode: 200}
	resp = sc.Apply(resp)

	if len(resp.Cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(resp.Cookies))
	}
	if resp.Cookies[0].Name != "session_id" {
		t.Errorf("first cookie = %q, want session_id", resp.Cookies[0].Name)
	}
	if resp.Cookies[1].Name != "csrf_token" {
		t.Errorf("second cookie = %q, want csrf_token", resp.Cookies[1].Name)
	}
}

func TestSessionCookiesApplyWithoutCSRF(t *testing.T) {
	session := &http.Cookie{Name: "session_id", Value: "sess-123"}
	sc := &SessionCookies{Session: session}
	resp := pickle.Response{StatusCode: 200}
	resp = sc.Apply(resp)

	if len(resp.Cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(resp.Cookies))
	}
}

func TestDestroyWithoutCookieReturnsError(t *testing.T) {
	saved := activeDriver
	activeDriver = &Driver{cookieName: "session_id", ttl: 3600}
	defer func() { activeDriver = saved }()

	req := httptest.NewRequest("POST", "/logout", nil)
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	_, err := Destroy(ctx)
	if err == nil {
		t.Fatal("expected error when no session cookie")
	}
}

func TestGetWithoutCookieReturnsError(t *testing.T) {
	saved := activeDriver
	activeDriver = &Driver{cookieName: "session_id", ttl: 3600}
	defer func() { activeDriver = saved }()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	_, err := Get(ctx, "foo")
	if err == nil {
		t.Fatal("expected error when no session cookie")
	}
}

func TestPutWithoutCookieReturnsError(t *testing.T) {
	saved := activeDriver
	activeDriver = &Driver{cookieName: "session_id", ttl: 3600}
	defer func() { activeDriver = saved }()

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	err := Put(ctx, "foo", "bar")
	if err == nil {
		t.Fatal("expected error when no session cookie")
	}
}
