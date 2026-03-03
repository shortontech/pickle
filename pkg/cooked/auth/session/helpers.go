package session

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	pickle "github.com/shortontech/pickle/pkg/cooked"
)

func driver() *Driver {
	if activeDriver == nil {
		panic("session: driver not initialized — call session.NewDriver() first (usually via auth.Init)")
	}
	return activeDriver
}

// Create inserts a new session into the database and returns a Response with
// session and CSRF cookies set. The caller should chain this onto their response:
//
//	resp, err := session.Create(ctx, userID, role)
//	if err != nil { return ctx.Error(err) }
//	return ctx.JSON(200, data).WithCookie(resp.Session).WithCookie(resp.CSRF)
func Create(ctx *pickle.Context, userID, role string) (*SessionCookies, error) {
	d := driver()

	sessionID := uuid.New().String()
	expiresAt := time.Now().Add(time.Duration(d.ttl) * time.Second)

	_, err := d.db.Exec(
		"INSERT INTO sessions (id, user_id, role, expires_at, created_at, updated_at) VALUES ($1, $2, $3, $4, NOW(), NOW())",
		sessionID, userID, role, expiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("session: create: %w", err)
	}

	sessionCookie := &http.Cookie{
		Name:     d.cookieName,
		Value:    sessionID,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}

	cookies := &SessionCookies{Session: sessionCookie}

	// Auto-set CSRF cookie if SESSION_SECRET is configured.
	if len(csrfConfig.secret) > 0 {
		cookies.CSRF = &http.Cookie{
			Name:     csrfConfig.cookieName,
			Value:    generateCSRFToken(sessionID, csrfConfig.secret),
			Path:     "/",
			Expires:  expiresAt,
			HttpOnly: false,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
		}
	}

	return cookies, nil
}

// SessionCookies holds the cookies that should be set after session creation.
type SessionCookies struct {
	Session *http.Cookie
	CSRF    *http.Cookie
}

// Apply adds session cookies to a response, returning the modified response.
func (sc *SessionCookies) Apply(resp pickle.Response) pickle.Response {
	resp = resp.WithCookie(sc.Session)
	if sc.CSRF != nil {
		resp = resp.WithCookie(sc.CSRF)
	}
	return resp
}

// Destroy deletes the current session from the database and returns expired
// cookies that clear the session and CSRF cookies in the browser.
func Destroy(ctx *pickle.Context) (pickle.Response, error) {
	d := driver()

	sessionID, err := ctx.Cookie(d.cookieName)
	if err != nil {
		return pickle.Response{}, fmt.Errorf("session: no session cookie")
	}

	_, err = d.db.Exec("DELETE FROM sessions WHERE id = $1", sessionID)
	if err != nil {
		return pickle.Response{}, fmt.Errorf("session: destroy: %w", err)
	}

	expired := time.Unix(0, 0)
	resp := ctx.NoContent().
		WithCookie(&http.Cookie{
			Name:     d.cookieName,
			Value:    "",
			Path:     "/",
			Expires:  expired,
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   true,
		}).
		WithCookie(&http.Cookie{
			Name:    csrfConfig.cookieName,
			Value:   "",
			Path:    "/",
			Expires: expired,
			MaxAge:  -1,
			Secure:  true,
		})

	return resp, nil
}

// Get reads a value from the session's payload JSONB by key.
// Returns empty string if the key doesn't exist.
func Get(ctx *pickle.Context, key string) (string, error) {
	d := driver()

	sessionID, err := ctx.Cookie(d.cookieName)
	if err != nil {
		return "", fmt.Errorf("session: no session cookie")
	}

	var payloadRaw []byte
	err = d.db.QueryRow("SELECT payload FROM sessions WHERE id = $1", sessionID).Scan(&payloadRaw)
	if err != nil {
		return "", fmt.Errorf("session: get: %w", err)
	}

	if payloadRaw == nil {
		return "", nil
	}

	var payload map[string]any
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return "", fmt.Errorf("session: get: invalid payload JSON: %w", err)
	}

	val, ok := payload[key]
	if !ok {
		return "", nil
	}

	switch v := val.(type) {
	case string:
		return v, nil
	default:
		b, _ := json.Marshal(v)
		return string(b), nil
	}
}

// Put writes a key-value pair into the session's payload JSONB.
// Creates the payload object if it doesn't exist yet.
func Put(ctx *pickle.Context, key string, value any) error {
	d := driver()

	sessionID, err := ctx.Cookie(d.cookieName)
	if err != nil {
		return fmt.Errorf("session: no session cookie")
	}

	patch, err := json.Marshal(map[string]any{key: value})
	if err != nil {
		return fmt.Errorf("session: put: %w", err)
	}

	_, err = d.db.Exec(
		"UPDATE sessions SET payload = COALESCE(payload, '{}'::jsonb) || $1::jsonb, updated_at = NOW() WHERE id = $2",
		patch, sessionID,
	)
	if err != nil {
		return fmt.Errorf("session: put: %w", err)
	}

	return nil
}
