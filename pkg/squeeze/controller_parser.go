package squeeze

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// ControllerMethod represents a parsed controller method with its AST body.
type ControllerMethod struct {
	ControllerType string
	MethodName     string
	File           string
	Line           int
	Body           *ast.BlockStmt
	Fset           *token.FileSet
}

// CallChain represents an unwound method call chain like models.QueryPost().WhereID(id).First()
type CallChain struct {
	Segments []ChainSegment
	Line     int
	AuthVars map[string]bool // auth-tainted variable names in scope for this chain
}

// ChainSegment is one link in a call chain.
type ChainSegment struct {
	Name string
	Args []ast.Expr
}

// Names returns just the segment names (e.g. ["models", "QueryPost", "WhereID", "First"]).
func (cc CallChain) Names() []string {
	names := make([]string, len(cc.Segments))
	for i, s := range cc.Segments {
		names[i] = s.Name
	}
	return names
}

// HasSegment returns true if any segment in the chain has the given name.
func (cc CallChain) HasSegment(name string) bool {
	for _, seg := range cc.Segments {
		if seg.Name == name {
			return true
		}
	}
	return false
}

// HasSegmentWithAuthArg returns true if any segment has an argument containing ctx.Auth()
// either directly or via a local variable that was assigned from a ctx.Auth() expression.
func (cc CallChain) HasSegmentWithAuthArg(prefix string) bool {
	return cc.HasSegmentWithAuthArgTainted(prefix, nil)
}

// HasSegmentWithAuthArgTainted is like HasSegmentWithAuthArg but also accepts a set of
// local variable names known to carry auth-derived values (e.g. authID from uuid.Parse(ctx.Auth().UserID)).
func (cc CallChain) HasSegmentWithAuthArgTainted(prefix string, authVars map[string]bool) bool {
	// Merge caller-provided authVars with chain-specific authVars
	merged := authVars
	if len(cc.AuthVars) > 0 {
		merged = make(map[string]bool)
		for v := range authVars {
			merged[v] = true
		}
		for v := range cc.AuthVars {
			merged[v] = true
		}
	}

	for _, seg := range cc.Segments {
		if !strings.HasPrefix(seg.Name, prefix) {
			continue
		}
		for _, arg := range seg.Args {
			if exprContainsAuthCall(arg) {
				return true
			}
			if len(merged) > 0 && exprIsAuthVar(arg, merged) {
				return true
			}
		}
	}
	return false
}

// FindAuthTaintedVars walks a method body and finds local variables assigned from expressions
// that contain ctx.Auth(). For example:
//
//	authID, err := uuid.Parse(ctx.Auth().UserID)
//
// returns {"authID": true}.
func FindAuthTaintedVars(body *ast.BlockStmt) map[string]bool {
	vars := make(map[string]bool)
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		// Check if any RHS expression contains ctx.Auth()
		hasAuth := false
		for _, rhs := range assign.Rhs {
			if exprContainsAuthCall(rhs) {
				hasAuth = true
				break
			}
		}
		if !hasAuth {
			return true
		}
		// Taint the first LHS identifier (the value, not the error)
		for _, lhs := range assign.Lhs {
			if ident, ok := lhs.(*ast.Ident); ok && ident.Name != "err" && ident.Name != "_" {
				vars[ident.Name] = true
			}
		}
		return true
	})
	return vars
}

// exprIsAuthVar checks if an expression is (or contains) one of the auth-tainted variable names.
func exprIsAuthVar(expr ast.Expr, authVars map[string]bool) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if found {
			return false
		}
		if ident, ok := n.(*ast.Ident); ok && authVars[ident.Name] {
			found = true
		}
		return true
	})
	return found
}

// CompositeLitInfo describes a model struct literal found in a controller method.
type CompositeLitInfo struct {
	PackageName string   // e.g. "models"
	TypeName    string   // e.g. "Post"
	FieldNames  []string // fields set in the literal
	Line        int
}

// ParseControllers parses all Go files in the controllers directory.
func ParseControllers(controllersDir string) (map[string]*ControllerMethod, error) {
	entries, err := os.ReadDir(controllersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("controllers directory not found: %s", controllersDir)
		}
		return nil, err
	}

	methods := make(map[string]*ControllerMethod)

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") || strings.HasSuffix(e.Name(), "_gen.go") {
			continue
		}

		path := filepath.Join(controllersDir, e.Name())
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, err
		}

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Body == nil {
				continue
			}

			ctrlType := receiverTypeName(fn.Recv)
			if ctrlType == "" {
				continue
			}

			key := ctrlType + "." + fn.Name.Name
			methods[key] = &ControllerMethod{
				ControllerType: ctrlType,
				MethodName:     fn.Name.Name,
				File:           path,
				Line:           fset.Position(fn.Pos()).Line,
				Body:           fn.Body,
				Fset:           fset,
			}
		}
	}

	return methods, nil
}

// receiverTypeName extracts the type name from a method receiver.
func receiverTypeName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	switch t := recv.List[0].Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if ident, ok := t.X.(*ast.Ident); ok {
			return ident.Name
		}
	}
	return ""
}

// ExtractCallChains walks a method body and extracts all call chains.
func ExtractCallChains(body *ast.BlockStmt, fset *token.FileSet) []CallChain {
	var chains []CallChain
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		segments := unwindCall(call)
		if len(segments) > 1 {
			chains = append(chains, CallChain{
				Segments: segments,
				Line:     fset.Position(call.Pos()).Line,
			})
		}
		return true
	})
	return chains
}

// unwindCall recursively unwinds a call chain into segments.
func unwindCall(expr ast.Expr) []ChainSegment {
	switch e := expr.(type) {
	case *ast.CallExpr:
		sel, ok := e.Fun.(*ast.SelectorExpr)
		if !ok {
			// Simple call like fn()
			if ident, ok := e.Fun.(*ast.Ident); ok {
				return []ChainSegment{{Name: ident.Name, Args: e.Args}}
			}
			return nil
		}
		parent := unwindCall(sel.X)
		return append(parent, ChainSegment{Name: sel.Sel.Name, Args: e.Args})
	case *ast.SelectorExpr:
		parent := unwindCall(e.X)
		return append(parent, ChainSegment{Name: e.Sel.Name})
	case *ast.Ident:
		return []ChainSegment{{Name: e.Name}}
	}
	return nil
}

// FindCallsTo walks a method body and finds calls to a specific package.function.
// e.g. FindCallsTo(body, "fmt", "Sprintf") finds fmt.Sprintf(...) calls.
func FindCallsTo(body *ast.BlockStmt, fset *token.FileSet, pkg, funcName string) []int {
	var lines []int
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if ident.Name == pkg && sel.Sel.Name == funcName {
			lines = append(lines, fset.Position(call.Pos()).Line)
		}
		return true
	})
	return lines
}

// FindBuiltinCalls finds calls to a Go builtin function (e.g. recover, panic) in a block.
func FindBuiltinCalls(body *ast.BlockStmt, fset *token.FileSet, name string) []int {
	var lines []int
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		if ident.Name == name {
			lines = append(lines, fset.Position(call.Pos()).Line)
		}
		return true
	})
	return lines
}

// FindMustParseCalls finds uuid.MustParse calls and categorizes them by argument type.
type MustParseCall struct {
	Line       int
	HasCtxParam bool // argument contains ctx.Param(...)
	HasCtxAuth  bool // argument contains ctx.Auth()
}

func FindMustParseCalls(body *ast.BlockStmt, fset *token.FileSet) []MustParseCall {
	var calls []MustParseCall
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if ident.Name != "uuid" || sel.Sel.Name != "MustParse" {
			return true
		}

		mpc := MustParseCall{
			Line: fset.Position(call.Pos()).Line,
		}
		for _, arg := range call.Args {
			if exprContainsParamCall(arg) {
				mpc.HasCtxParam = true
			}
			if exprContainsAuthCall(arg) {
				mpc.HasCtxAuth = true
			}
		}
		calls = append(calls, mpc)
		return true
	})
	return calls
}

// FindCompositeLiterals finds &models.Type{...} composite literals in a method body.
func FindCompositeLiterals(body *ast.BlockStmt, fset *token.FileSet) []CompositeLitInfo {
	var lits []CompositeLitInfo
	// Track composite lits we've already seen via &expr to avoid double-counting
	seen := make(map[*ast.CompositeLit]bool)
	ast.Inspect(body, func(n ast.Node) bool {
		// Look for &models.Post{...}
		unary, ok := n.(*ast.UnaryExpr)
		if ok && unary.Op == token.AND {
			cl, ok := unary.X.(*ast.CompositeLit)
			if ok {
				seen[cl] = true
				if info, ok := parseCompositeLit(cl, fset); ok {
					lits = append(lits, info)
				}
			}
			return true
		}
		// Also check direct composite lit (without &)
		cl, ok := n.(*ast.CompositeLit)
		if !ok || seen[cl] {
			return true
		}
		if info, ok := parseCompositeLit(cl, fset); ok {
			lits = append(lits, info)
		}
		return true
	})
	return lits
}

func parseCompositeLit(cl *ast.CompositeLit, fset *token.FileSet) (CompositeLitInfo, bool) {
	sel, ok := cl.Type.(*ast.SelectorExpr)
	if !ok {
		return CompositeLitInfo{}, false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return CompositeLitInfo{}, false
	}

	info := CompositeLitInfo{
		PackageName: pkg.Name,
		TypeName:    sel.Sel.Name,
		Line:        fset.Position(cl.Pos()).Line,
	}

	for _, elt := range cl.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if ident, ok := kv.Key.(*ast.Ident); ok {
			info.FieldNames = append(info.FieldNames, ident.Name)
		}
	}

	return info, true
}

// FindCtxJSONCalls finds ctx.JSON(status, payload) calls and returns info about the payload.
type CtxJSONCall struct {
	Line       int
	PayloadExpr ast.Expr
}

func FindCtxJSONCalls(body *ast.BlockStmt, fset *token.FileSet) []CtxJSONCall {
	var calls []CtxJSONCall
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "JSON" {
			return true
		}
		// Check it's ctx.JSON
		ident, ok := sel.X.(*ast.Ident)
		if !ok || ident.Name != "ctx" {
			return true
		}
		if len(call.Args) < 2 {
			return true
		}
		calls = append(calls, CtxJSONCall{
			Line:        fset.Position(call.Pos()).Line,
			PayloadExpr: call.Args[1],
		})
		return true
	})
	return calls
}

// exprContainsParamCall checks if an expression contains ctx.Param(...).
func exprContainsParamCall(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if ident.Name == "ctx" && sel.Sel.Name == "Param" {
			found = true
		}
		return true
	})
	return found
}

// bodyContainsAuthCall checks if a method body contains any ctx.Auth() call.
func bodyContainsAuthCall(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if expr, ok := n.(ast.Expr); ok && exprContainsAuthCall(expr) {
			found = true
			return false
		}
		return true
	})
	return found
}

// exprContainsAuthCall checks if an expression contains ctx.Auth().
func exprContainsAuthCall(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if ident.Name == "ctx" && sel.Sel.Name == "Auth" {
			found = true
		}
		return true
	})
	return found
}

// FindParamNames returns all string literal arguments to ctx.Param() and ctx.ParamUUID() calls in a method body.
func FindParamNames(body *ast.BlockStmt, fset *token.FileSet) []ParamCall {
	var calls []ParamCall
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if ident.Name != "ctx" || (sel.Sel.Name != "Param" && sel.Sel.Name != "ParamUUID") {
			return true
		}
		if len(call.Args) != 1 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		// Strip quotes
		name := strings.Trim(lit.Value, "\"")
		pos := fset.Position(call.Pos())
		calls = append(calls, ParamCall{Name: name, Line: pos.Line})
		return true
	})
	return calls
}

// ParamCall represents a ctx.Param("name") call found in a controller.
type ParamCall struct {
	Name string
	Line int
}

// RouteParams extracts parameter names from a route path (e.g., "/users/:id" -> ["id"]).
func RouteParams(path string) []string {
	var params []string
	for _, seg := range strings.Split(path, "/") {
		if strings.HasPrefix(seg, ":") {
			params = append(params, seg[1:])
		}
	}
	return params
}

// ParsedFunc represents a parsed project-local function for inlining.
type ParsedFunc struct {
	Body   *ast.BlockStmt
	Fset   *token.FileSet
	Params []*ast.Field
}

// FuncRegistry maps "pkg.FuncName" to parsed function info.
type FuncRegistry map[string]*ParsedFunc

// ParseProjectFunctions walks the app/ directory and indexes all top-level functions
// (excluding _gen.go and _test.go files) by "packageName.FuncName".
func ParseProjectFunctions(projectDir string) FuncRegistry {
	registry := make(FuncRegistry)
	appDir := filepath.Join(projectDir, "app")

	_ = filepath.Walk(appDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_gen.go") ||
			strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// Skip controllers — they're already parsed separately
		if strings.Contains(path, filepath.Join("http", "controllers")) {
			return nil
		}

		fset := token.NewFileSet()
		f, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return nil
		}

		pkgName := f.Name.Name
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil {
				continue
			}
			key := pkgName + "." + fn.Name.Name
			registry[key] = &ParsedFunc{
				Body:   fn.Body,
				Fset:   fset,
				Params: fn.Type.Params.List,
			}
		}
		return nil
	})

	return registry
}

// ExtractCallChainsRecursive extracts call chains from a method body, recursively
// inlining project-local function calls to discover chains inside service functions.
func ExtractCallChainsRecursive(body *ast.BlockStmt, fset *token.FileSet, registry FuncRegistry, authVars map[string]bool) []CallChain {
	chains := ExtractCallChains(body, fset)

	if len(registry) == 0 {
		return chains
	}

	// Find project-local calls and inline them
	visited := make(map[string]bool)
	var inlineFrom func(body *ast.BlockStmt, fset *token.FileSet, authVars map[string]bool)
	inlineFrom = func(body *ast.BlockStmt, fset *token.FileSet, authVars map[string]bool) {
		ast.Inspect(body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			// Look for pkg.Func() calls
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkgIdent, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}

			key := pkgIdent.Name + "." + sel.Sel.Name
			fn, ok := registry[key]
			if !ok || visited[key] {
				return true
			}
			visited[key] = true

			// Build auth vars scoped to the inner function — only from:
			// 1. Argument-to-parameter mapping (auth taint flows through call args)
			// 2. Auth-tainted assignments inside the function body itself
			// NOT from outer variable names (prevents name collision false positives)
			innerAuthVars := make(map[string]bool)

			// Map call args to function params for auth taint
			argIdx := 0
			for _, field := range fn.Params {
				for _, paramName := range field.Names {
					if argIdx < len(call.Args) {
						if exprIsAuthVar(call.Args[argIdx], authVars) || exprContainsAuthCall(call.Args[argIdx]) {
							innerAuthVars[paramName.Name] = true
						}
					}
					argIdx++
				}
			}

			// Also find auth tainted vars inside the function body
			for v := range FindAuthTaintedVars(fn.Body) {
				innerAuthVars[v] = true
			}

			// Extract chains from the inlined function and tag with scoped auth vars
			innerChains := ExtractCallChains(fn.Body, fn.Fset)
			for i := range innerChains {
				innerChains[i].AuthVars = innerAuthVars
				chains = append(chains, innerChains[i])
			}

			// Recurse into the function body (with scoped auth vars, not outer)
			inlineFrom(fn.Body, fn.Fset, innerAuthVars)

			visited[key] = false // allow re-expansion from different call sites
			return true
		})
	}

	inlineFrom(body, fset, authVars)
	return chains
}

// FindCompositeLiteralsRecursive finds composite literals in a method body and
// recursively in any project-local functions called from it.
func FindCompositeLiteralsRecursive(body *ast.BlockStmt, fset *token.FileSet, registry FuncRegistry) []CompositeLitInfo {
	lits := FindCompositeLiterals(body, fset)

	if len(registry) == 0 {
		return lits
	}

	visited := make(map[string]bool)
	var findInBody func(body *ast.BlockStmt, fset *token.FileSet)
	findInBody = func(body *ast.BlockStmt, fset *token.FileSet) {
		ast.Inspect(body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkgIdent, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			key := pkgIdent.Name + "." + sel.Sel.Name
			fn, ok := registry[key]
			if !ok || visited[key] {
				return true
			}
			visited[key] = true
			lits = append(lits, FindCompositeLiterals(fn.Body, fn.Fset)...)
			findInBody(fn.Body, fn.Fset)
			visited[key] = false
			return true
		})
	}
	findInBody(body, fset)

	return lits
}

// PayloadIsModelWithoutPublic checks if a ctx.JSON payload is a model variable
// that wasn't accessed via .Public(). Uses modelVars to identify which local
// variables hold model-typed values.
func PayloadIsModelWithoutPublic(expr ast.Expr, modelVars map[string]bool) bool {
	// If it's model.Public() or models.PublicXxx(...), it's fine
	if call, ok := expr.(*ast.CallExpr); ok {
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			name := sel.Sel.Name
			if name == "Public" || strings.HasPrefix(name, "Public") {
				return false
			}
		}
	}

	// If it's a bare identifier, only flag it if it's a known model variable
	if ident, ok := expr.(*ast.Ident); ok {
		return modelVars[ident.Name]
	}

	return false
}

// FindModelVars walks a method body and finds local variables assigned from model sources:
//   - models.QueryX().First() or .All() (query results)
//   - &models.X{} (struct literals)
func FindModelVars(body *ast.BlockStmt) map[string]bool {
	vars := make(map[string]bool)
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) == 0 || len(assign.Rhs) == 0 {
			return true
		}

		rhs := assign.Rhs[0]

		// Check for &models.X{...}
		if unary, ok := rhs.(*ast.UnaryExpr); ok {
			if cl, ok := unary.X.(*ast.CompositeLit); ok {
				if isModelsType(cl.Type) {
					addFirstIdent(assign.Lhs, vars)
					return true
				}
			}
		}

		// Check for models.X{...} (without &)
		if cl, ok := rhs.(*ast.CompositeLit); ok {
			if isModelsType(cl.Type) {
				addFirstIdent(assign.Lhs, vars)
				return true
			}
		}

		// Check for call chains ending in First()/All() on a models.Query*() chain
		if exprIsModelQuery(rhs) {
			addFirstIdent(assign.Lhs, vars)
		}

		return true
	})
	return vars
}

// isModelsType checks if a type expression is models.Something.
func isModelsType(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == "models"
}

// exprIsModelQuery checks if an expression is a call chain containing models.Query*().
func exprIsModelQuery(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// Check for models.QueryX()
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "models" && strings.HasPrefix(sel.Sel.Name, "Query") {
			found = true
		}
		return true
	})
	return found
}

// addFirstIdent adds the first non-error, non-blank identifier from an LHS list.
func addFirstIdent(lhs []ast.Expr, vars map[string]bool) {
	for _, l := range lhs {
		if ident, ok := l.(*ast.Ident); ok && ident.Name != "err" && ident.Name != "_" {
			vars[ident.Name] = true
			return
		}
	}
}
