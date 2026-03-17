package squeeze

import (
	"os"
	"path/filepath"
	"testing"
)

// ---- ParseControllers ----

func TestParseControllers_ParsesMethodsFromFile(t *testing.T) {
	dir := t.TempDir()
	src := `package controllers

import pickle "myapp/app/http"

type UserController struct{}

func (c UserController) Index(ctx *pickle.Context) pickle.Response {
	return ctx.JSON(200, nil)
}

func (c UserController) Show(ctx *pickle.Context) pickle.Response {
	id := ctx.Param("id")
	_ = id
	return ctx.JSON(200, nil)
}
`
	if err := os.WriteFile(filepath.Join(dir, "user_controller.go"), []byte(src), 0644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	methods, err := ParseControllers(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := methods["UserController.Index"]; !ok {
		t.Error("expected UserController.Index in methods")
	}
	if _, ok := methods["UserController.Show"]; !ok {
		t.Error("expected UserController.Show in methods")
	}
}

func TestParseControllers_SkipsGenFiles(t *testing.T) {
	dir := t.TempDir()
	src := `package controllers

type FakeController struct{}

func (c FakeController) Index() {}
`
	if err := os.WriteFile(filepath.Join(dir, "fake_gen.go"), []byte(src), 0644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	methods, err := ParseControllers(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 0 {
		t.Errorf("expected _gen.go to be skipped, got %d methods", len(methods))
	}
}

func TestParseControllers_MissingDir(t *testing.T) {
	_, err := ParseControllers("/nonexistent/controllers")
	if err == nil {
		t.Error("expected error for missing controllers dir")
	}
}

func TestParseControllers_PointerReceiver(t *testing.T) {
	dir := t.TempDir()
	src := `package controllers

type PostController struct{}

func (c *PostController) Store() {}
`
	if err := os.WriteFile(filepath.Join(dir, "post_controller.go"), []byte(src), 0644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	methods, err := ParseControllers(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := methods["PostController.Store"]; !ok {
		t.Error("expected pointer receiver method to be parsed")
	}
}

func TestParseControllers_IgnoresFunctions(t *testing.T) {
	dir := t.TempDir()
	src := `package controllers

func helper() {}
`
	if err := os.WriteFile(filepath.Join(dir, "helpers.go"), []byte(src), 0644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	methods, err := ParseControllers(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 0 {
		t.Errorf("expected 0 methods for top-level function, got %d", len(methods))
	}
}

// ---- ParseProjectFunctions ----

func TestParseProjectFunctions_ParsesTopLevelFunctions(t *testing.T) {
	projectDir := t.TempDir()
	servicesDir := filepath.Join(projectDir, "app", "services")
	if err := os.MkdirAll(servicesDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	src := `package services

import "models"

func GetUserByEmail(email string) (*models.User, error) {
	return models.QueryUser().WhereEmail(email).First()
}
`
	if err := os.WriteFile(filepath.Join(servicesDir, "user_service.go"), []byte(src), 0644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	registry := ParseProjectFunctions(projectDir)
	if _, ok := registry["services.GetUserByEmail"]; !ok {
		t.Error("expected services.GetUserByEmail in registry")
	}
}

func TestParseProjectFunctions_SkipsControllers(t *testing.T) {
	projectDir := t.TempDir()
	ctrlDir := filepath.Join(projectDir, "app", "http", "controllers")
	if err := os.MkdirAll(ctrlDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	src := `package controllers

func helper() {}
`
	if err := os.WriteFile(filepath.Join(ctrlDir, "helpers.go"), []byte(src), 0644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	registry := ParseProjectFunctions(projectDir)
	if _, ok := registry["controllers.helper"]; ok {
		t.Error("controllers directory should be skipped")
	}
}

func TestParseProjectFunctions_SkipsGenFiles(t *testing.T) {
	projectDir := t.TempDir()
	servicesDir := filepath.Join(projectDir, "app", "services")
	if err := os.MkdirAll(servicesDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	src := `package services

func GeneratedHelper() {}
`
	if err := os.WriteFile(filepath.Join(servicesDir, "gen_helper_gen.go"), []byte(src), 0644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	registry := ParseProjectFunctions(projectDir)
	if _, ok := registry["services.GeneratedHelper"]; ok {
		t.Error("_gen.go files should be skipped")
	}
}

func TestParseProjectFunctions_SkipsMethods(t *testing.T) {
	projectDir := t.TempDir()
	servicesDir := filepath.Join(projectDir, "app", "services")
	if err := os.MkdirAll(servicesDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	src := `package services

type UserService struct{}

func (s UserService) Get() {}
func TopLevel() {}
`
	if err := os.WriteFile(filepath.Join(servicesDir, "user.go"), []byte(src), 0644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	registry := ParseProjectFunctions(projectDir)
	if _, ok := registry["services.TopLevel"]; !ok {
		t.Error("expected TopLevel in registry")
	}
	if _, ok := registry["services.Get"]; ok {
		t.Error("method Get should NOT be in registry")
	}
}

func TestParseProjectFunctions_NoAppDir(t *testing.T) {
	// If app/ doesn't exist, should return empty registry without panicking
	registry := ParseProjectFunctions(t.TempDir())
	if len(registry) != 0 {
		t.Errorf("expected empty registry, got %d entries", len(registry))
	}
}

// ---- LoadConfig ----

func TestLoadConfig_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	yaml := `squeeze:
  middleware:
    auth:
      - Auth
    admin:
      - RequireAdmin
    rate_limit:
      - RateLimit
  rules:
    no_printf: false
`
	if err := os.WriteFile(filepath.Join(dir, "pickle.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("writing yaml: %v", err)
	}

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Squeeze.RuleEnabled("no_printf") {
		t.Error("no_printf should be disabled")
	}
	if !cfg.Squeeze.Middleware.IsAuthMiddleware("Auth") {
		t.Error("Auth should be auth middleware")
	}
	if !cfg.Squeeze.Middleware.IsAdminMiddleware("RequireAdmin") {
		t.Error("RequireAdmin should be admin middleware")
	}
	if !cfg.Squeeze.Middleware.IsRateLimitMiddleware("RateLimit") {
		t.Error("RateLimit should be rate limit middleware")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pickle.yaml"), []byte("{{invalid yaml{{"), 0644); err != nil {
		t.Fatalf("writing yaml: %v", err)
	}

	_, err := LoadConfig(dir)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}
