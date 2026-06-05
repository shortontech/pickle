package cooked

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	validatorPkg "github.com/go-playground/validator/v10"
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
	auth       *AuthClaims
	variables  map[string]any
	loaders    any // *DataLoaderRegistry — defined in dataloader_gen.go
	queryStats *QueryStats
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
	Alias      string // empty if no alias
	Args       map[string]any
	Selections []Field // nested selections
}

// QueryBudget defines pre-execution GraphQL request limits.
type QueryBudget struct {
	MaxDepth             int
	MaxFields            int
	MaxAliases           int
	MaxInputNodes        int
	MaxComplexity        int
	MaxRelationshipDepth int
}

// QueryStats describes the measured shape of a GraphQL request.
type QueryStats struct {
	Depth             int
	Fields            int
	Aliases           int
	InputNodes        int
	Complexity        int
	RelationshipDepth int
}

// FieldCost describes generated cost metadata for a GraphQL field.
type FieldCost struct {
	TypeName     string
	FieldName    string
	BaseCost     int
	IsList       bool
	IsRelation   bool
	DefaultLimit int
	MaxLimit     int
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

const defaultGraphQLPageSize = 25
const maxGraphQLPageSize = 100
const maxGraphQLInputListSize = 100
const maxQueryDepth = 10
const maxQueryFields = 200
const maxQueryAliases = 25
const maxQueryInputNodes = 500
const maxQueryComplexity = 1000
const maxGraphQLRelationshipDepth = 3

var generatedFieldCosts = map[string]FieldCost{}

func registerGraphQLFieldCosts(costs map[string]FieldCost) {
	for k, v := range costs {
		generatedFieldCosts[k] = v
	}
}

func defaultQueryBudget() QueryBudget {
	return QueryBudget{
		MaxDepth:             maxQueryDepth,
		MaxFields:            maxQueryFields,
		MaxAliases:           maxQueryAliases,
		MaxInputNodes:        maxQueryInputNodes,
		MaxComplexity:        maxQueryComplexity,
		MaxRelationshipDepth: maxGraphQLRelationshipDepth,
	}
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
	if len(queryDoc.Operations) > 1 {
		return nil, fmt.Errorf("multiple operations are not supported")
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

// extractPage parses and validates pagination arguments from a GraphQL field's args.
func extractPage(args map[string]any) (PageArgs, error) {
	p := PageArgs{First: defaultGraphQLPageSize}
	if args == nil {
		return p, nil
	}
	pageArg, ok := args["page"]
	if !ok {
		return p, nil
	}
	page, ok := pageArg.(map[string]any)
	if !ok {
		return p, fmt.Errorf("page must be an object")
	}
	hasFirst := page["first"] != nil
	hasLast := page["last"] != nil
	if hasFirst && hasLast {
		return p, fmt.Errorf("page cannot specify both first and last")
	}
	if v, ok := page["first"].(string); ok {
		n, err := parsePositiveInt(v)
		if err != nil {
			return p, fmt.Errorf("page.first: %w", err)
		}
		if n > maxGraphQLPageSize {
			return p, fmt.Errorf("page.first %d exceeds maximum %d", n, maxGraphQLPageSize)
		}
		p.First = n
	}
	if v, ok := page["after"].(string); ok {
		offset, err := decodeCursor(v)
		if err != nil {
			return p, err
		}
		p.After = v
		p.Offset = offset
	}
	if v, ok := page["last"].(string); ok {
		n, err := parsePositiveInt(v)
		if err != nil {
			return p, fmt.Errorf("page.last: %w", err)
		}
		if n > maxGraphQLPageSize {
			return p, fmt.Errorf("page.last %d exceeds maximum %d", n, maxGraphQLPageSize)
		}
		p.Last = n
		p.First = n
	}
	if v, ok := page["before"].(string); ok {
		if _, err := decodeCursor(v); err != nil {
			return p, err
		}
		p.Before = v
	}
	return p, nil
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

func parsePositiveInt(s string) (int, error) {
	n := parseInt(s)
	if n <= 0 {
		return 0, fmt.Errorf("must be positive")
	}
	return n, nil
}

// encodeCursor encodes an offset as a cursor string.
func encodeCursor(offset int) string {
	return fmt.Sprintf("cursor:%d", offset)
}

// decodeCursor decodes a cursor string to an offset.
func decodeCursor(cursor string) (int, error) {
	if !strings.HasPrefix(cursor, "cursor:") {
		return 0, fmt.Errorf("invalid cursor")
	}
	return parseInt(cursor[7:]), nil
}

func enforceQueryBudget(doc *Document, budget QueryBudget) (*QueryStats, error) {
	stats := measureQueryStats(doc.Fields, 1, 0)
	if stats.Depth > budget.MaxDepth {
		return stats, fmt.Errorf("query depth %d exceeds maximum %d", stats.Depth, budget.MaxDepth)
	}
	if stats.Fields > budget.MaxFields {
		return stats, fmt.Errorf("query field count %d exceeds maximum %d", stats.Fields, budget.MaxFields)
	}
	if stats.Aliases > budget.MaxAliases {
		return stats, fmt.Errorf("query alias count %d exceeds maximum %d", stats.Aliases, budget.MaxAliases)
	}
	if stats.InputNodes > budget.MaxInputNodes {
		return stats, fmt.Errorf("query input node count %d exceeds maximum %d", stats.InputNodes, budget.MaxInputNodes)
	}
	if stats.Complexity > budget.MaxComplexity {
		return stats, fmt.Errorf("query complexity %d exceeds maximum %d", stats.Complexity, budget.MaxComplexity)
	}
	if stats.RelationshipDepth > budget.MaxRelationshipDepth {
		return stats, fmt.Errorf("relationship depth %d exceeds maximum %d", stats.RelationshipDepth, budget.MaxRelationshipDepth)
	}
	return stats, nil
}

func measureQueryStats(fields []Field, depth, relationshipDepth int) *QueryStats {
	stats := &QueryStats{Depth: 0}
	for _, f := range fields {
		cost := graphQLFieldCost(f)
		relDepth := relationshipDepth
		if cost.IsRelation {
			relDepth++
		}
		stats.Fields++
		if f.Alias != "" {
			stats.Aliases++
		}
		stats.InputNodes += countInputNodes(f.Args)
		stats.Complexity += fieldComplexity(f, cost)
		if depth > stats.Depth {
			stats.Depth = depth
		}
		if relDepth > stats.RelationshipDepth {
			stats.RelationshipDepth = relDepth
		}
		child := measureQueryStats(f.Selections, depth+1, relDepth)
		stats.Fields += child.Fields
		stats.Aliases += child.Aliases
		stats.InputNodes += child.InputNodes
		stats.Complexity += child.Complexity
		if child.Depth > stats.Depth {
			stats.Depth = child.Depth
		}
		if child.RelationshipDepth > stats.RelationshipDepth {
			stats.RelationshipDepth = child.RelationshipDepth
		}
	}
	return stats
}

func graphQLFieldCost(field Field) FieldCost {
	for _, cost := range generatedFieldCosts {
		if cost.FieldName == field.Name {
			return cost
		}
	}
	return FieldCost{FieldName: field.Name, BaseCost: 1}
}

func fieldComplexity(field Field, cost FieldCost) int {
	base := cost.BaseCost
	if base <= 0 {
		base = 1
	}
	if cost.IsList {
		limit := defaultGraphQLPageSize
		if pageArg, ok := field.Args["page"].(map[string]any); ok {
			if v, ok := pageArg["first"].(string); ok {
				if n := parseInt(v); n > 0 {
					limit = n
				}
			}
		}
		return base * limit
	}
	return base
}

func countInputNodes(v any) int {
	switch x := v.(type) {
	case nil:
		return 0
	case map[string]any:
		n := len(x)
		for _, child := range x {
			n += countInputNodes(child)
		}
		return n
	case []any:
		n := len(x)
		for _, child := range x {
			n += countInputNodes(child)
		}
		return n
	default:
		return 1
	}
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

// validateInput runs struct validation on a GraphQL input and returns a
// ValidationError if any fields fail. Uses go-playground/validator.
func validateInput(input any) error {
	validate := inputValidator()
	if err := validate.Struct(input); err != nil {
		if _, ok := err.(*validatorPkg.InvalidValidationError); ok {
			return fmt.Errorf("validation setup error: %w", err)
		}
		var fields []FieldError
		for _, fe := range err.(validatorPkg.ValidationErrors) {
			fields = append(fields, FieldError{
				Field:   camelCase(fe.Field()),
				Message: validationMessage(fe),
			})
		}
		return &ValidationError{Fields: fields}
	}
	return nil
}

// camelCase lowercases the first letter of a string.
func camelCase(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// validationMessage returns a human-readable message for a validation error.
func validationMessage(fe validatorPkg.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email address"
	case "min":
		return "must be at least " + fe.Param() + " characters"
	case "max":
		return "must be at most " + fe.Param() + " characters"
	case "oneof":
		return "must be one of: " + fe.Param()
	case "uuid":
		return "must be a valid UUID"
	default:
		return "failed " + fe.Tag() + " validation"
	}
}

// inputValidatorInstance is a lazily initialized validator.
var inputValidatorInstance *validatorPkg.Validate

// inputValidator returns the shared validator instance.
func inputValidator() *validatorPkg.Validate {
	if inputValidatorInstance == nil {
		inputValidatorInstance = validatorPkg.New()
	}
	return inputValidatorInstance
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

// --- Playground ---

// PlaygroundHandler returns an http.Handler that serves a GraphQL playground UI.
// Mount it at /playground in debug mode.
func PlaygroundHandler(endpoint string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
  <title>GraphQL Playground</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/graphql-playground-react/build/static/css/index.css" />
  <script src="https://cdn.jsdelivr.net/npm/graphql-playground-react/build/static/js/middleware.js"></script>
</head>
<body>
  <div id="root"></div>
  <script>
    window.addEventListener('load', function() {
      GraphQLPlayground.init(document.getElementById('root'), { endpoint: '` + endpoint + `' })
    })
  </script>
</body>
</html>`))
	})
}

// queryDepth calculates the depth of a parsed document's field selections.
func queryDepth(fields []Field) int {
	max := 0
	for _, f := range fields {
		d := 1 + queryDepth(f.Selections)
		if d > max {
			max = d
		}
	}
	return max
}

// --- Introspection Control ---

// allowIntrospection controls whether __schema and __type queries are allowed.
// Disabled by default to prevent schema leakage in production. Enable
// explicitly in local tooling with SetIntrospection(true).
var allowIntrospection = false

// SetIntrospection enables or disables GraphQL introspection queries.
func SetIntrospection(allow bool) {
	allowIntrospection = allow
}

// isIntrospectionField returns true if the field is an introspection query.
func isIntrospectionField(name string) bool {
	return name == "__schema" || name == "__type" || name == "__typename"
}
