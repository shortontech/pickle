package session

import (
	"net/http"
	"net/http/httptest"
	"testing"

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

func setupCSRF(t *testing.T) {
	t.Helper()
	initCSRF(testEnv(map[string]string{
		"SESSION_SECRET": "test-secret-for-csrf",
	}))
	sessionCookieName = "session_id"
}

func TestGenerateAndValidateToken(t *testing.T) {
	secret := []byte("test-secret")
	sessionID := "sess-abc-123"

	token := generateCSRFToken(sessionID, secret)
	if token == "" {
		t.Fatal("generated token is empty")
	}

	if !validateCSRFToken(token, sessionID, secret) {
		t.Error("valid token rejected")
	}
}

func TestTokenInvalidForDifferentSession(t *testing.T) {
	secret := []byte("test-secret")

	token := generateCSRFToken("session-1", secret)
	if validateCSRFToken(token, "session-2", secret) {
		t.Error("token should be invalid for a different session ID")
	}
}

func TestTokenInvalidForDifferentSecret(t *testing.T) {
	token := generateCSRFToken("sess-1", []byte("secret-a"))
	if validateCSRFToken(token, "sess-1", []byte("secret-b")) {
		t.Error("token should be invalid with different secret")
	}
}

func TestTamperedTokenRejected(t *testing.T) {
	secret := []byte("test-secret")
	token := generateCSRFToken("sess-1", secret)

	// Replace the signature with a different valid hex string
	parts := token[:len(token)-64]
	tampered := parts + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if validateCSRFToken(tampered, "sess-1", secret) {
		t.Error("tampered token should be rejected")
	}
}

func TestMalformedTokensRejected(t *testing.T) {
	secret := []byte("test-secret")
	cases := []string{
		"",
		"no-dot-separator",
		"not-hex.not-hex",
		"aabb." + "zzzz", // invalid hex in sig
	}
	for _, tok := range cases {
		if validateCSRFToken(tok, "sess", secret) {
			t.Errorf("malformed token %q should be rejected", tok)
		}
	}
}

func TestCSRF_GETSetsTokenCookie(t *testing.T) {
	setupCSRF(t)

	req := httptest.NewRequest("GET", "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "sess-123"})
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	handler := func() pickle.Response {
		return ctx.JSON(200, map[string]string{"ok": "true"})
	}

	resp := CSRF(ctx, handler)

	// Should have a CSRF cookie set
	found := false
	for _, c := range resp.Cookies {
		if c.Name == "csrf_token" {
			found = true
			if c.HttpOnly {
				t.Error("CSRF cookie should not be HttpOnly (JS must read it)")
			}
			if !c.Secure {
				t.Error("CSRF cookie should be Secure")
			}
		}
	}
	if !found {
		t.Error("GET response should include csrf_token cookie")
	}
}

func TestCSRF_GETSkipsCookieIfAlreadyPresent(t *testing.T) {
	setupCSRF(t)

	req := httptest.NewRequest("GET", "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "sess-123"})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "existing-token"})
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	resp := CSRF(ctx, func() pickle.Response {
		return ctx.JSON(200, nil)
	})

	for _, c := range resp.Cookies {
		if c.Name == "csrf_token" {
			t.Error("should not re-set csrf_token cookie when already present")
		}
	}
}

func TestCSRF_POSTWithoutTokenReturns403(t *testing.T) {
	setupCSRF(t)

	req := httptest.NewRequest("POST", "/transfers", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "sess-123"})
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	resp := CSRF(ctx, func() pickle.Response {
		t.Fatal("handler should not be called")
		return pickle.Response{}
	})

	if resp.StatusCode != 403 {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestCSRF_POSTWithValidToken(t *testing.T) {
	setupCSRF(t)

	sessionID := "sess-123"
	token := generateCSRFToken(sessionID, csrfConfig.secret)

	req := httptest.NewRequest("POST", "/transfers", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	req.Header.Set("X-CSRF-TOKEN", token)
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	called := false
	resp := CSRF(ctx, func() pickle.Response {
		called = true
		return ctx.JSON(201, nil)
	})

	if !called {
		t.Error("handler should have been called")
	}
	if resp.StatusCode != 201 {
		t.Errorf("status = %d, want 201", resp.StatusCode)
	}
}

func TestCSRF_POSTWithInvalidTokenReturns403(t *testing.T) {
	setupCSRF(t)

	req := httptest.NewRequest("POST", "/transfers", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "sess-123"})
	req.Header.Set("X-CSRF-TOKEN", "bogus.token")
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	resp := CSRF(ctx, func() pickle.Response {
		t.Fatal("handler should not be called")
		return pickle.Response{}
	})

	if resp.StatusCode != 403 {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestCSRF_BearerTokenBypass(t *testing.T) {
	setupCSRF(t)

	req := httptest.NewRequest("POST", "/api/transfers", nil)
	req.Header.Set("Authorization", "Bearer some-jwt-token")
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	called := false
	CSRF(ctx, func() pickle.Response {
		called = true
		return ctx.JSON(200, nil)
	})

	if !called {
		t.Error("Bearer token requests should bypass CSRF")
	}
}

func TestCSRF_PUTAndDELETERequireToken(t *testing.T) {
	setupCSRF(t)

	for _, method := range []string{"PUT", "PATCH", "DELETE"} {
		req := httptest.NewRequest(method, "/resource/1", nil)
		req.AddCookie(&http.Cookie{Name: "session_id", Value: "sess-123"})
		w := httptest.NewRecorder()
		ctx := pickle.NewContext(w, req)

		resp := CSRF(ctx, func() pickle.Response {
			t.Fatalf("%s without token should not reach handler", method)
			return pickle.Response{}
		})

		if resp.StatusCode != 403 {
			t.Errorf("%s: status = %d, want 403", method, resp.StatusCode)
		}
	}
}

func TestCSRF_PanicsWithoutSecret(t *testing.T) {
	csrfConfig.secret = nil

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic when SESSION_SECRET is not set")
		}
	}()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ctx := pickle.NewContext(w, req)

	CSRF(ctx, func() pickle.Response { return pickle.Response{} })
}
