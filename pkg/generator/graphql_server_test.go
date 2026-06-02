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
