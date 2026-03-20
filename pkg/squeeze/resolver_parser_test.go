package squeeze

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseResolversSkipsGenFiles(t *testing.T) {
	dir := t.TempDir()

	// Write a _gen.go file (should be skipped)
	os.WriteFile(filepath.Join(dir, "resolver_gen.go"), []byte(`package graphql

type RootResolver struct{}

func (r *RootResolver) resolveQuery() {}
`), 0o644)

	// Write a user resolver (should be parsed)
	os.WriteFile(filepath.Join(dir, "resolver.go"), []byte(`package graphql

type RootResolver struct{}

func (r *RootResolver) CustomQuery() {}
`), 0o644)

	methods, err := ParseControllers(dir)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := methods["RootResolver.CustomQuery"]; !ok {
		t.Error("expected to find RootResolver.CustomQuery from user file")
	}
	if _, ok := methods["RootResolver.resolveQuery"]; ok {
		t.Error("should not have parsed _gen.go method")
	}
}

func TestResolverMethodsGetNoPrintfCoverage(t *testing.T) {
	src := `package graphql
import "fmt"
func (r *RootResolver) CustomMutation() {
	fmt.Printf("debug: %v", something)
}`
	body, fset, _ := parseFunc(t, src)

	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{
			"RootResolver.CustomMutation": {
				ControllerType: "RootResolver",
				MethodName:     "CustomMutation",
				File:           "graphql/resolver.go",
				Line:           1,
				Body:           body,
				Fset:           fset,
			},
		},
	}

	findings := ruleNoPrintf(ctx)
	if len(findings) == 0 {
		t.Error("no_printf should flag fmt.Printf in custom resolver")
	}
}

func TestResolverMethodsGetNoRecoverCoverage(t *testing.T) {
	src := `package graphql
func (r *RootResolver) CustomQuery() {
	defer func() {
		recover()
	}()
}`
	body, fset, _ := parseFunc(t, src)

	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{
			"RootResolver.CustomQuery": {
				ControllerType: "RootResolver",
				MethodName:     "CustomQuery",
				File:           "graphql/resolver.go",
				Line:           1,
				Body:           body,
				Fset:           fset,
			},
		},
	}

	findings := ruleNoRecover(ctx)
	if len(findings) == 0 {
		t.Error("no_recover should flag recover() in custom resolver")
	}
}
