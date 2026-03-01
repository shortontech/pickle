package squeeze

import (
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

// HasSegmentWithAuthArg returns true if any segment has an argument containing ctx.Auth().
func (cc CallChain) HasSegmentWithAuthArg(prefix string) bool {
	for _, seg := range cc.Segments {
		if !strings.HasPrefix(seg.Name, prefix) {
			continue
		}
		for _, arg := range seg.Args {
			if exprContainsAuthCall(arg) {
				return true
			}
		}
	}
	return false
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
			return nil, nil
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
	ast.Inspect(body, func(n ast.Node) bool {
		// Look for &models.Post{...}
		unary, ok := n.(*ast.UnaryExpr)
		if !ok || unary.Op != token.AND {
			// Also check direct composite lit (without &)
			cl, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if info, ok := parseCompositeLit(cl, fset); ok {
				lits = append(lits, info)
			}
			return true
		}
		cl, ok := unary.X.(*ast.CompositeLit)
		if !ok {
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

// PayloadIsModelWithoutPublic checks if a ctx.JSON payload is a model variable
// that wasn't accessed via .Public().
func PayloadIsModelWithoutPublic(expr ast.Expr) bool {
	// If it's model.Public() or models.PublicXxx(...), it's fine
	if call, ok := expr.(*ast.CallExpr); ok {
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			name := sel.Sel.Name
			if name == "Public" || strings.HasPrefix(name, "Public") {
				return false
			}
		}
	}

	// If it's a bare identifier (e.g. `user`, `posts`), check if it could be a model
	// We can't know for sure without type info, but this is a heuristic
	// The rules layer will cross-reference with routes that lack auth
	switch expr.(type) {
	case *ast.Ident:
		return true // could be a model variable — the rule will decide
	}

	return false
}
