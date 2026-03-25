package cooked

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

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
		raw := Env("TRUSTED_PROXIES", "")
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
		enabled := Env("RATE_LIMIT", "true")
		rps, _ := strconv.ParseFloat(Env("RATE_LIMIT_RPS", "10"), 64)
		burst, _ := strconv.Atoi(Env("RATE_LIMIT_BURST", "20"))

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
// the request should be rejected, or nil if it should proceed.
// Called by the router before middleware and handlers — this is framework-level
// protection, not middleware.
func checkRateLimit(r *http.Request) *Response {
	initGlobalLimiter()

	if !globalLimiter.enabled {
		return nil
	}

	ip := clientIP(r)
	bucket, ok := globalLimiter.allow(ip)
	if ok {
		return nil
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
	return &resp
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
			return next()
		}

		retry := bucket.retryAfter(store.rps)
		return Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       map[string]string{"error": "rate limit exceeded"},
			Headers: map[string]string{
				"Content-Type": "application/json",
				"Retry-After":  strconv.Itoa(retry),
			},
		}
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
