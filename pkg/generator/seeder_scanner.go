package generator

import (
	"bytes"
	"fmt"
	"go/ast"
	goformat "go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shortontech/pickle/pkg/names"
)

type SeederDefinition struct {
	Name       string
	Kind       string // scenario or row
	Table      string
	ReturnType string
	File       string
	Policy     string
	GraphCalls []SeederGraphCall
}

// SeederGraphCall is safe static scenario metadata. Value-bearing With calls
// are deliberately excluded so read-only tooling cannot expose seed secrets.
type SeederGraphCall struct {
	Method    string
	Arguments []string
	Line      int
}

// ScanSeeders discovers exported Seed methods in database/seeders.
func ScanSeeders(dir string) ([]SeederDefinition, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var definitions []SeederDefinition
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") || strings.HasSuffix(entry.Name(), "_gen.go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		policies := map[string]string{}
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "Policy" || fn.Recv == nil || fn.Body == nil || len(fn.Body.List) != 1 {
				continue
			}
			name := receiverTypeNameForSeeder(fn.Recv)
			if statement, ok := fn.Body.List[0].(*ast.ReturnStmt); ok && len(statement.Results) == 1 {
				policies[name] = seederExprText(fset, statement.Results[0])
			}
		}
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "Seed" || fn.Recv == nil {
				continue
			}
			name := receiverTypeNameForSeeder(fn.Recv)
			if name == "" || !ast.IsExported(name) {
				continue
			}
			definition := SeederDefinition{Name: name, File: path}
			if fn.Type.Results == nil || len(fn.Type.Results.List) == 0 {
				definition.Kind = "scenario"
				definition.Policy = policies[name]
				definition.GraphCalls = scanSeederGraphCalls(fset, fn.Body)
			} else if len(fn.Type.Results.List) == 1 {
				definition.Kind = "row"
				definition.ReturnType = exprString(fn.Type.Results.List[0].Type)
				base := strings.TrimSuffix(name, "Seeder")
				definition.Table = names.Pluralize(names.PascalToSnake(base))
			} else {
				return nil, fmt.Errorf("seeder %s Seed method must return zero or one value", name)
			}
			definitions = append(definitions, definition)
		}
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
	return definitions, nil
}

func scanSeederGraphCalls(fset *token.FileSet, body *ast.BlockStmt) []SeederGraphCall {
	if body == nil {
		return nil
	}
	allowed := map[string]bool{"Create": true, "CreateN": true, "For": true, "ForEach": true, "Between": true, "UniqueBy": true, "Update": true}
	var calls []SeederGraphCall
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !allowed[selector.Sel.Name] {
			return true
		}
		arguments := make([]string, len(call.Args))
		for index, argument := range call.Args {
			arguments[index] = seederExprText(fset, argument)
		}
		calls = append(calls, SeederGraphCall{Method: selector.Sel.Name, Arguments: arguments, Line: fset.Position(call.Pos()).Line})
		return true
	})
	sort.SliceStable(calls, func(i, j int) bool {
		if calls[i].Line != calls[j].Line {
			return calls[i].Line < calls[j].Line
		}
		return calls[i].Method < calls[j].Method
	})
	return calls
}

func seederExprText(fset *token.FileSet, expression ast.Expr) string {
	var out bytes.Buffer
	if err := goformat.Node(&out, fset, expression); err != nil {
		return "?"
	}
	return out.String()
}

func receiverTypeNameForSeeder(fields *ast.FieldList) string {
	if fields == nil || len(fields.List) == 0 {
		return ""
	}
	switch value := fields.List[0].Type.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		if ident, ok := value.X.(*ast.Ident); ok {
			return ident.Name
		}
	}
	return ""
}

func exprString(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		return exprString(value.X) + "." + value.Sel.Name
	case *ast.StarExpr:
		return "*" + exprString(value.X)
	case *ast.ArrayType:
		return "[]" + exprString(value.Elt)
	}
	return ""
}
