package cooked

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
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

// resetTrustedProxies resets the trusted proxies state for testing.
func resetTrustedProxies() {
	trustedProxiesOnce = syncOnceZero()
	trustedProxies = nil
	trustedProxiesAll = false
}

// syncOnceZero returns a zero-value sync.Once (not yet executed).
func syncOnceZero() syncOnce { return syncOnce{} }

type syncOnce = sync.Once

func TestClientIPNoTrustedProxies(t *testing.T) {
	resetTrustedProxies()
	t.Setenv("TRUSTED_PROXIES", "")

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "5.6.7.8:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")

	got := clientIP(r)
	if got != "5.6.7.8" {
		t.Errorf("expected RemoteAddr when no trusted proxies, got %q", got)
	}
}

func TestClientIPTrustedCIDR(t *testing.T) {
	resetTrustedProxies()
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8")

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")

	got := clientIP(r)
	if got != "1.2.3.4" {
		t.Errorf("expected XFF client IP when proxy trusted, got %q", got)
	}
}

func TestClientIPUntrustedProxy(t *testing.T) {
	resetTrustedProxies()
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8")

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "5.6.7.8:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")

	got := clientIP(r)
	if got != "5.6.7.8" {
		t.Errorf("expected RemoteAddr when proxy not trusted, got %q", got)
	}
}

func TestClientIPTrustAll(t *testing.T) {
	resetTrustedProxies()
	t.Setenv("TRUSTED_PROXIES", "all")

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "5.6.7.8:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")

	got := clientIP(r)
	if got != "1.2.3.4" {
		t.Errorf("expected XFF client IP in trust-all mode, got %q", got)
	}
}

func TestClientIPMultiHopChain(t *testing.T) {
	resetTrustedProxies()
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8,172.16.0.0/12")

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.5, 172.16.0.1")

	got := clientIP(r)
	if got != "1.2.3.4" {
		t.Errorf("expected first untrusted IP from chain, got %q", got)
	}
}

func TestClientIPMultiHopPartialTrust(t *testing.T) {
	resetTrustedProxies()
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8")

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 9.9.9.9, 10.0.0.5")

	got := clientIP(r)
	// Walking right-to-left: 10.0.0.5 is trusted, 9.9.9.9 is not → return 9.9.9.9
	if got != "9.9.9.9" {
		t.Errorf("expected first untrusted IP from right, got %q", got)
	}
}

func TestClientIPXRealIPTrusted(t *testing.T) {
	resetTrustedProxies()
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8")

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Real-IP", "9.8.7.6")

	got := clientIP(r)
	if got != "9.8.7.6" {
		t.Errorf("expected X-Real-IP when proxy trusted, got %q", got)
	}
}

func TestClientIPRemoteAddrFallback(t *testing.T) {
	resetTrustedProxies()
	t.Setenv("TRUSTED_PROXIES", "all")

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "5.6.7.8:1234"

	got := clientIP(r)
	if got != "5.6.7.8" {
		t.Errorf("expected RemoteAddr fallback, got %q", got)
	}
}

func TestClientIPBareIPTrust(t *testing.T) {
	resetTrustedProxies()
	t.Setenv("TRUSTED_PROXIES", "10.0.0.1")

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")

	got := clientIP(r)
	if got != "1.2.3.4" {
		t.Errorf("expected XFF when bare IP trusted, got %q", got)
	}
}

func TestFirstUntrustedIPAllTrusted(t *testing.T) {
	resetTrustedProxies()
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8")

	// When all IPs in the chain are trusted, return leftmost.
	got := firstUntrustedIP("10.0.0.1, 10.0.0.2, 10.0.0.3")
	if got != "10.0.0.1" {
		t.Errorf("expected leftmost IP when all trusted, got %q", got)
	}
}

func TestProxyHeadersTrustedInvalidIP(t *testing.T) {
	resetTrustedProxies()
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8")

	if proxyHeadersTrusted("not-an-ip") {
		t.Error("invalid IP should not be trusted")
	}
}

func TestStripPort(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"1.2.3.4:5678", "1.2.3.4"},
		{"1.2.3.4", "1.2.3.4"},
		{"[::1]:8080", "::1"},
	}
	for _, tt := range tests {
		got := stripPort(tt.input)
		if got != tt.want {
			t.Errorf("stripPort(%q) = %q, want %q", tt.input, got, tt.want)
		}
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
	resetTrustedProxies()
	t.Setenv("TRUSTED_PROXIES", "")

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

// Verify net import is used (for compilation).
var _ = net.ParseIP

func now() time.Time { return time.Now() }
