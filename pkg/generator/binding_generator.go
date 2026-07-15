package generator

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

// RequestDef describes a request struct parsed from the requests/ directory.
type RequestDef struct {
	Name   string         // e.g. CreateUserRequest
	Fields []RequestField // struct fields in order
	File   string         // source file path (for diagnostics)
}

// RequestField describes a single field in a request struct.
type RequestField struct {
	Name         string // Go field name
	Type         string // Go type as source string
	JSONTag      string // json struct tag value (e.g. "name")
	Validate     string // validate struct tag value (e.g. "required,min=1,max=255")
	IsResourceID bool   // ResourceID or *ResourceID, including qualified forms
	ImportAlias  string // qualifier for a qualified ResourceID
	ImportPath   string // import path providing the qualified ResourceID
}

// ScanRequests parses all Go files in a directory and extracts request struct definitions.
func ScanRequests(dir string) ([]RequestDef, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading requests dir: %w", err)
	}

	var requests []RequestDef

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}

		path := filepath.Join(dir, e.Name())
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		imports := make(map[string]string)
		var httpImportPath string
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			alias := pathpkg.Base(importPath)
			if imp.Name != nil {
				alias = imp.Name.Name
			}
			imports[alias] = importPath
			if strings.HasSuffix(importPath, "/app/http") || strings.HasSuffix(importPath, "/http") {
				httpImportPath = importPath
			}
		}

		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}

			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}

				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}

				if !strings.HasSuffix(ts.Name.Name, "Request") {
					continue
				}

				req := RequestDef{Name: ts.Name.Name, File: path}

				for _, field := range st.Fields.List {
					if len(field.Names) == 0 {
						continue // embedded field
					}

					rf := RequestField{
						Name: field.Names[0].Name,
						Type: exprToTypeString(field.Type),
					}

					if field.Tag != nil {
						rf.JSONTag = extractTag(field.Tag.Value, "json")
						rf.Validate = extractTag(field.Tag.Value, "validate")
					}
					rf.IsResourceID = isResourceIDType(rf.Type)
					if rf.IsResourceID {
						baseType := strings.TrimPrefix(rf.Type, "*")
						if dot := strings.IndexByte(baseType, '.'); dot > 0 {
							rf.ImportAlias = baseType[:dot]
							rf.ImportPath = imports[rf.ImportAlias]
							if rf.ImportPath == "" && rf.ImportAlias == "pickle" {
								rf.ImportPath = httpImportPath
							}
						}
					}

					req.Fields = append(req.Fields, rf)
				}

				requests = append(requests, req)
			}
		}
	}

	sort.Slice(requests, func(i, j int) bool {
		return requests[i].Name < requests[j].Name
	})

	return requests, nil
}

func isResourceIDType(typeName string) bool {
	typeName = strings.TrimPrefix(typeName, "*")
	return typeName == "ResourceID" || strings.HasSuffix(typeName, ".ResourceID")
}

// extractTag pulls a named tag value from a raw struct tag literal (including backticks).
func extractTag(rawTag, name string) string {
	// Strip backticks
	tag := strings.Trim(rawTag, "`")

	// Find name:"value"
	key := name + `:"`
	idx := strings.Index(tag, key)
	if idx < 0 {
		return ""
	}
	val := tag[idx+len(key):]
	end := strings.Index(val, `"`)
	if end < 0 {
		return ""
	}
	val = val[:end]

	// For json tags, strip options like ",omitempty"
	if name == "json" {
		if comma := strings.Index(val, ","); comma >= 0 {
			val = val[:comma]
		}
	}

	return val
}

// exprToTypeString renders an AST type expression to a Go source string.
func exprToTypeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprToTypeString(t.X)
	case *ast.SelectorExpr:
		if pkg, ok := t.X.(*ast.Ident); ok {
			return pkg.Name + "." + t.Sel.Name
		}
	case *ast.ArrayType:
		return "[]" + exprToTypeString(t.Elt)
	case *ast.MapType:
		return "map[" + exprToTypeString(t.Key) + "]" + exprToTypeString(t.Value)
	}
	return "interface{}"
}

var bindingTemplate = template.Must(template.New("bindings").Funcs(template.FuncMap{
	"bt": func() string { return "`" },
	"jsonName": func(field RequestField) string {
		if field.JSONTag != "" {
			return field.JSONTag
		}
		return field.Name
	},
}).Parse(bindingTemplateSource))

const bindingTemplateSource = `// Code generated by Pickle. DO NOT EDIT.
package {{ .Package }}

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
{{ range .ResourceIDImports }}
	{{ .Alias }} "{{ .Path }}"
{{- end }}
)

var validate = validator.New()

func init() {
	_ = validate.RegisterValidation("resource_id", func(fl validator.FieldLevel) bool {
		value := fl.Field()
		if value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return true
			}
			value = value.Elem()
		}
		checker, ok := value.Interface().(interface{ IsZero() bool })
		return ok && !checker.IsZero()
	})
}

// ValidationError represents a single field validation failure.
type ValidationError struct {
	Field   string {{ bt }}json:"field"{{ bt }}
	Message string {{ bt }}json:"message"{{ bt }}
}

// BindingError represents a request binding or validation failure.
type BindingError struct {
	Status int               {{ bt }}json:"-"{{ bt }}
	Errors []ValidationError {{ bt }}json:"errors"{{ bt }}
}

func (e *BindingError) Error() string {
	msgs := make([]string, len(e.Errors))
	for i, ve := range e.Errors {
		msgs[i] = ve.Field + ": " + ve.Message
	}
	return strings.Join(msgs, "; ")
}

func formatValidationErrors(err error) *BindingError {
	ve, ok := err.(validator.ValidationErrors)
	if !ok {
		return &BindingError{
			Status: 422,
			Errors: []ValidationError{{ "{{" }}Field: "_body", Message: err.Error()}},
		}
	}

	errors := make([]ValidationError, len(ve))
	for i, fe := range ve {
		errors[i] = ValidationError{
			Field:   toSnakeCase(fe.Field()),
			Message: formatFieldError(fe),
		}
	}

	return &BindingError{Status: 422, Errors: errors}
}

func formatFieldError(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email address"
	case "min":
		return fmt.Sprintf("must be at least %s", fe.Param())
	case "max":
		return fmt.Sprintf("must be at most %s", fe.Param())
	case "oneof":
		return fmt.Sprintf("must be one of: %s", fe.Param())
	case "uuid":
		return "must be a valid UUID"
	case "resource_id":
		return "must be a valid Resource ID"
	case "required_if":
		return fmt.Sprintf("is required when %s", fe.Param())
	default:
		return fmt.Sprintf("failed %s validation", fe.Tag())
	}
}

func toSnakeCase(s string) string {
	var result []byte
	for i, c := range s {
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				result = append(result, '_')
			}
			result = append(result, byte(c+32))
		} else {
			result = append(result, byte(c))
		}
	}
	return string(result)
}
{{ range .Requests }}
// Bind{{ .Name }} deserializes and validates a {{ .Name }} from the HTTP request body.
func Bind{{ .Name }}(r *http.Request) ({{ .Name }}, *BindingError) {
	var req {{ .Name }}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return req, &BindingError{Status: 400, Errors: []ValidationError{{ "{{" }}Field: "_body", Message: "invalid request body"}}}
	}
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(body, &rawFields); err != nil {
		return req, &BindingError{
			Status: 400,
			Errors: []ValidationError{{ "{{" }}Field: "_body", Message: "invalid request body"}},
		}
	}
	{{ range .Fields }}{{ if .IsResourceID }}{{ if ne .JSONTag "-" }}
	if raw, ok := rawFields[{{ printf "%q" (jsonName .) }}]; ok {
		var value {{ .Type }}
		if err := json.Unmarshal(raw, &value); err != nil {
			return req, &BindingError{Status: 422, Errors: []ValidationError{{ "{{" }}Field: {{ printf "%q" (jsonName .) }}, Message: "must be a valid Resource ID"}}}
		}
	}
	{{ end }}{{ end }}{{ end }}
	if err := json.Unmarshal(body, &req); err != nil {
		return req, &BindingError{Status: 400, Errors: []ValidationError{{ "{{" }}Field: "_body", Message: "invalid request body"}}}
	}
	if err := validate.Struct(req); err != nil {
		return req, formatValidationErrors(err)
	}
	return req, nil
}
{{ end -}}
`

type bindingTemplateData struct {
	Package           string
	Requests          []RequestDef
	ResourceIDImports []requestImport
}

type requestImport struct {
	Alias string
	Path  string
}

// GenerateBindings produces a Go source file with Bind functions for each request struct.
func GenerateBindings(requests []RequestDef, packageName string) ([]byte, error) {
	importPaths := map[string]string{}
	for _, request := range requests {
		for _, field := range request.Fields {
			if field.IsResourceID && field.ImportAlias != "" && field.ImportPath != "" {
				if existing := importPaths[field.ImportAlias]; existing != "" && existing != field.ImportPath {
					return nil, fmt.Errorf("ResourceID import alias %q resolves to both %q and %q", field.ImportAlias, existing, field.ImportPath)
				}
				importPaths[field.ImportAlias] = field.ImportPath
			}
		}
	}
	aliases := make([]string, 0, len(importPaths))
	for alias := range importPaths {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	resourceIDImports := make([]requestImport, 0, len(aliases))
	for _, alias := range aliases {
		resourceIDImports = append(resourceIDImports, requestImport{Alias: alias, Path: importPaths[alias]})
	}
	data := bindingTemplateData{
		Package:           packageName,
		Requests:          requests,
		ResourceIDImports: resourceIDImports,
	}

	var buf bytes.Buffer
	if err := bindingTemplate.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("template execution: %w", err)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("go format: %w\n---raw output---\n%s", err, buf.String())
	}

	return formatted, nil
}
