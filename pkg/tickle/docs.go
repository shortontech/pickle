package tickle

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// TypeDoc holds extracted documentation for an exported type.
type TypeDoc struct {
	Name    string      `json:"name"`
	Doc     string      `json:"doc,omitempty"`
	Methods []MethodDoc `json:"methods,omitempty"`
}

// MethodDoc holds extracted documentation for a method.
type MethodDoc struct {
	Name      string `json:"name"`
	Signature string `json:"signature"`
	Doc       string `json:"doc,omitempty"`
}

// ExtractDocs parses all Go files in srcDir and extracts doc comments
// for exported types and their methods.
func ExtractDocs(srcDir string) ([]TypeDoc, error) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", srcDir, err)
	}

	fset := token.NewFileSet()
	typeDocs := make(map[string]*TypeDoc)
	var typeOrder []string

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		path := filepath.Join(srcDir, entry.Name())
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", entry.Name(), err)
		}

		// Extract type declarations
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !ts.Name.IsExported() {
					continue
				}
				doc := ""
				if gen.Doc != nil {
					doc = strings.TrimSpace(gen.Doc.Text())
				}
				if _, exists := typeDocs[ts.Name.Name]; !exists {
					typeDocs[ts.Name.Name] = &TypeDoc{
						Name: ts.Name.Name,
						Doc:  doc,
					}
					typeOrder = append(typeOrder, ts.Name.Name)
				}
			}
		}

		// Extract methods
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() {
				continue
			}

			var receiverType string
			if fn.Recv != nil && len(fn.Recv.List) > 0 {
				receiverType = resolveReceiverType(fn.Recv.List[0].Type)
			}

			doc := ""
			if fn.Doc != nil {
				doc = strings.TrimSpace(fn.Doc.Text())
			}

			sig := formatSignature(fn)

			if receiverType != "" {
				td, exists := typeDocs[receiverType]
				if !exists {
					td = &TypeDoc{Name: receiverType}
					typeDocs[receiverType] = td
					typeOrder = append(typeOrder, receiverType)
				}
				td.Methods = append(td.Methods, MethodDoc{
					Name:      fn.Name.Name,
					Signature: sig,
					Doc:       doc,
				})
			} else {
				// Top-level exported function — add as a method of a pseudo-type "Functions"
				td, exists := typeDocs["Functions"]
				if !exists {
					td = &TypeDoc{Name: "Functions", Doc: "Top-level exported functions."}
					typeDocs["Functions"] = td
					typeOrder = append(typeOrder, "Functions")
				}
				td.Methods = append(td.Methods, MethodDoc{
					Name:      fn.Name.Name,
					Signature: sig,
					Doc:       doc,
				})
			}
		}
	}

	var result []TypeDoc
	for _, name := range typeOrder {
		result = append(result, *typeDocs[name])
	}
	return result, nil
}

func resolveReceiverType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return resolveReceiverType(t.X)
	case *ast.IndexExpr:
		return resolveReceiverType(t.X)
	}
	return ""
}

func formatSignature(fn *ast.FuncDecl) string {
	var b strings.Builder
	b.WriteString("func ")

	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		b.WriteString("(")
		b.WriteString(formatFieldType(fn.Recv.List[0].Type))
		b.WriteString(") ")
	}

	b.WriteString(fn.Name.Name)
	b.WriteString("(")

	if fn.Type.Params != nil {
		var params []string
		for _, p := range fn.Type.Params.List {
			typeName := formatFieldType(p.Type)
			if len(p.Names) > 0 {
				for _, n := range p.Names {
					params = append(params, n.Name+" "+typeName)
				}
			} else {
				params = append(params, typeName)
			}
		}
		b.WriteString(strings.Join(params, ", "))
	}
	b.WriteString(")")

	if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 {
		var results []string
		for _, r := range fn.Type.Results.List {
			results = append(results, formatFieldType(r.Type))
		}
		if len(results) == 1 {
			b.WriteString(" " + results[0])
		} else {
			b.WriteString(" (" + strings.Join(results, ", ") + ")")
		}
	}

	return b.String()
}

func formatFieldType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + formatFieldType(t.X)
	case *ast.SelectorExpr:
		return formatFieldType(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		return "[]" + formatFieldType(t.Elt)
	case *ast.MapType:
		return "map[" + formatFieldType(t.Key) + "]" + formatFieldType(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		return "func(...)"
	case *ast.IndexExpr:
		return formatFieldType(t.X) + "[" + formatFieldType(t.Index) + "]"
	case *ast.Ellipsis:
		return "..." + formatFieldType(t.Elt)
	default:
		return "any"
	}
}
