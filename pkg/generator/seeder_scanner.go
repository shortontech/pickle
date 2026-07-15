package generator

import (
	"fmt"
	"go/ast"
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
