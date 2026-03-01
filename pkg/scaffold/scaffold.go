package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shortontech/pickle/pkg/names"
)

// Create scaffolds a new Pickle project in targetDir with the given module name.
func Create(moduleName, targetDir string) error {
	ts := time.Now().Format("2006_01_02_150405")

	files := map[string]string{
		"go.mod":                              tmplGoMod(moduleName),
		".env":                                tmplDotEnv(),
		"cmd/server/main.go":                  tmplMain(moduleName),
		"config/app.go":                       tmplConfigApp(),
		"config/database.go":                  tmplConfigDatabase(),
		"routes/web.go":                       tmplRoutes(moduleName),
		"app/http/controllers/welcome_controller.go": tmplWelcomeController(moduleName),
		"app/http/middleware/auth.go":          tmplAuthMiddleware(moduleName),
		"app/http/requests/login.go":           tmplLoginRequest(),
		"database/migrations/" + ts + "_create_users_table.go": tmplMigration(ts),
	}

	// Create app/commands/ directory so the generator emits commands/pickle_gen.go
	if err := os.MkdirAll(filepath.Join(targetDir, "app", "commands"), 0o755); err != nil {
		return fmt.Errorf("creating app/commands: %w", err)
	}

	for relPath, content := range files {
		absPath := filepath.Join(targetDir, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return fmt.Errorf("creating directory for %s: %w", relPath, err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", relPath, err)
		}
		fmt.Printf("  created %s\n", relPath)
	}

	return nil
}

// MakeController scaffolds a new controller file.
func MakeController(name, projectDir, moduleName string) (string, error) {
	if err := sanitizeName(name); err != nil {
		return "", err
	}
	name = strings.TrimSuffix(name, "Controller")
	structName := names.SnakeToPascal(name) + "Controller"
	snake := names.PascalToSnake(name)
	if strings.Contains(name, "_") {
		snake = strings.ToLower(name)
		structName = names.SnakeToPascal(name) + "Controller"
	}
	fileName := snake + "_controller.go"
	relPath := filepath.Join("app", "http", "controllers", fileName)
	return writeScaffold(projectDir, relPath, tmplMakeController(structName, moduleName))
}

// MakeMiddleware scaffolds a new middleware file.
func MakeMiddleware(name, projectDir, moduleName string) (string, error) {
	if err := sanitizeName(name); err != nil {
		return "", err
	}
	pascal := names.SnakeToPascal(name)
	snake := names.PascalToSnake(pascal)
	fileName := snake + ".go"
	relPath := filepath.Join("app", "http", "middleware", fileName)
	return writeScaffold(projectDir, relPath, tmplMakeMiddleware(pascal, moduleName))
}

// MakeRequest scaffolds a new request file.
func MakeRequest(name, projectDir, moduleName string) (string, error) {
	if err := sanitizeName(name); err != nil {
		return "", err
	}
	name = strings.TrimSuffix(name, "Request")
	pascal := names.SnakeToPascal(name)
	snake := names.PascalToSnake(pascal)
	if strings.Contains(name, "_") {
		snake = strings.ToLower(name)
		pascal = names.SnakeToPascal(name)
	}
	fileName := snake + ".go"
	relPath := filepath.Join("app", "http", "requests", fileName)
	return writeScaffold(projectDir, relPath, tmplMakeRequest(pascal+"Request"))
}

// MakeMigration scaffolds a new migration file.
func MakeMigration(name, projectDir string) (string, error) {
	if err := sanitizeName(name); err != nil {
		return "", err
	}
	snake := names.PascalToSnake(name)
	if strings.Contains(name, "_") {
		snake = strings.ToLower(name)
	}
	ts := time.Now().Format("2006_01_02_150405")
	structName := names.SnakeToPascal(snake) + "_" + ts
	fileName := ts + "_" + snake + ".go"
	relPath := filepath.Join("database", "migrations", fileName)

	// Infer table name from description like "create_posts_table" → "posts"
	tableName := inferTableName(snake)

	return writeScaffold(projectDir, relPath, tmplMakeMigration(structName, tableName))
}

// sanitizeName rejects names containing path traversal sequences.
// Forward slashes are allowed for subdirectory scaffolding (e.g. "admin/User").
func sanitizeName(name string) error {
	if name == "" || name == "." {
		return fmt.Errorf("invalid name: must not be empty")
	}
	if strings.Contains(name, "..") || strings.Contains(name, "\\") {
		return fmt.Errorf("invalid name %q: must not contain '..' or backslashes", name)
	}
	return nil
}

func writeScaffold(projectDir, relPath, content string) (string, error) {
	absPath := filepath.Join(projectDir, relPath)
	// Ensure the resolved path is inside the project directory.
	absProject, _ := filepath.Abs(projectDir)
	absTarget, _ := filepath.Abs(absPath)
	if !strings.HasPrefix(absTarget, absProject+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes project directory", relPath)
	}
	if _, err := os.Stat(absPath); err == nil {
		return "", fmt.Errorf("%s already exists", relPath)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return "", fmt.Errorf("creating directory: %w", err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return "", err
	}
	return relPath, nil
}

func inferTableName(snake string) string {
	// create_posts_table → posts, add_index_to_users → users
	s := strings.TrimPrefix(snake, "create_")
	s = strings.TrimSuffix(s, "_table")
	if s != snake {
		return s
	}
	// Fallback: use the last word pluralized
	parts := strings.Split(snake, "_")
	return parts[len(parts)-1]
}

func tmplMakeController(structName, moduleName string) string {
	return r(`package controllers

import pickle "{{.ModuleName}}/app/http"

type `+structName+` struct {
	pickle.Controller
}

func (c `+structName+`) Index(ctx *pickle.Context) pickle.Response {
	// TODO: list resources
	return ctx.JSON(200, map[string]string{"status": "ok"})
}

func (c `+structName+`) Show(ctx *pickle.Context) pickle.Response {
	// TODO: show resource by ctx.Param("id")
	return ctx.JSON(200, nil)
}

func (c `+structName+`) Store(ctx *pickle.Context) pickle.Response {
	// TODO: create resource
	return ctx.JSON(201, nil)
}

func (c `+structName+`) Update(ctx *pickle.Context) pickle.Response {
	// TODO: update resource
	return ctx.JSON(200, nil)
}

func (c `+structName+`) Destroy(ctx *pickle.Context) pickle.Response {
	// TODO: delete resource
	return ctx.NoContent()
}
`, moduleName)
}

func tmplMakeMiddleware(funcName, moduleName string) string {
	return r(`package middleware

import pickle "{{.ModuleName}}/app/http"

func `+funcName+`(ctx *pickle.Context, next func() pickle.Response) pickle.Response {
	// TODO: implement middleware logic
	return next()
}
`, moduleName)
}

func tmplMakeRequest(structName string) string {
	return `package requests

type ` + structName + ` struct {
	// Example:
	// Name string ` + "`" + `json:"name" validate:"required"` + "`" + `
}
`
}

func tmplMakeMigration(structName, tableName string) string {
	return fmt.Sprintf(`package migrations

type %s struct {
	Migration
}

func (m *%s) Up() {
	m.CreateTable("%s", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
		t.Timestamps()
	})
}

func (m *%s) Down() {
	m.DropTableIfExists("%s")
}
`, structName, structName, tableName, structName, tableName)
}

func r(tmpl, moduleName string) string {
	return strings.ReplaceAll(tmpl, "{{.ModuleName}}", moduleName)
}

func tmplGoMod(mod string) string {
	return r(`module {{.ModuleName}}

go 1.23
`, mod)
}

func tmplDotEnv() string {
	return `APP_NAME=myapp
APP_ENV=local
APP_DEBUG=true
APP_PORT=8080
APP_URL=http://localhost:8080

DB_CONNECTION=pgsql
DB_HOST=127.0.0.1
DB_PORT=5432
DB_DATABASE=myapp
DB_USERNAME=postgres
DB_PASSWORD=
`
}

func tmplMain(mod string) string {
	return r(`package main

import (
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"

	"{{.ModuleName}}/app/commands"
)

func main() {
	commands.NewApp().Run(os.Args[1:])
}
`, mod)
}

func tmplConfigApp() string {
	return `package config

// AppConfig holds application-level settings.
type AppConfig struct {
	Name  string
	Env   string
	Debug bool
	Port  string
	URL   string
}

func app() AppConfig {
	return AppConfig{
		Name:  Env("APP_NAME", "myapp"),
		Env:   Env("APP_ENV", "local"),
		Debug: Env("APP_DEBUG", "true") == "true",
		Port:  Env("APP_PORT", "8080"),
		URL:   Env("APP_URL", "http://localhost:8080"),
	}
}
`
}

func tmplConfigDatabase() string {
	return `package config

// DatabaseConfig holds named database connections with a default.
type DatabaseConfig struct {
	Default     string
	Connections map[string]ConnectionConfig
}

func database() DatabaseConfig {
	return DatabaseConfig{
		Default: Env("DB_CONNECTION", "pgsql"),
		Connections: map[string]ConnectionConfig{
			"pgsql": {
				Driver:   "pgsql",
				Host:     Env("DB_HOST", "127.0.0.1"),
				Port:     Env("DB_PORT", "5432"),
				Name:     Env("DB_DATABASE", "myapp"),
				User:     Env("DB_USERNAME", "postgres"),
				Password: Env("DB_PASSWORD", ""),
			},
			"sqlite": {
				Driver: "sqlite",
				Name:   Env("DB_DATABASE", "database.sqlite"),
			},
		},
	}
}
`
}

func tmplRoutes(mod string) string {
	return r(`package routes

import (
	pickle "{{.ModuleName}}/app/http"
	"{{.ModuleName}}/app/http/controllers"
)

var API = pickle.Routes(func(r *pickle.Router) {
	r.Group("/api", func(r *pickle.Router) {
		r.Get("/", controllers.WelcomeController{}.Index)
	})
})
`, mod)
}

func tmplWelcomeController(mod string) string {
	return r(`package controllers

import (
	pickle "{{.ModuleName}}/app/http"
)

type WelcomeController struct {
	pickle.Controller
}

func (c WelcomeController) Index(ctx *pickle.Context) pickle.Response {
	return ctx.JSON(200, map[string]string{
		"message": "Welcome to Pickle!",
	})
}
`, mod)
}

func tmplAuthMiddleware(mod string) string {
	return r(`package middleware

import pickle "{{.ModuleName}}/app/http"

func Auth(ctx *pickle.Context, next func() pickle.Response) pickle.Response {
	token := ctx.BearerToken()
	if token == "" {
		return ctx.Unauthorized("missing token")
	}

	// TODO: validate token and set auth info
	// ctx.SetAuth(claims)

	return next()
}
`, mod)
}

func tmplLoginRequest() string {
	return `package requests

type LoginRequest struct {
	Email    string ` + "`" + `json:"email" validate:"required,email"` + "`" + `
	Password string ` + "`" + `json:"password" validate:"required,min=8"` + "`" + `
}
`
}

func tmplMigration(ts string) string {
	structName := "CreateUsersTable_" + ts
	return fmt.Sprintf(`package migrations

type %s struct {
	Migration
}

func (m *%s) Up() {
	m.CreateTable("users", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
		t.String("name").NotNull()
		t.String("email").NotNull().Unique()
		t.String("password").NotNull()
		t.Timestamps()
	})
}

func (m *%s) Down() {
	m.DropTableIfExists("users")
}
`, structName, structName, structName)
}
