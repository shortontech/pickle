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
)

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

// clientIP extracts the client IP from the request, checking proxy headers
// (X-Forwarded-For, X-Real-IP) before falling back to RemoteAddr.
func clientIP(r *http.Request) string {
	// X-Forwarded-For: client, proxy1, proxy2 — first entry is the client.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(ip)
		}
		return strings.TrimSpace(xff)
	}

	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Strip port from RemoteAddr.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
