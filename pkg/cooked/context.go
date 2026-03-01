package cooked

import (
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
	return c.params[name]
}

// SetParam sets a URL path parameter. Used by the generated route handler.
func (c *Context) SetParam(name, value string) {
	c.params[name] = value
}

// ParamUUID returns a URL path parameter parsed as a UUID.
func (c *Context) ParamUUID(name string) uuid.UUID {
	return uuid.MustParse(c.params[name])
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
func (c *Context) SetAuth(claims any) {
	switch v := claims.(type) {
	case *AuthInfo:
		c.auth = v
	default:
		c.auth = &AuthInfo{Claims: v}
	}
}

// Auth returns the authenticated user info, or nil if not authenticated.
func (c *Context) Auth() *AuthInfo {
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
		return c.NotFound("not found")
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

// Error returns a 500 Internal Server Error response.
func (c *Context) Error(err error) Response {
	return Response{
		StatusCode: http.StatusInternalServerError,
		Body:       map[string]string{"error": err.Error()},
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
