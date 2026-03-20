package generator

import (
	"strings"
	"testing"
)

func TestHandlerGeneratorIncludesDepthLimiting(t *testing.T) {
	src, err := GenerateGraphQLHandler("graphql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	if !strings.Contains(s, "queryDepth") {
		t.Error("handler should include query depth check")
	}
	if !strings.Contains(s, "maxQueryDepth") {
		t.Error("handler should reference maxQueryDepth")
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
