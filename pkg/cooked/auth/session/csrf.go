package session

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	pickle "github.com/shortontech/pickle/pkg/cooked"
)

// csrfConfig holds CSRF settings initialized from environment variables.
var csrfConfig struct {
	secret     []byte
	cookieName string
}

// initCSRF configures CSRF protection. Called by NewDriver so CSRF is ready
// whenever the session driver is initialized.
func initCSRF(env func(string, string) string) {
	csrfConfig.cookieName = env("CSRF_COOKIE", "csrf_token")

	secret := env("SESSION_SECRET", "")
	if secret != "" {
		csrfConfig.secret = []byte(secret)
	}
}

// CSRF is middleware that protects against cross-site request forgery.
//
// It uses the HMAC double-submit cookie pattern: a token is generated from a
// random nonce HMAC-signed with the session ID, set as a JS-readable cookie,
// and must be echoed back in the X-CSRF-TOKEN header on state-changing requests.
//
// Safe methods (GET, HEAD, OPTIONS) pass through with a token cookie set.
// Requests with an Authorization: Bearer header are skipped (API clients).
func CSRF(ctx *pickle.Context, next func() pickle.Response) pickle.Response {
	if len(csrfConfig.secret) == 0 {
		panic("session: CSRF middleware requires SESSION_SECRET to be set")
	}

	// API clients using Bearer tokens don't need CSRF protection.
	if strings.HasPrefix(ctx.Request().Header.Get("Authorization"), "Bearer ") {
		return next()
	}

	sessionID := sessionIDFromRequest(ctx.Request())

	// Safe methods: ensure a CSRF cookie is set, then pass through.
	method := ctx.Request().Method
	if method == "GET" || method == "HEAD" || method == "OPTIONS" {
		resp := next()
		if _, err := ctx.Cookie(csrfConfig.cookieName); err != nil {
			resp = resp.WithCookie(newCSRFCookie(sessionID))
		}
		return resp
	}

	// State-changing methods: validate the token.
	headerToken := ctx.Request().Header.Get("X-CSRF-TOKEN")
	if headerToken == "" {
		return ctx.Forbidden("CSRF token missing")
	}

	if !validateCSRFToken(headerToken, sessionID, csrfConfig.secret) {
		return ctx.Forbidden("CSRF token invalid")
	}

	return next()
}

// sessionCookieName is set by the session driver during Init so the CSRF
// middleware knows which cookie holds the session ID.
var sessionCookieName = "session_id"

func sessionIDFromRequest(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func newCSRFCookie(sessionID string) *http.Cookie {
	token := generateCSRFToken(sessionID, csrfConfig.secret)
	return &http.Cookie{
		Name:     csrfConfig.cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false, // JS must be able to read this
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
}

// generateCSRFToken creates a token: hex(nonce) + "." + hex(hmac(nonce|sessionID, secret)).
func generateCSRFToken(sessionID string, secret []byte) string {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		panic("csrf: failed to generate random nonce: " + err.Error())
	}
	nonceHex := hex.EncodeToString(nonce)
	sig := computeHMAC(nonce, []byte(sessionID), secret)
	return nonceHex + "." + hex.EncodeToString(sig)
}

// validateCSRFToken checks that token is a valid nonce.signature for the given session.
func validateCSRFToken(token, sessionID string, secret []byte) bool {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return false
	}

	nonce, err := hex.DecodeString(parts[0])
	if err != nil {
		return false
	}

	sig, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}

	expected := computeHMAC(nonce, []byte(sessionID), secret)
	return hmac.Equal(sig, expected)
}

func computeHMAC(nonce, sessionID, secret []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write(nonce)
	mac.Write(sessionID)
	return mac.Sum(nil)
}
