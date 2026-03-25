package cooked

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// AuthInfo holds authentication state set by middleware.
type AuthInfo struct {
	UserID string
	Role   string
	Claims any
}

// Context wraps an HTTP request and response, providing helpers for controllers and middleware.
type Context struct {
	request  *http.Request
	response http.ResponseWriter
	params   map[string]string
	auth     *AuthInfo
	bodyBuf  []byte // cached request body for PeekJSON
	roles    []string
	isAdmin  bool
}

// NewContext creates a Context from an HTTP request/response pair.
func NewContext(w http.ResponseWriter, r *http.Request) *Context {
	return &Context{
		request:  r,
		response: w,
		params:   make(map[string]string),
	}
}

// Request returns the underlying *http.Request.
func (c *Context) Request() *http.Request {
	return c.request
}

// ResponseWriter returns the underlying http.ResponseWriter.
func (c *Context) ResponseWriter() http.ResponseWriter {
	return c.response
}

// Param returns a URL path parameter by name (e.g. :id).
func (c *Context) Param(name string) string {
	val, ok := c.params[name]
	if !ok {
		panic("pickle: ctx.Param(\"" + name + "\") — no such route parameter (check route definition and spelling)")
	}
	return val
}

// SetParam sets a URL path parameter. Used by the generated route handler.
func (c *Context) SetParam(name, value string) {
	c.params[name] = value
}

// ParamUUID returns a URL path parameter parsed as a UUID.
// Returns the parsed UUID and an error if the param is not a valid UUID.
func (c *Context) ParamUUID(name string) (uuid.UUID, error) {
	return uuid.Parse(c.Param(name))
}

// Cookie returns the value of the named cookie, or an error if not present.
func (c *Context) Cookie(name string) (string, error) {
	cookie, err := c.request.Cookie(name)
	if err != nil {
		return "", err
	}
	return cookie.Value, nil
}

// Query returns a query string parameter by name.
func (c *Context) Query(name string) string {
	return c.request.URL.Query().Get(name)
}

// BearerToken extracts the token from the Authorization: Bearer header.
func (c *Context) BearerToken() string {
	h := c.request.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return h[7:]
}

// SetAuth stores authentication info (called by auth middleware).
// Panics if claims is not *AuthInfo — use &AuthInfo{UserID: ..., Claims: ...} instead of a raw type.
func (c *Context) SetAuth(claims any) {
	switch v := claims.(type) {
	case *AuthInfo:
		c.auth = v
	default:
		panic(fmt.Sprintf("pickle: SetAuth() requires *AuthInfo, got %T — wrap your claims: &pickle.AuthInfo{UserID: id, Claims: v}", claims))
	}
}

// Auth returns the authenticated user info. Panics if no auth middleware ran —
// calling Auth() on an unauthenticated route is a programming error.
func (c *Context) Auth() *AuthInfo {
	if c.auth == nil {
		panic("pickle: ctx.Auth() called without auth middleware — add Auth middleware to this route")
	}
	return c.auth
}

// ResourceQuery is implemented by generated query types to support ctx.Resource().
// It fetches a single record and returns it serialized for the given owner.
type ResourceQuery interface {
	FetchResource(ownerID string) (any, error)
}

// ResourceListQuery is implemented by generated query types to support ctx.Resources().
// It fetches all matching records and returns them serialized for the given owner.
type ResourceListQuery interface {
	FetchResources(ownerID string) (any, error)
}

// Resource executes a query that returns a single record, serialized based on
// the authenticated user's ownership. Returns 404 if the record is not found.
func (c *Context) Resource(q ResourceQuery) Response {
	ownerID := ""
	if c.auth != nil {
		ownerID = c.auth.UserID
	}
	result, err := q.FetchResource(ownerID)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return c.NotFound("not found")
		}
		return c.Error(err)
	}
	return c.JSON(http.StatusOK, result)
}

// Resources executes a query that returns a collection of records, serialized
// based on the authenticated user's ownership.
func (c *Context) Resources(q ResourceListQuery) Response {
	ownerID := ""
	if c.auth != nil {
		ownerID = c.auth.UserID
	}
	result, err := q.FetchResources(ownerID)
	if err != nil {
		return c.Error(err)
	}
	return c.JSON(http.StatusOK, result)
}

// JSON returns a JSON response with the given status code and data.
func (c *Context) JSON(status int, data any) Response {
	return Response{
		StatusCode: status,
		Body:       data,
		Headers:    map[string]string{"Content-Type": "application/json"},
	}
}

// NoContent returns a 204 No Content response.
func (c *Context) NoContent() Response {
	return Response{StatusCode: http.StatusNoContent}
}

// httpStatusError is implemented by errors that know their own HTTP status code.
// The query builder's typed errors (StaleVersionError, DeadlockError, etc.)
// implement this interface, keeping the mapping close to the error definition
// rather than requiring the HTTP package to know about every error type.
type httpStatusError interface {
	HTTPStatus() int
}

// Error maps an error to an appropriate HTTP response. Errors that implement
// httpStatusError produce their own status code; unknown errors return 500.
func (c *Context) Error(err error) Response {
	var httpErr httpStatusError
	if errors.As(err, &httpErr) {
		status := httpErr.HTTPStatus()
		if status >= 500 {
			log.Printf("internal error: %v", err)
			return c.JSON(status, map[string]string{"error": "internal server error"})
		}
		return c.JSON(status, map[string]string{"error": err.Error()})
	}

	log.Printf("internal error: %v", err)
	return Response{
		StatusCode: http.StatusInternalServerError,
		Body:       map[string]string{"error": "internal server error"},
		Headers:    map[string]string{"Content-Type": "application/json"},
	}
}

// NotFound returns a 404 response with a message.
func (c *Context) NotFound(msg string) Response {
	return Response{
		StatusCode: http.StatusNotFound,
		Body:       map[string]string{"error": msg},
		Headers:    map[string]string{"Content-Type": "application/json"},
	}
}

// Unauthorized returns a 401 response with a message.
func (c *Context) Unauthorized(msg string) Response {
	return Response{
		StatusCode: http.StatusUnauthorized,
		Body:       map[string]string{"error": msg},
		Headers:    map[string]string{"Content-Type": "application/json"},
	}
}

// Forbidden returns a 403 response with a message.
func (c *Context) Forbidden(msg string) Response {
	return Response{
		StatusCode: http.StatusForbidden,
		Body:       map[string]string{"error": msg},
		Headers:    map[string]string{"Content-Type": "application/json"},
	}
}

// BadRequest returns a 400 response with a message.
func (c *Context) BadRequest(msg string) Response {
	return Response{
		StatusCode: http.StatusBadRequest,
		Body:       map[string]string{"error": msg},
		Headers:    map[string]string{"Content-Type": "application/json"},
	}
}

// RoleInfo describes a user's role membership, used by SetRoles.
type RoleInfo struct {
	Slug     string
	Manages  bool
}

// SetRoles stores the user's role memberships on the context.
// Called by LoadRoles middleware after querying the role_user table.
func (c *Context) SetRoles(roles []RoleInfo) {
	c.roles = make([]string, len(roles))
	c.isAdmin = false
	for i, r := range roles {
		c.roles[i] = r.Slug
		if r.Manages {
			c.isAdmin = true
		}
	}
}

// Role returns the primary role slug (first role). Returns "" if no roles.
func (c *Context) Role() string {
	if len(c.roles) == 0 {
		return ""
	}
	return c.roles[0]
}

// Roles returns all role slugs for the authenticated user.
func (c *Context) Roles() []string {
	return c.roles
}

// HasRole returns true if the user has the given role.
func (c *Context) HasRole(slug string) bool {
	for _, r := range c.roles {
		if r == slug {
			return true
		}
	}
	return false
}

// HasAnyRole returns true if the user has any of the given roles.
func (c *Context) HasAnyRole(slugs ...string) bool {
	for _, slug := range slugs {
		if c.HasRole(slug) {
			return true
		}
	}
	return false
}

// IsAdmin returns true if the user has any role with Manages() set.
func (c *Context) IsAdmin() bool {
	return c.isAdmin
}

// PeekJSON reads a top-level string field from a JSON request body without
// consuming the body. The body is buffered so subsequent reads (Bind, etc.)
// still work. Returns "" if the field is missing or the body isn't JSON.
func (c *Context) PeekJSON(field string) string {
	if c.bodyBuf == nil {
		body, err := io.ReadAll(c.request.Body)
		if err != nil {
			return ""
		}
		c.bodyBuf = body
		c.request.Body = io.NopCloser(bytes.NewReader(body))
	} else {
		// Reset the body reader for subsequent reads.
		c.request.Body = io.NopCloser(bytes.NewReader(c.bodyBuf))
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(c.bodyBuf, &obj); err != nil {
		return ""
	}

	raw, ok := obj[field]
	if !ok {
		return ""
	}

	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}
