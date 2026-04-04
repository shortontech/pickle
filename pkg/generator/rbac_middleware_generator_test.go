package generator

import (
	"strings"
	"testing"
)

func TestGenerateLoadRolesMiddleware(t *testing.T) {
	src, err := GenerateLoadRolesMiddleware("github.com/example/myapp/app/http")
	if err != nil {
		t.Fatalf("GenerateLoadRolesMiddleware: %v", err)
	}

	code := string(src)

	if !strings.Contains(code, "package middleware") {
		t.Error("expected package middleware")
	}
	if !strings.Contains(code, `pickle "github.com/example/myapp/app/http"`) {
		t.Error("expected pickle import")
	}
	if !strings.Contains(code, "pickle.RoleLoaderFunc = loadRolesFromDB") {
		t.Error("expected init wiring of RoleLoaderFunc")
	}
	if !strings.Contains(code, "role_user") {
		t.Error("expected role_user query")
	}
	if !strings.Contains(code, "pickle.RoleInfo") {
		t.Error("expected RoleInfo type usage")
	}
	if !strings.Contains(code, "DO NOT EDIT") {
		t.Error("expected generated file header")
	}
}
