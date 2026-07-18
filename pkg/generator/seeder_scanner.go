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
	"reflect"
	"sort"
	"strconv"
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
	Fields     []SeederReturnField
}

type SeederReturnField struct {
	Name       string
	GoType     string
	Underlying string
	Nullable   bool
}

type seederTypeInfo struct {
	Underlying string
	Fields     []SeederReturnField
	Struct     bool
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
	types, err := scanSeederTypes(dir, entries)
	if err != nil {
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
		tables := map[string]string{}
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "Policy" || fn.Recv == nil || fn.Body == nil || len(fn.Body.List) != 1 {
				if ok && fn.Name.Name == "Table" && fn.Recv != nil && fn.Body != nil && len(fn.Body.List) == 1 {
					name := receiverTypeNameForSeeder(fn.Recv)
					if statement, ok := fn.Body.List[0].(*ast.ReturnStmt); ok && len(statement.Results) == 1 {
						if literal, ok := statement.Results[0].(*ast.BasicLit); ok && literal.Kind == token.STRING {
							tables[name], _ = strconv.Unquote(literal.Value)
						}
					}
				}
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
				definition.ReturnType = exprString(fn.Type.Results.List[0].Type)
				info, declared := types[strings.TrimPrefix(definition.ReturnType, "*")]
				if declared && info.Struct {
					definition.Kind = "row"
					definition.Fields = append([]SeederReturnField(nil), info.Fields...)
					base := strings.TrimSuffix(name, "Seeder")
					definition.Table = names.Pluralize(names.PascalToSnake(base))
					if tables[name] != "" {
						definition.Table = tables[name]
					}
				} else {
					definition.Kind = "value"
				}
			} else {
				return nil, fmt.Errorf("seeder %s Seed method must return zero or one value", name)
			}
			definitions = append(definitions, definition)
		}
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
	return definitions, nil
}

func scanSeederTypes(dir string, entries []os.DirEntry) (map[string]seederTypeInfo, error) {
	types := map[string]seederTypeInfo{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") || strings.HasSuffix(entry.Name(), "_gen.go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, spec := range general.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				info := seederTypeInfo{Underlying: exprString(typeSpec.Type)}
				if structure, ok := typeSpec.Type.(*ast.StructType); ok {
					info.Struct = true
					for _, field := range structure.Fields.List {
						if len(field.Names) == 0 {
							continue
						}
						for _, fieldName := range field.Names {
							if !fieldName.IsExported() {
								continue
							}
							name := names.PascalToSnake(fieldName.Name)
							if field.Tag != nil {
								tagText, _ := strconv.Unquote(field.Tag.Value)
								tag := reflect.StructTag(tagText)
								for _, key := range []string{"seed", "db", "json"} {
									if tagged := strings.Split(tag.Get(key), ",")[0]; tagged != "" {
										name = tagged
										break
									}
								}
							}
							goType := exprString(field.Type)
							info.Fields = append(info.Fields, SeederReturnField{Name: name, GoType: goType, Nullable: strings.HasPrefix(goType, "*")})
						}
					}
				}
				types[typeSpec.Name.Name] = info
			}
		}
	}
	for name, info := range types {
		for index := range info.Fields {
			info.Fields[index].Underlying = resolveSeederUnderlying(info.Fields[index].GoType, types, map[string]bool{})
		}
		types[name] = info
	}
	return types, nil
}

func resolveSeederUnderlying(goType string, types map[string]seederTypeInfo, seen map[string]bool) string {
	prefix := ""
	base := goType
	if strings.HasPrefix(base, "*") {
		prefix, base = "*", strings.TrimPrefix(base, "*")
	}
	if seen[base] {
		return goType
	}
	if info, ok := types[base]; ok && !info.Struct && info.Underlying != "" && info.Underlying != base {
		seen[base] = true
		return prefix + resolveSeederUnderlying(info.Underlying, types, seen)
	}
	return goType
}

func scanSeederGraphCalls(fset *token.FileSet, body *ast.BlockStmt) []SeederGraphCall {
	if body == nil {
		return nil
	}
	allowed := map[string]bool{"Create": true, "CreateN": true, "For": true, "ForEach": true, "Between": true, "DependsOn": true, "UniqueBy": true, "Update": true, "With": true, "WithFactory": true}
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
		argumentCount := len(call.Args)
		if selector.Sel.Name == "With" && argumentCount > 1 {
			argumentCount = 1
		}
		arguments := make([]string, argumentCount)
		for index, argument := range call.Args[:argumentCount] {
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
		if value.Len == nil {
			return "[]" + exprString(value.Elt)
		}
		return "[" + exprString(value.Len) + "]" + exprString(value.Elt)
	case *ast.MapType:
		return "map[" + exprString(value.Key) + "]" + exprString(value.Value)
	case *ast.InterfaceType:
		if value.Methods == nil || len(value.Methods.List) == 0 {
			return "any"
		}
		return "interface"
	case *ast.IndexExpr:
		return exprString(value.X) + "[" + exprString(value.Index) + "]"
	case *ast.IndexListExpr:
		parts := make([]string, len(value.Indices))
		for index, item := range value.Indices {
			parts[index] = exprString(item)
		}
		return exprString(value.X) + "[" + strings.Join(parts, ",") + "]"
	case *ast.BasicLit:
		return value.Value
	case *ast.ParenExpr:
		return exprString(value.X)
	}
	return ""
}
