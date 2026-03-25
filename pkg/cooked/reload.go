package cooked

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
"sync/atomic"
	"time"
)

// scrubDSN replaces password components in DSN strings with "***".
// Handles: postgres://user:pass@host, mysql://user:pass@host,
// and key=value formats (password=secret, passwd=secret, secret=value).
func scrubDSN(s string) string {
	// Scrub URI-style: ://user:password@
	s = regexp.MustCompile(`(://[^:]+:)[^@]+(@)`).ReplaceAllString(s, "${1}***${2}")
	// Scrub key=value style: password=, passwd=, PASSWORD=, secret=
	s = regexp.MustCompile(`(?i)((?:password|passwd|secret)\s*=\s*)\S+`).ReplaceAllString(s, "${1}***")
	return s
}

func shortID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var lastReload atomic.Int64

const reloadCooldown = 300 // 5 minutes in seconds

// ConfigReloadFunc is the function called to reload configuration.
// Set this during app initialization to wire up config reloading.
// It should return the new config (for logging), a list of changed
// env var names, and any validation error.
var ConfigReloadFunc func() (any, []string, error)

// canReload checks whether enough time has passed since the last reload.
// Returns true and updates the timestamp atomically if allowed.
func canReload() bool {
	now := time.Now().Unix()
	last := lastReload.Load()
	if now-last < reloadCooldown {
		return false
	}
	return lastReload.CompareAndSwap(last, now)
}

// secondsUntilNextReload returns the number of seconds remaining in the cooldown.
func secondsUntilNextReload() int64 {
	last := lastReload.Load()
	now := time.Now().Unix()
	remaining := reloadCooldown - (now - last)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// WARNING: /pickle/config/reload, /pickle/health, and /pickle/metrics
// are operations endpoints. They must be network-restricted to internal
// traffic only (e.g., VPC, localhost, internal load balancer).
// Do not expose these to the public internet.

// handleConfigReload is the POST /pickle/config/reload handler.
// It re-reads environment variables, validates them, and atomically
// swaps the in-memory RuntimeConfig. Rate limited to 1 per 5 minutes.
func handleConfigReload(w http.ResponseWriter, r *http.Request) {
	source := r.RemoteAddr
	log.Printf("pickle: config reload requested source=%s", source)

	if ConfigReloadFunc == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]any{
			"status":  "error",
			"message": "config reload not configured",
		})
		return
	}

	if !canReload() {
		remaining := secondsUntilNextReload()
		log.Printf("pickle: config reload rate limited next_allowed_in=%ds", remaining)
		w.Header().Set("Retry-After", fmt.Sprintf("%d", remaining))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{
			"status":  "error",
			"message": "rate limited",
		})
		return
	}

	start := time.Now()
	_, changes, err := ConfigReloadFunc()
	if err != nil {
		reloadID := shortID()
		log.Printf("pickle: config reload failed reload_id=%s error=%q", reloadID, scrubDSN(err.Error()))
		// Roll back the timestamp so a retry is possible after fixing the issue
		lastReload.Store(0)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"status":    "error",
			"message":   "config reload failed — check application logs",
			"reload_id": reloadID,
		})
		return
	}

	duration := time.Since(start)
	log.Printf("pickle: config reloaded changes=%v duration=%s", changes, duration)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"status":      "ok",
		"reloaded_at": time.Now().UTC().Format(time.RFC3339),
		"changes":     changes,
	})
}

// RegisterPickleEndpoints registers Pickle's internal operations endpoints
// on the given ServeMux. These are infrastructure endpoints, not application routes.
//
// WARNING: /pickle/health, /pickle/config/reload, and /pickle/metrics
// are operations endpoints. They must be network-restricted to internal
// traffic only (e.g., VPC, localhost, internal load balancer).
// Do not expose these to the public internet.
func RegisterPickleEndpoints(mux *http.ServeMux) {
	mux.HandleFunc("GET /pickle/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /pickle/health/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.HandleFunc("POST /pickle/config/reload", handleConfigReload)
	mux.HandleFunc("POST /pickle/config/reload/", handleConfigReload)
}
