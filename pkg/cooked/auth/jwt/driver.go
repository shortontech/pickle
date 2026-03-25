package jwt

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	pickle "github.com/shortontech/pickle/pkg/cooked"
)

// ErrInvalidToken is returned for all token validation failures.
// The specific reason is logged server-side but never exposed to callers.
var ErrInvalidToken = errors.New("jwt: invalid token")

// Driver implements JWT-based authentication using HMAC signing (HS256/HS384/HS512).
// All crypto uses Go's stdlib — no third-party JWT library.
// Tokens are tracked in a jwt_tokens table for revocation support.
type Driver struct {
	db        *sql.DB
	secret    string
	issuer    string
	expiry    int // seconds
	algorithm string
}

// NewDriver creates a JWT auth driver. Config is read from environment:
//   - JWT_SECRET: HMAC signing key (required)
//   - JWT_ISSUER: expected issuer claim (optional)
//   - JWT_EXPIRY: token lifetime in seconds (default: 3600)
//   - JWT_ALGORITHM: HS256, HS384, or HS512 (default: HS256)
func NewDriver(env func(string, string) string, db *sql.DB) *Driver {
	expiry := 3600
	if v := env("JWT_EXPIRY", ""); v != "" {
		// Simple atoi without importing strconv
		n := 0
		for _, c := range v {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			}
		}
		if n > 0 {
			expiry = n
		}
	}

	secret := env("JWT_SECRET", "")
	alg := env("JWT_ALGORITHM", "HS256")

	if secret == "" {
		panic("jwt: JWT_SECRET is required")
	}

	minLen := map[string]int{"HS256": 32, "HS384": 48, "HS512": 64}
	if min, ok := minLen[alg]; ok && len(secret) < min {
		panic(fmt.Sprintf("jwt: JWT_SECRET must be at least %d bytes for %s, got %d", min, alg, len(secret)))
	}

	return &Driver{
		db:        db,
		secret:    secret,
		issuer:    env("JWT_ISSUER", ""),
		expiry:    expiry,
		algorithm: alg,
	}
}

// Claims represents standard + custom JWT claims.
type Claims struct {
	JTI       string         `json:"jti,omitempty"`
	Subject   string         `json:"sub,omitempty"`
	Issuer    string         `json:"iss,omitempty"`
	ExpiresAt int64          `json:"exp,omitempty"`
	IssuedAt  int64          `json:"iat,omitempty"`
	Role      string         `json:"role,omitempty"`
	Extra     map[string]any `json:"-"`
}

// Authenticate extracts the Bearer token from the request, validates it,
// and returns AuthInfo on success.
func (d *Driver) Authenticate(r *http.Request) (*pickle.AuthInfo, error) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return nil, errors.New("missing bearer token")
	}
	token := h[7:]
	return d.ValidateToken(token)
}

// SignToken creates a signed JWT from the given claims and registers it in the
// jwt_tokens table for revocation tracking. The token is not valid unless it
// exists in the table.
func (d *Driver) SignToken(claims Claims) (string, error) {
	if d.secret == "" {
		return "", errors.New("jwt: secret not configured")
	}
	if d.db == nil {
		return "", errors.New("jwt: database not configured")
	}

	now := time.Now().Unix()
	if claims.IssuedAt == 0 {
		claims.IssuedAt = now
	}
	if claims.ExpiresAt == 0 && d.expiry > 0 {
		claims.ExpiresAt = now + int64(d.expiry)
	}
	if claims.Issuer == "" && d.issuer != "" {
		claims.Issuer = d.issuer
	}
	if claims.JTI == "" {
		claims.JTI = uuid.New().String()
	}

	alg := d.algorithm
	if alg == "" {
		alg = "HS256"
	}

	header := base64URLEncode([]byte(`{"alg":"` + alg + `","typ":"JWT"}`))

	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payloadEnc := base64URLEncode(payload)

	signingInput := header + "." + payloadEnc
	sig, err := hmacSign([]byte(signingInput), []byte(d.secret), alg)
	if err != nil {
		return "", err
	}

	// Register the token in the allowlist.
	expiresAt := time.Unix(claims.ExpiresAt, 0)
	_, err = d.db.Exec(
		"INSERT INTO jwt_tokens (jti, user_id, expires_at, created_at) VALUES ($1, $2, $3, NOW())",
		claims.JTI, claims.Subject, expiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("jwt: failed to register token: %w", err)
	}

	return signingInput + "." + base64URLEncode(sig), nil
}

// ValidateToken parses and validates a JWT string, returning AuthInfo on success.
func (d *Driver) ValidateToken(tokenStr string) (*pickle.AuthInfo, error) {
	if d.secret == "" {
		log.Printf("jwt: rejected token reason=secret_not_configured")
		return nil, ErrInvalidToken
	}

	parts := strings.SplitN(tokenStr, ".", 3)
	if len(parts) != 3 {
		log.Printf("jwt: rejected token reason=malformed_token")
		return nil, ErrInvalidToken
	}

	// Decode header and verify algorithm matches
	headerJSON, err := base64URLDecode(parts[0])
	if err != nil {
		log.Printf("jwt: rejected token reason=invalid_header_encoding")
		return nil, ErrInvalidToken
	}
	var header struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		log.Printf("jwt: rejected token reason=invalid_header")
		return nil, ErrInvalidToken
	}

	alg := d.algorithm
	if alg == "" {
		alg = "HS256"
	}
	if header.Alg != alg {
		log.Printf("jwt: rejected token reason=algorithm_mismatch header=%s expected=%s", header.Alg, alg)
		return nil, ErrInvalidToken
	}

	// Verify signature
	signingInput := parts[0] + "." + parts[1]
	sig, err := base64URLDecode(parts[2])
	if err != nil {
		log.Printf("jwt: rejected token reason=invalid_signature_encoding")
		return nil, ErrInvalidToken
	}
	if !hmacVerify([]byte(signingInput), sig, []byte(d.secret), alg) {
		log.Printf("jwt: rejected token reason=invalid_signature")
		return nil, ErrInvalidToken
	}

	// Decode claims
	claimsJSON, err := base64URLDecode(parts[1])
	if err != nil {
		log.Printf("jwt: rejected token reason=invalid_payload_encoding")
		return nil, ErrInvalidToken
	}
	var claims Claims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		log.Printf("jwt: rejected token reason=invalid_claims")
		return nil, ErrInvalidToken
	}

	// Check expiry
	if claims.ExpiresAt > 0 && time.Now().Unix() > claims.ExpiresAt {
		log.Printf("jwt: rejected token jti=%s reason=token_expired", claims.JTI)
		return nil, ErrInvalidToken
	}

	// Check issuer
	if d.issuer != "" && claims.Issuer != d.issuer {
		log.Printf("jwt: rejected token jti=%s reason=invalid_issuer issuer=%s expected=%s", claims.JTI, claims.Issuer, d.issuer)
		return nil, ErrInvalidToken
	}

	// Check revocation allowlist
	if d.db != nil && claims.JTI != "" {
		var revokedAt sql.NullTime
		err := d.db.QueryRow(
			"SELECT revoked_at FROM jwt_tokens WHERE jti = $1",
			claims.JTI,
		).Scan(&revokedAt)
		if err == sql.ErrNoRows {
			log.Printf("jwt: rejected token jti=%s reason=token_not_found", claims.JTI)
			return nil, ErrInvalidToken
		}
		if err != nil {
			log.Printf("jwt: rejected token jti=%s reason=database_error err=%v", claims.JTI, err)
			return nil, ErrInvalidToken
		}
		if revokedAt.Valid {
			log.Printf("jwt: rejected token jti=%s reason=token_revoked", claims.JTI)
			return nil, ErrInvalidToken
		}
	}

	return &pickle.AuthInfo{
		UserID: claims.Subject,
		Role:   claims.Role,
		Claims: claims,
	}, nil
}

// RevokeToken revokes a single token by JTI.
func (d *Driver) RevokeToken(jti string) error {
	if d.db == nil {
		return errors.New("jwt: database not configured")
	}
	_, err := d.db.Exec("UPDATE jwt_tokens SET revoked_at = NOW() WHERE jti = $1", jti)
	if err != nil {
		return fmt.Errorf("jwt: revoke token: %w", err)
	}
	return nil
}

// RevokeAllForUser revokes all tokens for the given user ID.
func (d *Driver) RevokeAllForUser(userID string) error {
	if d.db == nil {
		return errors.New("jwt: database not configured")
	}
	_, err := d.db.Exec("UPDATE jwt_tokens SET revoked_at = NOW() WHERE user_id = $1 AND revoked_at IS NULL", userID)
	if err != nil {
		return fmt.Errorf("jwt: revoke all for user: %w", err)
	}
	return nil
}

// --- internal helpers ---

func hmacHashFunc(alg string) func() hash.Hash {
	switch alg {
	case "HS384":
		return sha512.New384
	case "HS512":
		return sha512.New
	default:
		return sha256.New
	}
}

func hmacSign(input, secret []byte, alg string) ([]byte, error) {
	mac := hmac.New(hmacHashFunc(alg), secret)
	mac.Write(input)
	return mac.Sum(nil), nil
}

func hmacVerify(input, sig, secret []byte, alg string) bool {
	expected, err := hmacSign(input, secret, alg)
	if err != nil {
		return false
	}
	return hmac.Equal(sig, expected)
}

func base64URLEncode(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

func base64URLDecode(s string) ([]byte, error) {
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}
