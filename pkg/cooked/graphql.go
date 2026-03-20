package cooked

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

// AuthClaims holds authentication state for a GraphQL request.
type AuthClaims struct {
	UserID string
	Role   string
}

// ResolveContext carries auth, variables, and dataloaders for a single GraphQL request.
type ResolveContext struct {
	auth      *AuthClaims
	variables map[string]any
	loaders   any // *DataLoaderRegistry — defined in dataloader_gen.go
}

// IsAuthenticated returns true if the request has valid auth.
func (c *ResolveContext) IsAuthenticated() bool {
	return c.auth != nil
}

// UserID returns the authenticated user's ID, or empty string.
func (c *ResolveContext) UserID() string {
	if c.auth == nil {
		return ""
	}
	return c.auth.UserID
}

// HasRole returns true if the authenticated user has the given role.
func (c *ResolveContext) HasRole(role string) bool {
	if c.auth == nil {
		return false
	}
	return c.auth.Role == role
}

// CanSeeOwnerFields returns true if the caller owns the resource or is admin.
func (c *ResolveContext) CanSeeOwnerFields(ownerID string) bool {
	if c.auth == nil {
		return false
	}
	return c.auth.UserID == ownerID || c.auth.Role == "admin"
}

// Visibility returns the visibility tier for the current request.
func (c *ResolveContext) Visibility() VisibilityTier {
	if c.auth == nil {
		return VisibilityPublic
	}
	if c.auth.Role == "admin" {
		return VisibilityAll
	}
	return VisibilityOwner
}

// VisibilityTier represents the access level of a request.
type VisibilityTier int

const (
	// VisibilityPublic is for unauthenticated access.
	VisibilityPublic VisibilityTier = iota
	// VisibilityOwner is for authenticated users viewing their own data.
	VisibilityOwner
	// VisibilityAll is for admin access.
	VisibilityAll
)

// Document represents a parsed GraphQL request.
type Document struct {
	Operation string         // "query" | "mutation"
	Name      string         // operation name, may be empty
	Fields    []Field        // top-level field selections
	Variables map[string]any // variable values from the request
}

// Field represents a selected field with arguments and sub-selections.
type Field struct {
	Name       string
	Alias      string         // empty if no alias
	Args       map[string]any
	Selections []Field // nested selections
}

// ValidationError holds field-level validation errors for GraphQL responses.
type ValidationError struct {
	Fields []FieldError `json:"fields"`
}

// FieldError is a single field validation error.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	if len(e.Fields) == 0 {
		return "validation failed"
	}
	return fmt.Sprintf("validation failed: %s: %s", e.Fields[0].Field, e.Fields[0].Message)
}

// PageArgs holds parsed pagination arguments.
type PageArgs struct {
	First  int
	After  string
	Last   int
	Before string
	Offset int
}

// parseDocument parses a GraphQL query string using gqlparser and converts
// the resulting AST into Pickle's Document type.
func parseDocument(schema *ast.Schema, src string) (*Document, error) {
	queryDoc, gqlErr := gqlparser.LoadQuery(schema, src)
	if gqlErr != nil {
		return nil, gqlErr
	}
	if len(queryDoc.Operations) == 0 {
		return nil, fmt.Errorf("no operations found in query")
	}
	op := queryDoc.Operations[0]
	opType := strings.ToLower(string(op.Operation))
	if opType == "subscription" {
		return nil, fmt.Errorf("subscriptions are not supported")
	}
	doc := &Document{
		Operation: opType,
		Name:      op.Name,
		Fields:    convertSelectionSet(op.SelectionSet),
	}
	return doc, nil
}

func convertSelectionSet(ss ast.SelectionSet) []Field {
	fields := make([]Field, 0, len(ss))
	for _, sel := range ss {
		switch s := sel.(type) {
		case *ast.Field:
			f := Field{
				Name:       s.Name,
				Alias:      s.Alias,
				Args:       convertArguments(s.Arguments),
				Selections: convertSelectionSet(s.SelectionSet),
			}
			fields = append(fields, f)
		case *ast.InlineFragment:
			fields = append(fields, convertSelectionSet(s.SelectionSet)...)
		case *ast.FragmentSpread:
			// fragments are pre-merged by gqlparser's validator
		}
	}
	return fields
}

func convertArguments(args ast.ArgumentList) map[string]any {
	if len(args) == 0 {
		return nil
	}
	m := make(map[string]any, len(args))
	for _, a := range args {
		m[a.Name] = valueToGo(a.Value)
	}
	return m
}

func valueToGo(v *ast.Value) any {
	if v == nil {
		return nil
	}
	switch v.Kind {
	case ast.IntValue, ast.FloatValue, ast.StringValue, ast.EnumValue, ast.BooleanValue:
		return v.Raw
	case ast.ListValue:
		list := make([]any, len(v.Children))
		for i, child := range v.Children {
			list[i] = valueToGo(child.Value)
		}
		return list
	case ast.ObjectValue:
		obj := make(map[string]any, len(v.Children))
		for _, child := range v.Children {
			obj[child.Name] = valueToGo(child.Value)
		}
		return obj
	case ast.NullValue:
		return nil
	case ast.Variable:
		// Variables are resolved by gqlparser during validation
		return v.Raw
	default:
		return v.Raw
	}
}

// execute runs a parsed document against the root resolver.
func execute(ctx *ResolveContext, root rootResolver, doc *Document) (map[string]any, []map[string]any) {
	data := make(map[string]any, len(doc.Fields))
	var errors []map[string]any

	for _, field := range doc.Fields {
		alias := field.Alias
		if alias == "" {
			alias = field.Name
		}

		var val any
		var err error

		switch doc.Operation {
		case "query":
			val, err = root.resolveQuery(ctx, field)
		case "mutation":
			val, err = root.resolveMutation(ctx, field)
		default:
			err = fmt.Errorf("unsupported operation: %s", doc.Operation)
		}

		if err != nil {
			errors = append(errors, toGraphQLError(err, []string{alias}))
			data[alias] = nil
		} else {
			data[alias] = val
		}
	}

	return data, errors
}

// extractPage parses pagination arguments from a GraphQL field's args.
func extractPage(args map[string]any) PageArgs {
	p := PageArgs{First: 25} // default page size
	if args == nil {
		return p
	}
	pageArg, ok := args["page"]
	if !ok {
		return p
	}
	page, ok := pageArg.(map[string]any)
	if !ok {
		return p
	}
	if v, ok := page["first"].(string); ok {
		if n := parseInt(v); n > 0 {
			p.First = n
		}
	}
	if v, ok := page["after"].(string); ok {
		p.After = v
	}
	if v, ok := page["last"].(string); ok {
		if n := parseInt(v); n > 0 {
			p.Last = n
		}
	}
	if v, ok := page["before"].(string); ok {
		p.Before = v
	}
	return p
}

func parseInt(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		} else {
			return 0
		}
	}
	return n
}

// encodeCursor encodes an offset as a cursor string.
func encodeCursor(offset int) string {
	return fmt.Sprintf("cursor:%d", offset)
}

// decodeCursor decodes a cursor string to an offset.
func decodeCursor(cursor string) int {
	if !strings.HasPrefix(cursor, "cursor:") {
		return 0
	}
	return parseInt(cursor[7:])
}

// selectionsFor finds nested selections by traversing a path of field names.
func selectionsFor(selections []Field, path ...string) []Field {
	current := selections
	for _, name := range path {
		for _, f := range current {
			if f.Name == name {
				current = f.Selections
				break
			}
		}
	}
	return current
}

// writeError writes a GraphQL error response.
func writeError(w http.ResponseWriter, message, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // GraphQL errors use 200
	json.NewEncoder(w).Encode(map[string]any{
		"data": nil,
		"errors": []map[string]any{
			{
				"message":    message,
				"extensions": map[string]any{"code": code},
			},
		},
	})
}

// extractAuth extracts AuthClaims from the Authorization header.
// This is a placeholder — user projects override with their own auth extraction.
func extractAuth(r *http.Request) *AuthClaims {
	header := r.Header.Get("Authorization")
	if header == "" {
		return nil
	}
	// Bearer token extraction is handled by user middleware.
	// This is a stub that returns nil — the generated handler
	// is meant to be wrapped with auth middleware that sets claims.
	return nil
}

// --- Batch Loader ---

type batchResult[V any] struct {
	val V
	err error
}

type batchLoader[K comparable, V any] struct {
	mu      sync.Mutex
	pending []K
	waiters []chan batchResult[V]
	fn      func(keys []K) []batchResult[V]
	timer   *time.Timer
}

func newBatchLoader[K comparable, V any](fn func([]K) []batchResult[V]) *batchLoader[K, V] {
	return &batchLoader[K, V]{fn: fn}
}

func (l *batchLoader[K, V]) load(key K) (V, error) {
	ch := make(chan batchResult[V], 1)
	l.mu.Lock()
	l.pending = append(l.pending, key)
	l.waiters = append(l.waiters, ch)
	if l.timer == nil {
		l.timer = time.AfterFunc(0, l.dispatch)
	}
	l.mu.Unlock()
	r := <-ch
	return r.val, r.err
}

func (l *batchLoader[K, V]) dispatch() {
	l.mu.Lock()
	keys := l.pending
	waiters := l.waiters
	l.pending = nil
	l.waiters = nil
	l.timer = nil
	l.mu.Unlock()
	results := l.fn(keys)
	for i, w := range waiters {
		if i < len(results) {
			w <- results[i]
		} else {
			var zero V
			w <- batchResult[V]{val: zero, err: fmt.Errorf("batch result missing for key at index %d", i)}
		}
	}
}

// rootResolver is the interface that the generated RootResolver must implement.
type rootResolver interface {
	resolveQuery(ctx *ResolveContext, field Field) (any, error)
	resolveMutation(ctx *ResolveContext, field Field) (any, error)
}

// GraphQLError is an error with a GraphQL error code for structured error responses.
type GraphQLError struct {
	Message    string
	Code       string
	Field      string // optional: the field path that caused the error
	Extensions map[string]any
}

func (e *GraphQLError) Error() string {
	return e.Message
}

// Error code constants following the GraphQL community conventions.
const (
	CodeBadUserInput            = "BAD_USER_INPUT"
	CodeUnauthenticated         = "UNAUTHENTICATED"
	CodeForbidden               = "FORBIDDEN"
	CodeNotFound                = "NOT_FOUND"
	CodeInternalServerError     = "INTERNAL_SERVER_ERROR"
	CodeGraphQLParseFailed      = "GRAPHQL_PARSE_FAILED"
	CodeGraphQLValidationFailed = "GRAPHQL_VALIDATION_FAILED"
)

// Unauthenticated returns a GraphQL error for missing or invalid authentication.
func Unauthenticated(msg string) *GraphQLError {
	return &GraphQLError{Message: msg, Code: CodeUnauthenticated}
}

// Forbidden returns a GraphQL error for insufficient permissions.
func Forbidden(msg string) *GraphQLError {
	return &GraphQLError{Message: msg, Code: CodeForbidden}
}

// NotFound returns a GraphQL error for a missing resource.
func NotFound(resource string) *GraphQLError {
	return &GraphQLError{
		Message: fmt.Sprintf("%s not found", resource),
		Code:    CodeNotFound,
	}
}

// BadInput returns a GraphQL error for invalid user input.
func BadInput(msg string) *GraphQLError {
	return &GraphQLError{Message: msg, Code: CodeBadUserInput}
}

// InternalError returns a GraphQL error for unexpected server errors.
func InternalError(msg string) *GraphQLError {
	return &GraphQLError{Message: msg, Code: CodeInternalServerError}
}

// toGraphQLError converts any error to a structured GraphQL error map.
// If the error is already a *GraphQLError, its code is preserved.
// Otherwise it's treated as an internal error.
func toGraphQLError(err error, path []string) map[string]any {
	gqlErr := map[string]any{
		"message": err.Error(),
		"path":    path,
	}

	if ge, ok := err.(*GraphQLError); ok {
		ext := map[string]any{"code": ge.Code}
		if ge.Field != "" {
			ext["field"] = ge.Field
		}
		for k, v := range ge.Extensions {
			ext[k] = v
		}
		gqlErr["extensions"] = ext
	} else if ve, ok := err.(*ValidationError); ok {
		gqlErr["extensions"] = map[string]any{
			"code":   CodeBadUserInput,
			"fields": ve.Fields,
		}
	} else {
		gqlErr["extensions"] = map[string]any{
			"code": CodeInternalServerError,
		}
	}

	return gqlErr
}

