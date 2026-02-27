package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
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
