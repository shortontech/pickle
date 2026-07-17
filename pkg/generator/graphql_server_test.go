package generator

import (
	"strings"
	"testing"
)

func TestHandlerGeneratorIncludesQueryBudgeting(t *testing.T) {
	src, err := GenerateGraphQLHandler("graphql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	if !strings.Contains(s, "enforceQueryBudget") {
		t.Error("handler should enforce query budget")
	}
	if !strings.Contains(s, "defaultQueryBudget") {
		t.Error("handler should use the default query budget")
	}
	if !strings.Contains(s, "ctx.queryStats = stats") {
		t.Error("handler should store query stats before execution")
	}
}

func TestGraphQLPolicyBoundaryUsesSealedAuthentication(t *testing.T) {
	src,err:=GenerateGraphQLPolicyBoundary("graphql","example.com/app");if err!=nil{t.Fatal(err)};text:=string(src)
	for _,want:=range []string{"auth.AuthenticatePolicySource", "models.PolicyContextFromVerified", "models.PublicPolicyContext", `example.com/app/app/http/auth`}{if !strings.Contains(text,want){t.Fatalf("missing %q in %s",want,text)}}
}

func TestHandlerGeneratorIncludesIntrospectionControl(t *testing.T) {
	src, err := GenerateGraphQLHandler("graphql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	if !strings.Contains(s, "allowIntrospection") {
		t.Error("handler should check allowIntrospection")
	}
	if !strings.Contains(s, "isIntrospectionField") {
		t.Error("handler should use isIntrospectionField")
	}
}

func TestCoreGraphQLDisablesIntrospectionByDefault(t *testing.T) {
	src := GenerateCoreGraphQL("graphql")
	if !strings.Contains(string(src), "var allowIntrospection = false") {
		t.Error("introspection should be disabled by default")
	}
}

func TestHandlerGeneratorIncludesValidation(t *testing.T) {
	src, err := GenerateGraphQLHandler("graphql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	if !strings.Contains(s, "parseDocument") {
		t.Error("handler should parse the document")
	}
	if !strings.Contains(s, "execute(") {
		t.Error("handler should execute the query")
	}
}
