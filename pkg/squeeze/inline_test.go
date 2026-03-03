package squeeze

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// parseFunc parses a Go source string and returns the body and fset of the
// first function declaration found.
func parseFunc(t *testing.T, src string) (*ast.BlockStmt, *token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Body != nil {
			return fn.Body, fset, f
		}
	}
	t.Fatal("no function found")
	return nil, nil, nil
}

// buildRegistry parses a Go source string and registers all top-level functions
// under the given package name.
func buildRegistry(t *testing.T, pkgName, src string) FuncRegistry {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, pkgName+".go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	reg := make(FuncRegistry)
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil {
			continue
		}
		key := pkgName + "." + fn.Name.Name
		reg[key] = &ParsedFunc{
			Body:   fn.Body,
			Fset:   fset,
			Params: fn.Type.Params.List,
		}
	}
	return reg
}

func chainHasSegment(chains []CallChain, name string) bool {
	for _, c := range chains {
		if c.HasSegment(name) {
			return true
		}
	}
	return false
}

func TestExtractCallChainsRecursive_InlinesServiceCalls(t *testing.T) {
	// Controller calls services.DoQuery() which contains models.QueryPost().WhereID(id).First()
	controllerSrc := `package controllers
func Store() {
	services.DoQuery()
}`
	serviceSrc := `package services
import "models"
func DoQuery() {
	models.QueryPost().WhereID(id).First()
}`

	body, fset, _ := parseFunc(t, controllerSrc)
	registry := buildRegistry(t, "services", serviceSrc)
	authVars := make(map[string]bool)

	chains := ExtractCallChainsRecursive(body, fset, registry, authVars)

	// Should find the query chain from the service
	if !chainHasSegment(chains, "QueryPost") {
		t.Error("expected inlined chain to contain QueryPost")
	}
	if !chainHasSegment(chains, "WhereID") {
		t.Error("expected inlined chain to contain WhereID")
	}
}

func TestExtractCallChainsRecursive_AuthTaintPropagation(t *testing.T) {
	// Controller passes authID (tainted) to services.FindPost(authID)
	// Service uses the parameter in WhereUserID(userID)
	controllerSrc := `package controllers
func Show() {
	services.FindPost(authID)
}`
	serviceSrc := `package services
import "models"
func FindPost(userID string) {
	models.QueryPost().WhereUserID(userID).First()
}`

	body, fset, _ := parseFunc(t, controllerSrc)
	registry := buildRegistry(t, "services", serviceSrc)
	authVars := map[string]bool{"authID": true}

	chains := ExtractCallChainsRecursive(body, fset, registry, authVars)

	// Find the inlined chain and check that its AuthVars contains userID
	found := false
	for _, chain := range chains {
		if chain.HasSegment("WhereUserID") {
			if chain.AuthVars["userID"] {
				found = true
			}
			break
		}
	}
	if !found {
		t.Error("expected auth taint to propagate: authID -> userID parameter")
	}
}

func TestExtractCallChainsRecursive_NoNameCollision(t *testing.T) {
	// Controller has authID tainted, but passes `postID` (not tainted) to
	// a service whose parameter is also named `authID`.
	// The service's authID should NOT be tainted.
	controllerSrc := `package controllers
func Destroy() {
	services.FindByID(postID)
}`
	serviceSrc := `package services
import "models"
func FindByID(authID string) {
	models.QueryPost().WhereUserID(authID).First()
}`

	body, fset, _ := parseFunc(t, controllerSrc)
	registry := buildRegistry(t, "services", serviceSrc)
	authVars := map[string]bool{"authID": true} // controller scope has authID tainted

	chains := ExtractCallChainsRecursive(body, fset, registry, authVars)

	// The service's authID param received postID (not tainted), so
	// the inner chain should NOT have authID in its AuthVars.
	for _, chain := range chains {
		if chain.HasSegment("WhereUserID") {
			if chain.AuthVars != nil && chain.AuthVars["authID"] {
				t.Error("name collision: service param 'authID' should NOT be tainted when it received non-tainted 'postID'")
			}
			// Also verify via HasSegmentWithAuthArgTainted — passing empty outer authVars
			// to isolate the check to chain-level AuthVars only
			if chain.HasSegmentWithAuthArgTainted("Where", nil) {
				t.Error("HasSegmentWithAuthArgTainted should return false for non-tainted chain")
			}
			break
		}
	}
}

func TestExtractCallChainsRecursive_CyclePrevention(t *testing.T) {
	// Two functions that call each other — should not infinite loop
	src := `package services
import "models"
func A() {
	services.B()
	models.QueryPost().First()
}
func B() {
	services.A()
	models.QueryUser().First()
}`

	body, fset, _ := parseFunc(t, `package controllers
func Index() {
	services.A()
}`)

	registry := buildRegistry(t, "services", src)
	authVars := make(map[string]bool)

	// Should complete without infinite recursion
	chains := ExtractCallChainsRecursive(body, fset, registry, authVars)

	if !chainHasSegment(chains, "QueryPost") {
		t.Error("expected chain from A containing QueryPost")
	}
	if !chainHasSegment(chains, "QueryUser") {
		t.Error("expected chain from B containing QueryUser")
	}
}

func TestExtractCallChainsRecursive_EmptyRegistry(t *testing.T) {
	// With no registry, should behave like plain ExtractCallChains
	src := `package controllers
import "models"
func Index() {
	models.QueryPost().WhereID(id).First()
}`
	body, fset, _ := parseFunc(t, src)

	chains := ExtractCallChainsRecursive(body, fset, nil, nil)

	if !chainHasSegment(chains, "QueryPost") {
		t.Error("expected chain with QueryPost even with nil registry")
	}
}

func TestExtractCallChainsRecursive_DeepNesting(t *testing.T) {
	// Controller -> services.A() -> services.B() -> models.QueryPost()
	controllerSrc := `package controllers
func Store() {
	services.A()
}`
	serviceSrc := `package services
import "models"
func A() {
	services.B()
}
func B() {
	models.QueryPost().WhereID(id).Create(p)
}`

	body, fset, _ := parseFunc(t, controllerSrc)
	registry := buildRegistry(t, "services", serviceSrc)
	authVars := make(map[string]bool)

	chains := ExtractCallChainsRecursive(body, fset, registry, authVars)

	if !chainHasSegment(chains, "QueryPost") {
		t.Error("expected deeply nested QueryPost chain to be discovered")
	}
}

func TestFindCompositeLiteralsRecursive_FindsInService(t *testing.T) {
	controllerSrc := `package controllers
func Store() {
	services.CreatePost()
}`
	serviceSrc := `package services
import "models"
func CreatePost() {
	post := &models.Post{
		Title: title,
		Body:  body,
	}
	_ = post
}`

	body, fset, _ := parseFunc(t, controllerSrc)
	registry := buildRegistry(t, "services", serviceSrc)

	lits := FindCompositeLiteralsRecursive(body, fset, registry)

	found := false
	for _, lit := range lits {
		if lit.PackageName == "models" && lit.TypeName == "Post" {
			found = true
			if len(lit.FieldNames) != 2 {
				t.Errorf("expected 2 fields, got %d", len(lit.FieldNames))
			}
		}
	}
	if !found {
		t.Error("expected to find models.Post composite literal from service")
	}
}

func TestFindCompositeLiterals_NoDuplicateForAddressOf(t *testing.T) {
	// &models.Post{} should produce exactly one result, not two
	src := `package controllers
import "models"
func Store() {
	post := &models.Post{
		Title: "hello",
	}
	_ = post
}`

	body, fset, _ := parseFunc(t, src)
	lits := FindCompositeLiterals(body, fset)

	count := 0
	for _, lit := range lits {
		if lit.PackageName == "models" && lit.TypeName == "Post" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 composite literal, got %d (duplicate from &expr)", count)
	}
}

func TestHasSegmentWithAuthArgTainted_ChainAuthVars(t *testing.T) {
	// Chain has its own AuthVars — should be checked even with nil outer authVars
	src := `package test
import "models"
func F() {
	models.QueryPost().WhereUserID(userID).First()
}`
	body, fset, _ := parseFunc(t, src)
	chains := ExtractCallChains(body, fset)

	if len(chains) == 0 {
		t.Fatal("expected at least one chain")
	}

	chain := chains[0]
	chain.AuthVars = map[string]bool{"userID": true}

	if !chain.HasSegmentWithAuthArgTainted("Where", nil) {
		t.Error("expected chain-level AuthVars to be checked")
	}

	// Without chain AuthVars, should not match
	chain.AuthVars = nil
	if chain.HasSegmentWithAuthArgTainted("Where", nil) {
		t.Error("expected no match without AuthVars")
	}
}
