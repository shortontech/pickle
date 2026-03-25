package cooked

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthRateLimitByUserID(t *testing.T) {
	t.Setenv("AUTH_RATE_LIMIT_RPS", "100")
	t.Setenv("AUTH_RATE_LIMIT_BURST", "2")

	config := AuthRateLimit()
	mw := config.Middleware()

	handler := func() Response { return Response{StatusCode: 200} }

	// Two requests from user-1 should be allowed (burst=2).
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		ctx := NewContext(httptest.NewRecorder(), req)
		ctx.auth = &AuthInfo{UserID: "user-1", Role: "user"}

		resp := mw(ctx, handler)
		if resp.StatusCode != 200 {
			t.Fatalf("request %d from user-1 should be allowed, got %d", i+1, resp.StatusCode)
		}
	}

	// 3rd from user-1 should be denied.
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	ctx := NewContext(httptest.NewRecorder(), req)
	ctx.auth = &AuthInfo{UserID: "user-1", Role: "user"}

	resp := mw(ctx, handler)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("3rd request from user-1 should be denied, got %d", resp.StatusCode)
	}

	// user-2 from the same IP should still be allowed (keyed by user, not IP).
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	ctx = NewContext(httptest.NewRecorder(), req)
	ctx.auth = &AuthInfo{UserID: "user-2", Role: "user"}

	resp = mw(ctx, handler)
	if resp.StatusCode != 200 {
		t.Fatalf("user-2 should be allowed, got %d", resp.StatusCode)
	}
}

func TestAuthRateLimitFallsBackToIP(t *testing.T) {
	resetTrustedProxies()
	t.Setenv("TRUSTED_PROXIES", "")
	t.Setenv("AUTH_RATE_LIMIT_RPS", "100")
	t.Setenv("AUTH_RATE_LIMIT_BURST", "1")

	config := AuthRateLimit()
	mw := config.Middleware()

	handler := func() Response { return Response{StatusCode: 200} }

	// No auth set — should fall back to IP.
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "5.6.7.8:1234"
	ctx := NewContext(httptest.NewRecorder(), req)

	resp := mw(ctx, handler)
	if resp.StatusCode != 200 {
		t.Fatalf("first request should be allowed, got %d", resp.StatusCode)
	}

	// Second from same IP should be denied (burst=1).
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "5.6.7.8:1234"
	ctx = NewContext(httptest.NewRecorder(), req)

	resp = mw(ctx, handler)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second request should be denied, got %d", resp.StatusCode)
	}
}

func TestAuthRateLimitTiers(t *testing.T) {
	t.Setenv("AUTH_RATE_LIMIT_RPS", "100")
	t.Setenv("AUTH_RATE_LIMIT_BURST", "10")

	config := AuthRateLimit().Tiers(map[string]RateTier{
		"admin": {RPS: 100, Burst: 5},
		"free":  {RPS: 100, Burst: 1},
	})
	mw := config.Middleware()

	handler := func() Response { return Response{StatusCode: 200} }

	// Free user: burst=1, so second request should be denied.
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	ctx := NewContext(httptest.NewRecorder(), req)
	ctx.auth = &AuthInfo{UserID: "user-456", Role: "free"}

	resp := mw(ctx, handler)
	if resp.StatusCode != 200 {
		t.Fatalf("first free request should be allowed, got %d", resp.StatusCode)
	}

	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	ctx = NewContext(httptest.NewRecorder(), req)
	ctx.auth = &AuthInfo{UserID: "user-456", Role: "free"}

	resp = mw(ctx, handler)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second free request should be denied, got %d", resp.StatusCode)
	}

	// Admin user with same user ID: should still be allowed (namespaced by role).
	for i := 0; i < 5; i++ {
		req = httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		ctx = NewContext(httptest.NewRecorder(), req)
		ctx.auth = &AuthInfo{UserID: "user-456", Role: "admin"}

		resp = mw(ctx, handler)
		if resp.StatusCode != 200 {
			t.Fatalf("admin request %d should be allowed, got %d", i+1, resp.StatusCode)
		}
	}

	// Unknown role should get default burst (10).
	for i := 0; i < 10; i++ {
		req = httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		ctx = NewContext(httptest.NewRecorder(), req)
		ctx.auth = &AuthInfo{UserID: "user-789", Role: "unknown"}

		resp = mw(ctx, handler)
		if resp.StatusCode != 200 {
			t.Fatalf("unknown role request %d should use default, got %d", i+1, resp.StatusCode)
		}
	}
}

func TestAuthRateLimitCustomKeyFunc(t *testing.T) {
	t.Setenv("AUTH_RATE_LIMIT_RPS", "100")
	t.Setenv("AUTH_RATE_LIMIT_BURST", "1")

	config := AuthRateLimit().KeyFunc(func(ctx *Context) string {
		return ctx.Request().Header.Get("X-API-Key")
	})
	mw := config.Middleware()

	handler := func() Response { return Response{StatusCode: 200} }

	// First request with key-A: allowed.
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-API-Key", "key-A")
	ctx := NewContext(httptest.NewRecorder(), req)

	resp := mw(ctx, handler)
	if resp.StatusCode != 200 {
		t.Fatalf("first key-A request should be allowed, got %d", resp.StatusCode)
	}

	// Second with key-A: denied.
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-API-Key", "key-A")
	ctx = NewContext(httptest.NewRecorder(), req)

	resp = mw(ctx, handler)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second key-A request should be denied, got %d", resp.StatusCode)
	}

	// key-B: allowed (separate bucket).
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-API-Key", "key-B")
	ctx = NewContext(httptest.NewRecorder(), req)

	resp = mw(ctx, handler)
	if resp.StatusCode != 200 {
		t.Fatalf("first key-B request should be allowed, got %d", resp.StatusCode)
	}
}

func TestPeekJSON(t *testing.T) {
	body := `{"email":"test@example.com","password":"secret"}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := NewContext(httptest.NewRecorder(), req)

	// First call should extract the field.
	email := ctx.PeekJSON("email")
	if email != "test@example.com" {
		t.Fatalf("expected test@example.com, got %q", email)
	}

	// Second call should work (cached body).
	password := ctx.PeekJSON("password")
	if password != "secret" {
		t.Fatalf("expected secret, got %q", password)
	}

	// Missing field returns "".
	missing := ctx.PeekJSON("nonexistent")
	if missing != "" {
		t.Fatalf("expected empty string, got %q", missing)
	}

	// Body should still be readable after PeekJSON.
	buf := make([]byte, len(body))
	n, _ := ctx.Request().Body.Read(buf)
	if string(buf[:n]) != body {
		t.Fatalf("body should still be readable, got %q", string(buf[:n]))
	}
}

func TestPeekJSONLoginKeying(t *testing.T) {
	resetTrustedProxies()
	t.Setenv("TRUSTED_PROXIES", "")
	t.Setenv("AUTH_RATE_LIMIT_RPS", "100")
	t.Setenv("AUTH_RATE_LIMIT_BURST", "1")

	config := AuthRateLimit().KeyFunc(func(ctx *Context) string {
		email := ctx.PeekJSON("email")
		if email == "" {
			return ""
		}
		return "login:" + email
	})
	mw := config.Middleware()

	handler := func() Response { return Response{StatusCode: 200} }

	// First login attempt for victim@example.com from IP-1.
	body1 := `{"email":"victim@example.com","password":"attempt1"}`
	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(body1))
	req.RemoteAddr = "1.1.1.1:1234"
	ctx := NewContext(httptest.NewRecorder(), req)

	resp := mw(ctx, handler)
	if resp.StatusCode != 200 {
		t.Fatalf("first login should be allowed, got %d", resp.StatusCode)
	}

	// Second attempt for same email from different IP — should be denied (keyed by email).
	body2 := `{"email":"victim@example.com","password":"attempt2"}`
	req = httptest.NewRequest("POST", "/auth/login", strings.NewReader(body2))
	req.RemoteAddr = "2.2.2.2:1234"
	ctx = NewContext(httptest.NewRecorder(), req)

	resp = mw(ctx, handler)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second login for same email should be denied, got %d", resp.StatusCode)
	}

	// Different email should be allowed.
	body3 := `{"email":"other@example.com","password":"attempt1"}`
	req = httptest.NewRequest("POST", "/auth/login", strings.NewReader(body3))
	req.RemoteAddr = "1.1.1.1:1234"
	ctx = NewContext(httptest.NewRecorder(), req)

	resp = mw(ctx, handler)
	if resp.StatusCode != 200 {
		t.Fatalf("login for different email should be allowed, got %d", resp.StatusCode)
	}
}

func TestAuthRateLimitHeaders(t *testing.T) {
	t.Setenv("AUTH_RATE_LIMIT_RPS", "10")
	t.Setenv("AUTH_RATE_LIMIT_BURST", "5")

	config := AuthRateLimit()
	mw := config.Middleware()

	handler := func() Response { return Response{StatusCode: 200} }

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	ctx := NewContext(httptest.NewRecorder(), req)
	ctx.auth = &AuthInfo{UserID: "user-1"}

	resp := mw(ctx, handler)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if resp.Headers["X-RateLimit-Limit"] != "10" {
		t.Errorf("X-RateLimit-Limit = %q, want 10", resp.Headers["X-RateLimit-Limit"])
	}
	if resp.Headers["X-RateLimit-Remaining"] == "" {
		t.Error("X-RateLimit-Remaining should be set")
	}
	if resp.Headers["X-RateLimit-Reset"] == "" {
		t.Error("X-RateLimit-Reset should be set")
	}
}

func TestAuthRateLimit429Headers(t *testing.T) {
	t.Setenv("AUTH_RATE_LIMIT_RPS", "100")
	t.Setenv("AUTH_RATE_LIMIT_BURST", "1")

	config := AuthRateLimit()
	mw := config.Middleware()

	handler := func() Response { return Response{StatusCode: 200} }

	// Exhaust the bucket.
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	ctx := NewContext(httptest.NewRecorder(), req)
	ctx.auth = &AuthInfo{UserID: "header-test-user"}
	mw(ctx, handler)

	// This should be 429.
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	ctx = NewContext(httptest.NewRecorder(), req)
	ctx.auth = &AuthInfo{UserID: "header-test-user"}

	resp := mw(ctx, handler)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", resp.StatusCode)
	}
	if resp.Headers["Retry-After"] == "" {
		t.Error("Retry-After should be set on 429")
	}
	if resp.Headers["X-RateLimit-Limit"] == "" {
		t.Error("X-RateLimit-Limit should be set on 429")
	}
	if resp.Headers["X-RateLimit-Remaining"] != "0" {
		t.Errorf("X-RateLimit-Remaining should be 0 on 429, got %q", resp.Headers["X-RateLimit-Remaining"])
	}
}

func TestOnRateLimitCallback(t *testing.T) {
	t.Setenv("AUTH_RATE_LIMIT_RPS", "100")
	t.Setenv("AUTH_RATE_LIMIT_BURST", "1")

	var events []RateLimitEvent
	rateLimitCallback = func(ctx *Context, event RateLimitEvent) {
		events = append(events, event)
	}
	defer func() { rateLimitCallback = nil }()

	config := AuthRateLimit()
	mw := config.Middleware()

	handler := func() Response { return Response{StatusCode: 200} }

	// First request: allowed.
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	ctx := NewContext(httptest.NewRecorder(), req)
	ctx.auth = &AuthInfo{UserID: "callback-user"}
	mw(ctx, handler)

	// Second request: denied.
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	ctx = NewContext(httptest.NewRecorder(), req)
	ctx.auth = &AuthInfo{UserID: "callback-user"}
	mw(ctx, handler)

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	if events[0].Layer != "auth" || events[0].Key != "callback-user" || !events[0].Allowed {
		t.Errorf("event[0] = %+v, want auth layer, allowed", events[0])
	}
	if events[1].Layer != "auth" || events[1].Key != "callback-user" || events[1].Allowed {
		t.Errorf("event[1] = %+v, want auth layer, denied", events[1])
	}
	if events[0].Path != "/test" {
		t.Errorf("event[0].Path = %q, want /test", events[0].Path)
	}
}

func TestMiddlewareProviderInterface(t *testing.T) {
	t.Setenv("AUTH_RATE_LIMIT_RPS", "100")
	t.Setenv("AUTH_RATE_LIMIT_BURST", "100")

	config := AuthRateLimit()

	// AuthRateLimitConfig should implement MiddlewareProvider.
	var _ MiddlewareProvider = config

	// Should be passable directly to route registration.
	r := Routes(func(r *Router) {
		r.Get("/test", noop, config)
	})

	routes := r.AllRoutes()
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	if len(routes[0].Middleware) != 1 {
		t.Fatalf("expected 1 middleware, got %d", len(routes[0].Middleware))
	}
}
