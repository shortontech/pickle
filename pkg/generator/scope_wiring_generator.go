package generator

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// ScopeDef describes a user-defined scope function parsed from database/scopes/.
type ScopeDef struct {
	Name       string   // exported function name, e.g. "Active"
	ExtraParams []ScopeParam // parameters beyond the first ScopeBuilder param
	SourceFile string   // relative path to the source file
}

// ScopeParam describes an additional parameter on a scope function.
type ScopeParam struct {
	Name string
	Type string
}

// ScanScopes scans the directory database/scopes/{model}/ for exported scope functions.
// Returns scope definitions grouped by model name.
func ScanScopes(scopesDir string) (map[string][]ScopeDef, error) {
	result := map[string][]ScopeDef{}

	entries, err := os.ReadDir(scopesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		modelDir := entry.Name()
		modelPath := filepath.Join(scopesDir, modelDir)

		scopes, err := parseScopeDir(modelPath, modelDir)
		if err != nil {
			return nil, fmt.Errorf("parsing scopes for %s: %w", modelDir, err)
		}
		if len(scopes) > 0 {
			result[modelDir] = scopes
		}
	}

	return result, nil
}

func parseScopeDir(dir, modelDir string) ([]ScopeDef, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		return nil, err
	}

	var scopes []ScopeDef
	for _, pkg := range pkgs {
		for filename, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || !fn.Name.IsExported() {
					continue
				}

				scope, err := parseScopeFunc(fn, filepath.Join("database/scopes", modelDir, filepath.Base(filename)))
				if err != nil {
					return nil, fmt.Errorf("%s: %w", filename, err)
				}
				if scope != nil {
					scopes = append(scopes, *scope)
				}
			}
		}
	}
	return scopes, nil
}

// terminalMethods are methods that must never be called from a scope function.
// These perform data access or mutation and belong only on QueryBuilder.
var terminalMethods = map[string]bool{
	"First":     true,
	"All":       true,
	"Count":     true,
	"Create":    true,
	"Update":    true,
	"Delete":    true,
	"Raw":       true,
	"aggregate": true,
}

func parseScopeFunc(fn *ast.FuncDecl, sourceFile string) (*ScopeDef, error) {
	params := fn.Type.Params
	if params == nil || len(params.List) == 0 {
		return nil, fmt.Errorf("scope %s: must have at least one parameter (ScopeBuilder)", fn.Name.Name)
	}

	// First param should be *XxxScopeBuilder — we just verify it's a pointer type
	firstParam := params.List[0]
	if _, ok := firstParam.Type.(*ast.StarExpr); !ok {
		return nil, fmt.Errorf("scope %s: first parameter must be a pointer to ScopeBuilder", fn.Name.Name)
	}

	// AST validation: check that no terminal methods are called in the function body
	if err := validateNoTerminalCalls(fn); err != nil {
		return nil, fmt.Errorf("scope %s in %s: %w", fn.Name.Name, sourceFile, err)
	}

	scope := &ScopeDef{
		Name:       fn.Name.Name,
		SourceFile: sourceFile,
	}

	// Parse extra params
	for i := 1; i < len(params.List); i++ {
		p := params.List[i]
		typeStr := exprToString(p.Type)
		for _, name := range p.Names {
			scope.ExtraParams = append(scope.ExtraParams, ScopeParam{
				Name: name.Name,
				Type: typeStr,
			})
		}
	}

	return scope, nil
}

// validateNoTerminalCalls walks the AST of a scope function and returns an error
// if any terminal method (First, All, Count, Create, Update, Delete, etc.) is called.
func validateNoTerminalCalls(fn *ast.FuncDecl) error {
	if fn.Body == nil {
		return nil
	}
	var found string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found != "" {
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
		if terminalMethods[sel.Sel.Name] {
			found = sel.Sel.Name
			return false
		}
		return true
	})
	if found != "" {
		return fmt.Errorf("calls terminal method %s() — scope functions may only use filter/sort methods", found)
	}
	return nil
}

func exprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprToString(e.X) + "." + e.Sel.Name
	case *ast.StarExpr:
		return "*" + exprToString(e.X)
	case *ast.ArrayType:
		if e.Len == nil {
			return "[]" + exprToString(e.Elt)
		}
		return fmt.Sprintf("[%s]%s", exprToString(e.Len), exprToString(e.Elt))
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", exprToString(e.Key), exprToString(e.Value))
	case *ast.BasicLit:
		return e.Value
	default:
		return "interface{}"
	}
}

// GenerateScopeWiring produces a Go source file with wrapper methods on XxxQuery
// that delegate to user-defined scope functions.
func GenerateScopeWiring(modelName string, scopes []ScopeDef, packageName, scopeImportPath string) ([]byte, error) {
	if len(scopes) == 0 {
		return nil, nil
	}

	structName := tableToStructName(modelName)
	queryType := structName + "Query"
	scopeBuilderType := structName + "ScopeBuilder"

	var buf bytes.Buffer
	buf.WriteString("// Code generated by Pickle. DO NOT EDIT.\n")
	buf.WriteString(fmt.Sprintf("package %s\n\n", packageName))
	buf.WriteString(fmt.Sprintf("import scopes %q\n\n", scopeImportPath))

	for _, scope := range scopes {
		// Source comment
		buf.WriteString(fmt.Sprintf("// %s — source: %s\n", scope.Name, scope.SourceFile))

		// Build parameter list
		var paramSig, paramCall string
		if len(scope.ExtraParams) > 0 {
			var sigs, calls []string
			for _, p := range scope.ExtraParams {
				sigs = append(sigs, fmt.Sprintf("%s %s", p.Name, p.Type))
				calls = append(calls, p.Name)
			}
			paramSig = strings.Join(sigs, ", ")
			paramCall = ", " + strings.Join(calls, ", ")
		}

		buf.WriteString(fmt.Sprintf("func (q *%s) %s(%s) *%s {\n", queryType, scope.Name, paramSig, queryType))
		buf.WriteString(fmt.Sprintf("\tsb := q.ToScopeBuilder()\n"))

		// Wrap in model-specific scope builder to pass to the scope function
		_ = scopeBuilderType
		buf.WriteString(fmt.Sprintf("\tresult := scopes.%s(sb%s)\n", scope.Name, paramCall))
		buf.WriteString(fmt.Sprintf("\tq.ApplyScope(result)\n"))
		buf.WriteString(fmt.Sprintf("\treturn q\n"))
		buf.WriteString("}\n\n")
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return buf.Bytes(), fmt.Errorf("formatting scope wiring for %s: %w\n%s", modelName, err, buf.String())
	}
	return formatted, nil
}
