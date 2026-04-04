package cooked

import (
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// env returns the value of the environment variable named by key,
// or fallback if the variable is not set. This is a local helper so
// the rate-limiter code is self-contained when embedded into generated
// packages that don't include the config module.
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// MiddlewareProvider is implemented by types that can produce a MiddlewareFunc.
// Route registration methods check for this interface and unwrap automatically,
// so builders like AuthRateLimitConfig can be passed directly as middleware.
type MiddlewareProvider interface {
	Middleware() MiddlewareFunc
}

// RateLimitEvent contains information about a rate limit check for observability.
type RateLimitEvent struct {
	Key       string  // identity key or IP
	Layer     string  // "ip" or "auth"
	Path      string  // request path
	RPS       float64 // configured limit
	Burst     int     // configured burst
	Remaining float64 // tokens remaining (0 for denied)
	Allowed   bool    // whether the request was allowed
}

// RateTier defines rate limit parameters for a specific role or plan.
type RateTier struct {
	RPS   float64
	Burst int
}

// AuthRateLimitConfig is the builder for identity-aware rate limiting.
// Each config owns its own rateLimiterStore instance.
type AuthRateLimitConfig struct {
	rps     float64
	burst   int
	keyFunc func(*Context) string
	tiers   map[string]RateTier
	store   *rateLimiterStore
}

// AuthRateLimit returns a middleware that rate-limits by authenticated identity.
// Uses ctx.Auth().UserID as the default key. Call builder methods to customize.
// Reads defaults from AUTH_RATE_LIMIT_RPS (default 30) and AUTH_RATE_LIMIT_BURST (default 60).
func AuthRateLimit() *AuthRateLimitConfig {
	rps, _ := strconv.ParseFloat(env("AUTH_RATE_LIMIT_RPS", "30"), 64)
	burst, _ := strconv.Atoi(env("AUTH_RATE_LIMIT_BURST", "60"))
	if burst < 1 {
		burst = 1
	}

	c := &AuthRateLimitConfig{
		rps:   rps,
		burst: burst,
	}
	c.store = &rateLimiterStore{
		rps:     rps,
		burst:   burst,
		enabled: true,
	}
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			c.store.cleanup()
		}
	}()
	return c
}

// RPS sets the requests-per-second limit.
func (c *AuthRateLimitConfig) RPS(rps float64) *AuthRateLimitConfig {
	c.rps = rps
	c.store.rps = rps
	return c
}

// Burst sets the burst size.
func (c *AuthRateLimitConfig) Burst(burst int) *AuthRateLimitConfig {
	if burst < 1 {
		burst = 1
	}
	c.burst = burst
	c.store.burst = burst
	return c
}

// KeyFunc sets a custom key extraction function. If the function returns "",
// the middleware falls back to IP-based limiting.
func (c *AuthRateLimitConfig) KeyFunc(fn func(*Context) string) *AuthRateLimitConfig {
	c.keyFunc = fn
	return c
}

// Tiers configures per-role rate limits. When tiers are set, the middleware
// reads ctx.Auth().Role and applies the matching tier. Unknown roles get the
// global default.
func (c *AuthRateLimitConfig) Tiers(tiers map[string]RateTier) *AuthRateLimitConfig {
	c.tiers = tiers
	return c
}

// Middleware returns the MiddlewareFunc for this config. Implements MiddlewareProvider.
func (c *AuthRateLimitConfig) Middleware() MiddlewareFunc {
	return c.build()
}

func (c *AuthRateLimitConfig) build() MiddlewareFunc {
	return func(ctx *Context, next func() Response) Response {
		// Resolve identity key.
		key := ""

		// 1. Try ctx.Auth().UserID (don't panic if auth not set).
		if ctx.auth != nil && ctx.auth.UserID != "" {
			key = ctx.auth.UserID
		}

		// 2. Try custom key function.
		if key == "" && c.keyFunc != nil {
			key = c.keyFunc(ctx)
		}

		// 3. Fall back to IP.
		if key == "" {
			key = clientIP(ctx.Request())
		}

		// Determine RPS and burst — check tiers if configured.
		rps := c.rps
		burst := c.burst
		if c.tiers != nil && ctx.auth != nil && ctx.auth.Role != "" {
			if tier, ok := c.tiers[ctx.auth.Role]; ok {
				rps = tier.RPS
				burst = tier.Burst
				// Namespace the key by role so role changes get separate buckets.
				key = ctx.auth.Role + ":" + key
			}
		}

		// Use a per-rps/burst bucket key to handle tier lookups in the shared store.
		bucket, ok := c.store.allowWithParams(key, rps, burst)

		// Fire OnRateLimit callback if configured.
		if rateLimitCallback != nil {
			remaining := 0.0
			if bucket != nil {
				bucket.mu.Lock()
				remaining = bucket.tokens
				bucket.mu.Unlock()
			}
			rateLimitCallback(ctx, RateLimitEvent{
				Key:       key,
				Layer:     "auth",
				Path:      ctx.Request().URL.Path,
				RPS:       rps,
				Burst:     burst,
				Remaining: remaining,
				Allowed:   ok,
			})
		}

		if ok {
			remaining := 0.0
			if bucket != nil {
				bucket.mu.Lock()
				remaining = bucket.tokens
				bucket.mu.Unlock()
			}
			resp := next()
			resp = setRateLimitHeaders(resp, rps, burst, remaining)
			return resp
		}

		retry := bucket.retryAfter(rps)
		resp := Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       map[string]string{"error": "rate limit exceeded"},
			Headers: map[string]string{
				"Content-Type": "application/json",
				"Retry-After":  strconv.Itoa(retry),
			},
		}
		resp = setRateLimitHeaders(resp, rps, burst, 0)
		return resp
	}
}

// setRateLimitHeaders adds X-RateLimit-* headers to a response.
func setRateLimitHeaders(resp Response, rps float64, burst int, remaining float64) Response {
	if resp.Headers == nil {
		resp.Headers = make(map[string]string)
	}
	resp.Headers["X-RateLimit-Limit"] = strconv.Itoa(int(rps))
	rem := int(remaining)
	if rem < 0 {
		rem = 0
	}
	resp.Headers["X-RateLimit-Remaining"] = strconv.Itoa(rem)
	// Reset is the time when the bucket would be full again.
	secondsToFull := 0.0
	if rps > 0 {
		deficit := float64(burst) - remaining
		if deficit > 0 {
			secondsToFull = deficit / rps
		}
	}
	resetTime := time.Now().Add(time.Duration(secondsToFull * float64(time.Second))).Unix()
	resp.Headers["X-RateLimit-Reset"] = strconv.FormatInt(resetTime, 10)
	return resp
}

// rateLimitCallback is the global OnRateLimit callback.
var rateLimitCallback func(ctx *Context, event RateLimitEvent)

// rateBucket is a token bucket for a single client IP.
type rateBucket struct {
	mu       sync.Mutex
	tokens   float64
	lastFill time.Time
	lastSeen time.Time
}

func (b *rateBucket) allow(rps float64, burst int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastFill).Seconds()
	b.tokens = math.Min(float64(burst), b.tokens+elapsed*rps)
	b.lastFill = now
	b.lastSeen = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// retryAfter returns seconds until the next token is available.
func (b *rateBucket) retryAfter(rps float64) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if rps <= 0 {
		return 1
	}
	deficit := 1.0 - b.tokens
	if deficit <= 0 {
		return 0
	}
	return int(math.Ceil(deficit / rps))
}

// rateLimiterStore holds per-IP buckets and configuration.
type rateLimiterStore struct {
	buckets sync.Map // string -> *rateBucket
	rps     float64
	burst   int
	enabled bool
}

func (s *rateLimiterStore) allow(ip string) (*rateBucket, bool) {
	val, _ := s.buckets.LoadOrStore(ip, &rateBucket{
		tokens:   float64(s.burst),
		lastFill: time.Now(),
		lastSeen: time.Now(),
	})
	b := val.(*rateBucket)
	return b, b.allow(s.rps, s.burst)
}

// allowWithParams is like allow but uses custom rps/burst (for tier support).
func (s *rateLimiterStore) allowWithParams(key string, rps float64, burst int) (*rateBucket, bool) {
	val, _ := s.buckets.LoadOrStore(key, &rateBucket{
		tokens:   float64(burst),
		lastFill: time.Now(),
		lastSeen: time.Now(),
	})
	b := val.(*rateBucket)
	return b, b.allow(rps, burst)
}

// cleanup removes buckets not seen in the last 10 minutes.
func (s *rateLimiterStore) cleanup() {
	cutoff := time.Now().Add(-10 * time.Minute)
	s.buckets.Range(func(key, val any) bool {
		b := val.(*rateBucket)
		b.mu.Lock()
		stale := b.lastSeen.Before(cutoff)
		b.mu.Unlock()
		if stale {
			s.buckets.Delete(key)
		}
		return true
	})
}

var (
	globalLimiter     *rateLimiterStore
	globalLimiterOnce sync.Once

	trustedProxies     []net.IPNet
	trustedProxiesAll  bool
	trustedProxiesOnce sync.Once
)

func initTrustedProxies() {
	trustedProxiesOnce.Do(func() {
		raw := env("TRUSTED_PROXIES", "")
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if raw == "all" {
			trustedProxiesAll = true
			return
		}
		for _, entry := range strings.Split(raw, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			// If it's a bare IP, make it a /32 or /128.
			if !strings.Contains(entry, "/") {
				ip := net.ParseIP(entry)
				if ip == nil {
					continue
				}
				if ip.To4() != nil {
					entry += "/32"
				} else {
					entry += "/128"
				}
			}
			_, cidr, err := net.ParseCIDR(entry)
			if err != nil {
				continue
			}
			trustedProxies = append(trustedProxies, *cidr)
		}
	})
}

// proxyHeadersTrusted returns true if the remote IP is in the TRUSTED_PROXIES list.
func proxyHeadersTrusted(remoteIP string) bool {
	initTrustedProxies()
	if trustedProxiesAll {
		return true
	}
	if len(trustedProxies) == 0 {
		return false
	}
	ip := net.ParseIP(remoteIP)
	if ip == nil {
		return false
	}
	for _, cidr := range trustedProxies {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// firstUntrustedIP walks an X-Forwarded-For chain right-to-left, skipping
// IPs that are in TRUSTED_PROXIES, and returns the first untrusted IP.
func firstUntrustedIP(xff string) string {
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := strings.TrimSpace(parts[i])
		if ip == "" {
			continue
		}
		if !proxyHeadersTrusted(ip) {
			return ip
		}
	}
	// All IPs in chain are trusted — return the leftmost.
	return strings.TrimSpace(parts[0])
}

func initGlobalLimiter() {
	globalLimiterOnce.Do(func() {
		enabled := env("RATE_LIMIT", "true")
		rps, _ := strconv.ParseFloat(env("RATE_LIMIT_RPS", "10"), 64)
		burst, _ := strconv.Atoi(env("RATE_LIMIT_BURST", "20"))

		if burst < 1 {
			burst = 1
		}

		globalLimiter = &rateLimiterStore{
			rps:     rps,
			burst:   burst,
			enabled: enabled == "true" && rps > 0,
		}

		if globalLimiter.enabled {
			go func() {
				ticker := time.NewTicker(5 * time.Minute)
				defer ticker.Stop()
				for range ticker.C {
					globalLimiter.cleanup()
				}
			}()
		}
	})
}

// checkRateLimit checks the global rate limiter. Returns a 429 Response if
// the request should be rejected, or nil if it should proceed. When the
// request is allowed, it returns rate limit headers via the second return value.
// Called by the router before middleware and handlers — this is framework-level
// protection, not middleware.
func checkRateLimit(r *http.Request) (*Response, map[string]string) {
	initGlobalLimiter()

	if !globalLimiter.enabled {
		return nil, nil
	}

	ip := clientIP(r)
	bucket, ok := globalLimiter.allow(ip)

	remaining := 0.0
	if bucket != nil {
		bucket.mu.Lock()
		remaining = bucket.tokens
		bucket.mu.Unlock()
	}

	// Fire OnRateLimit callback if configured.
	if rateLimitCallback != nil {
		ctx := &Context{request: r}
		rateLimitCallback(ctx, RateLimitEvent{
			Key:       ip,
			Layer:     "ip",
			Path:      r.URL.Path,
			RPS:       globalLimiter.rps,
			Burst:     globalLimiter.burst,
			Remaining: remaining,
			Allowed:   ok,
		})
	}

	if ok {
		// Build headers to attach to the eventual response.
		headers := make(map[string]string)
		dummyResp := setRateLimitHeaders(Response{Headers: headers}, globalLimiter.rps, globalLimiter.burst, remaining)
		return nil, dummyResp.Headers
	}

	retry := bucket.retryAfter(globalLimiter.rps)
	resp := Response{
		StatusCode: http.StatusTooManyRequests,
		Body:       map[string]string{"error": "rate limit exceeded"},
		Headers: map[string]string{
			"Content-Type": "application/json",
			"Retry-After":  strconv.Itoa(retry),
		},
	}
	resp = setRateLimitHeaders(resp, globalLimiter.rps, globalLimiter.burst, 0)
	return &resp, nil
}

// RateLimit returns a middleware that applies a per-IP rate limit with the
// given requests-per-second and burst size. Use this for per-route or
// per-group overrides on top of the framework-level global rate limit.
//
//	r.Group("/api/expensive", middleware.Auth, func(r *pickle.Router) {
//	    r.Post("/", handler, pickle.RateLimit(2, 5))
//	})
func RateLimit(rps float64, burst int) MiddlewareFunc {
	if burst < 1 {
		burst = 1
	}
	store := &rateLimiterStore{
		rps:     rps,
		burst:   burst,
		enabled: rps > 0,
	}
	// Cleanup goroutine for this per-route limiter.
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			store.cleanup()
		}
	}()

	return func(ctx *Context, next func() Response) Response {
		if !store.enabled {
			return next()
		}

		ip := clientIP(ctx.Request())
		bucket, ok := store.allow(ip)

		if ok {
			remaining := 0.0
			if bucket != nil {
				bucket.mu.Lock()
				remaining = bucket.tokens
				bucket.mu.Unlock()
			}
			resp := next()
			resp = setRateLimitHeaders(resp, store.rps, store.burst, remaining)
			return resp
		}

		retry := bucket.retryAfter(store.rps)
		resp := Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       map[string]string{"error": "rate limit exceeded"},
			Headers: map[string]string{
				"Content-Type": "application/json",
				"Retry-After":  strconv.Itoa(retry),
			},
		}
		resp = setRateLimitHeaders(resp, store.rps, store.burst, 0)
		return resp
	}
}

// clientIP extracts the client IP from the request. Proxy headers
// (X-Forwarded-For, X-Real-IP) are only trusted when the immediate
// remote address is in the TRUSTED_PROXIES list.
func clientIP(r *http.Request) string {
	remote := stripPort(r.RemoteAddr)

	if !proxyHeadersTrusted(remote) {
		return remote
	}

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return firstUntrustedIP(xff)
	}

	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	return remote
}

// stripPort removes the port from an address like "1.2.3.4:5678".
func stripPort(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}
