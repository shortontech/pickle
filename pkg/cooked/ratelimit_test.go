package cooked

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateBucketAllow(t *testing.T) {
	b := &rateBucket{
		tokens:   3,
		lastFill: now(),
		lastSeen: now(),
	}

	// Should allow 3 requests (burst of 3).
	for i := 0; i < 3; i++ {
		if !b.allow(1, 3) {
			t.Fatalf("request %d should have been allowed", i+1)
		}
	}

	// 4th should be denied.
	if b.allow(1, 3) {
		t.Fatal("4th request should have been denied")
	}
}

func TestRateBucketRetryAfter(t *testing.T) {
	b := &rateBucket{
		tokens:   0,
		lastFill: now(),
		lastSeen: now(),
	}

	retry := b.retryAfter(10)
	if retry != 1 {
		t.Fatalf("expected retry-after=1 at 10rps, got %d", retry)
	}

	retry = b.retryAfter(0.5)
	if retry != 2 {
		t.Fatalf("expected retry-after=2 at 0.5rps, got %d", retry)
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		remote   string
		expected string
	}{
		{
			name:     "X-Forwarded-For single",
			headers:  map[string]string{"X-Forwarded-For": "1.2.3.4"},
			remote:   "5.6.7.8:1234",
			expected: "1.2.3.4",
		},
		{
			name:     "X-Forwarded-For chain",
			headers:  map[string]string{"X-Forwarded-For": "1.2.3.4, 10.0.0.1"},
			remote:   "5.6.7.8:1234",
			expected: "1.2.3.4",
		},
		{
			name:     "X-Real-IP",
			headers:  map[string]string{"X-Real-IP": "9.8.7.6"},
			remote:   "5.6.7.8:1234",
			expected: "9.8.7.6",
		},
		{
			name:     "RemoteAddr with port",
			headers:  map[string]string{},
			remote:   "5.6.7.8:1234",
			expected: "5.6.7.8",
		},
		{
			name:     "RemoteAddr without port",
			headers:  map[string]string{},
			remote:   "5.6.7.8",
			expected: "5.6.7.8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.remote
			for k, v := range tt.headers {
				r.Header.Set(k, v)
			}
			got := clientIP(r)
			if got != tt.expected {
				t.Errorf("clientIP() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestRateLimiterStoreAllowAndDeny(t *testing.T) {
	store := &rateLimiterStore{
		rps:     100,
		burst:   2,
		enabled: true,
	}

	// First 2 requests allowed.
	if _, ok := store.allow("10.0.0.1"); !ok {
		t.Fatal("first request should be allowed")
	}
	if _, ok := store.allow("10.0.0.1"); !ok {
		t.Fatal("second request should be allowed")
	}

	// 3rd denied.
	if _, ok := store.allow("10.0.0.1"); ok {
		t.Fatal("third request should be denied")
	}

	// Different IP still allowed.
	if _, ok := store.allow("10.0.0.2"); !ok {
		t.Fatal("different IP should be allowed")
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	mw := RateLimit(100, 1)

	handler := func() Response {
		return Response{StatusCode: 200}
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:5555"
	ctx := NewContext(httptest.NewRecorder(), req)

	// First request: allowed.
	resp := mw(ctx, handler)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Second request: denied.
	resp = mw(ctx, handler)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", resp.StatusCode)
	}
	if resp.Headers["Retry-After"] == "" {
		t.Fatal("expected Retry-After header")
	}
}

func now() time.Time { return time.Now() }
