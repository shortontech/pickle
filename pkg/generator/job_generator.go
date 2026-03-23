package generator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

// ScanJobs parses Go files in the jobs directory and returns the names
// of exported types that have a Handle() error method.
func ScanJobs(jobsDir string) ([]string, error) {
	entries, err := os.ReadDir(jobsDir)
	if err != nil {
		return nil, err
	}

	var jobs []string

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		// Skip generated files
		if strings.HasSuffix(e.Name(), "_gen.go") {
			continue
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, jobsDir+"/"+e.Name(), nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", e.Name(), err)
		}

		jobs = append(jobs, findJobTypes(f)...)
	}

	return jobs, nil
}

// findJobTypes returns exported type names that have a Handle() error method.
func findJobTypes(f *ast.File) []string {
	// Collect all exported struct type names
	structNames := map[string]bool{}
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
			if _, ok := ts.Type.(*ast.StructType); ok {
				structNames[ts.Name.Name] = true
			}
		}
	}

	// Check which structs have a Handle method
	hasHandle := map[string]bool{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}

		typeName := receiverTypeName(fn.Recv.List[0].Type)
		if typeName == "" || !structNames[typeName] {
			continue
		}

		if fn.Name.Name == "Handle" {
			hasHandle[typeName] = true
		}
	}

	var result []string
	for name := range hasHandle {
		result = append(result, name)
	}
	return result
}
