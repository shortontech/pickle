package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"User", false},
		{"create_posts_table", false},
		{"RateLimit", false},
		{"admin/User", false},
		{"../../../etc/passwd", true},
		{"../tmp/Evil", true},
		{"admin/../../../etc/passwd", true},
		{"foo\\bar", true},
		{"..", true},
		{".", true},
		{"", true},
	}

	for _, tt := range tests {
		err := sanitizeName(tt.name)
		if (err != nil) != tt.wantErr {
			t.Errorf("sanitizeName(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
		}
	}
}

func TestSanitizeNameStartsWithDigit(t *testing.T) {
	err := sanitizeName("1user")
	if err == nil {
		t.Error("expected error for name starting with digit")
	}
}

func TestSanitizeNameInvalidChars(t *testing.T) {
	for _, name := range []string{"my-controller", "foo bar", "user@name", "foo+bar"} {
		err := sanitizeName(name)
		if err == nil {
			t.Errorf("expected error for name %q with invalid chars", name)
		}
	}
}

func TestMakeControllerPathTraversal(t *testing.T) {
	dir := t.TempDir()
	_, err := MakeController("../../../tmp/Evil", dir, "example.com/app")
	if err == nil {
		t.Fatal("expected error for path traversal name, got nil")
	}
}

func TestMakeControllerValid(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeController("Product", dir, "example.com/app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join("app", "http", "controllers", "product_controller.go")
	if relPath != expected {
		t.Errorf("got %q, want %q", relPath, expected)
	}
	if _, err := os.Stat(filepath.Join(dir, relPath)); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestMakeControllerTrimsControllerSuffix(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeController("ProductController", dir, "example.com/app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(relPath, "product_controller.go") {
		t.Errorf("expected product_controller.go, got %q", relPath)
	}
}

func TestMakeControllerSnakeName(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeController("blog_post", dir, "example.com/app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(relPath, "blog_post_controller.go") {
		t.Errorf("expected blog_post_controller.go, got %q", relPath)
	}
	content, _ := os.ReadFile(filepath.Join(dir, relPath))
	if !strings.Contains(string(content), "BlogPostController") {
		t.Errorf("expected BlogPostController in content, got:\n%s", content)
	}
}

func TestMakeControllerContent(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeController("Order", dir, "myapp.com/proj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(dir, relPath))
	s := string(content)
	if !strings.Contains(s, "OrderController") {
		t.Error("expected OrderController struct in content")
	}
	if !strings.Contains(s, "myapp.com/proj/app/http") {
		t.Error("expected module path in import")
	}
	if !strings.Contains(s, "func (c OrderController) Index") {
		t.Error("expected Index method")
	}
}

func TestMakeControllerDuplicate(t *testing.T) {
	dir := t.TempDir()
	_, err := MakeController("Widget", dir, "example.com/app")
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	_, err = MakeController("Widget", dir, "example.com/app")
	if err == nil {
		t.Fatal("expected error for duplicate controller, got nil")
	}
}

func TestMakeMiddlewareValid(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeMiddleware("RateLimit", dir, "example.com/app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join("app", "http", "middleware", "rate_limit.go")
	if relPath != expected {
		t.Errorf("got %q, want %q", relPath, expected)
	}
	if _, err := os.Stat(filepath.Join(dir, relPath)); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestMakeMiddlewareContent(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeMiddleware("Cors", dir, "myapp.com/proj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(dir, relPath))
	s := string(content)
	if !strings.Contains(s, "func Cors(") {
		t.Error("expected Cors function in content")
	}
	if !strings.Contains(s, "myapp.com/proj/app/http") {
		t.Error("expected module path in import")
	}
	if !strings.Contains(s, "return next()") {
		t.Error("expected next() call in content")
	}
}

func TestMakeMiddlewareInvalidName(t *testing.T) {
	dir := t.TempDir()
	_, err := MakeMiddleware("../evil", dir, "example.com/app")
	if err == nil {
		t.Fatal("expected error for path traversal name")
	}
}

func TestMakeRequestValid(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeRequest("CreateUser", dir, "example.com/app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join("app", "http", "requests", "create_user.go")
	if relPath != expected {
		t.Errorf("got %q, want %q", relPath, expected)
	}
	if _, err := os.Stat(filepath.Join(dir, relPath)); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestMakeRequestTrimsRequestSuffix(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeRequest("CreateUserRequest", dir, "example.com/app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(relPath, "create_user.go") {
		t.Errorf("expected create_user.go, got %q", relPath)
	}
	content, _ := os.ReadFile(filepath.Join(dir, relPath))
	if !strings.Contains(string(content), "CreateUserRequest") {
		t.Errorf("expected CreateUserRequest struct in content")
	}
}

func TestMakeRequestSnakeName(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeRequest("update_post", dir, "example.com/app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(relPath, "update_post.go") {
		t.Errorf("expected update_post.go, got %q", relPath)
	}
	content, _ := os.ReadFile(filepath.Join(dir, relPath))
	if !strings.Contains(string(content), "UpdatePostRequest") {
		t.Errorf("expected UpdatePostRequest in content, got:\n%s", content)
	}
}

func TestMakeRequestContent(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeRequest("StoreOrder", dir, "example.com/app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(dir, relPath))
	s := string(content)
	if !strings.Contains(s, "package requests") {
		t.Error("expected package requests")
	}
	if !strings.Contains(s, "StoreOrderRequest") {
		t.Error("expected StoreOrderRequest struct")
	}
}

func TestMakeRequestInvalidName(t *testing.T) {
	dir := t.TempDir()
	_, err := MakeRequest("../evil", dir, "example.com/app")
	if err == nil {
		t.Fatal("expected error for path traversal name")
	}
}

func TestMakeMigrationValid(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeMigration("create_posts_table", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(relPath, filepath.Join("database", "migrations")) {
		t.Errorf("expected migration in database/migrations, got %q", relPath)
	}
	if !strings.HasSuffix(relPath, "_create_posts_table.go") {
		t.Errorf("expected _create_posts_table.go suffix, got %q", relPath)
	}
	if _, err := os.Stat(filepath.Join(dir, relPath)); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestMakeMigrationContent(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeMigration("create_orders_table", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(dir, relPath))
	s := string(content)
	if !strings.Contains(s, "package migrations") {
		t.Error("expected package migrations")
	}
	if !strings.Contains(s, "Migration") {
		t.Error("expected embedded Migration struct")
	}
	if !strings.Contains(s, `"orders"`) {
		t.Errorf("expected table name 'orders' in content, got:\n%s", s)
	}
	if !strings.Contains(s, "func (m *") {
		t.Error("expected Up/Down methods")
	}
	if !strings.Contains(s, "DropTableIfExists") {
		t.Error("expected DropTableIfExists in Down method")
	}
}

func TestMakeMigrationPascalName(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeMigration("AddIndexToUsers", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(relPath, "add_index_to_users") {
		t.Errorf("expected snake_case filename, got %q", relPath)
	}
}

func TestMakeMigrationInvalidName(t *testing.T) {
	dir := t.TempDir()
	_, err := MakeMigration("../evil", dir)
	if err == nil {
		t.Fatal("expected error for path traversal name")
	}
}

func TestWriteScaffoldEscapeCheck(t *testing.T) {
	dir := t.TempDir()
	_, err := writeScaffold(dir, "../outside.go", "package x")
	if err == nil {
		t.Fatal("expected error for path escaping project directory")
	}
}

func TestWriteScaffoldDuplicate(t *testing.T) {
	dir := t.TempDir()
	_, err := writeScaffold(dir, "foo.go", "package x")
	if err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	_, err = writeScaffold(dir, "foo.go", "package x")
	if err == nil {
		t.Fatal("expected error for duplicate file")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' in error, got: %v", err)
	}
}

func TestWriteScaffoldCreatesContent(t *testing.T) {
	dir := t.TempDir()
	relPath, err := writeScaffold(dir, "sub/dir/file.go", "package sub")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if relPath != "sub/dir/file.go" {
		t.Errorf("expected sub/dir/file.go, got %q", relPath)
	}
	content, _ := os.ReadFile(filepath.Join(dir, relPath))
	if string(content) != "package sub" {
		t.Errorf("unexpected content: %q", string(content))
	}
}

func TestInferTableName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"create_posts_table", "posts"},
		{"create_users_table", "users"},
		{"create_transfers_table", "transfers"},
		{"add_index_to_users", "users"},
		{"drop_orders_table", "drop_orders"},
		{"some_migration", "migration"},
	}
	for _, tt := range tests {
		got := inferTableName(tt.input)
		if got != tt.want {
			t.Errorf("inferTableName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCreate(t *testing.T) {
	dir := t.TempDir()
	err := Create("example.com/myapp", dir)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	expectedFiles := []string{
		"go.mod",
		".env",
		"cmd/server/main.go",
		"config/app.go",
		"config/database.go",
		"routes/web.go",
		"app/http/controllers/welcome_controller.go",
		"app/http/middleware/auth.go",
		"app/http/requests/login.go",
	}
	for _, rel := range expectedFiles {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("expected file %s to exist: %v", rel, err)
		}
	}

	// Check auth driver directories created
	for _, driver := range []string{"jwt", "session", "oauth"} {
		driverDir := filepath.Join(dir, "app", "http", "auth", driver)
		if _, err := os.Stat(driverDir); err != nil {
			t.Errorf("expected auth driver dir %s to exist: %v", driver, err)
		}
	}

	// Check commands directory created
	if _, err := os.Stat(filepath.Join(dir, "app", "commands")); err != nil {
		t.Errorf("expected app/commands directory to exist: %v", err)
	}
}

func TestCreateGoModContent(t *testing.T) {
	dir := t.TempDir()
	if err := Create("github.com/acme/project", dir); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	s := string(content)
	if !strings.Contains(s, "module github.com/acme/project") {
		t.Errorf("expected module declaration, got:\n%s", s)
	}
	if !strings.Contains(s, "go 1.23") {
		t.Errorf("expected go 1.23, got:\n%s", s)
	}
}

func TestCreateMainGoContent(t *testing.T) {
	dir := t.TempDir()
	if err := Create("github.com/acme/project", dir); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(dir, "cmd/server/main.go"))
	s := string(content)
	if !strings.Contains(s, "github.com/acme/project/app/commands") {
		t.Errorf("expected module path in main.go imports, got:\n%s", s)
	}
}

func TestCreateMigrationFile(t *testing.T) {
	dir := t.TempDir()
	if err := Create("myapp", dir); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// Find a migration file
	entries, err := os.ReadDir(filepath.Join(dir, "database", "migrations"))
	if err != nil {
		t.Fatalf("migrations dir not found: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one migration file")
	}
	var migFile string
	for _, e := range entries {
		if strings.Contains(e.Name(), "create_users_table") {
			migFile = filepath.Join(dir, "database", "migrations", e.Name())
		}
	}
	if migFile == "" {
		t.Fatal("expected create_users_table migration file")
	}
	content, _ := os.ReadFile(migFile)
	s := string(content)
	if !strings.Contains(s, "CreateUsersTable_") {
		t.Errorf("expected CreateUsersTable_ struct, got:\n%s", s)
	}
	if !strings.Contains(s, `"users"`) {
		t.Errorf("expected users table name, got:\n%s", s)
	}
}

func TestCreateRoutesContent(t *testing.T) {
	dir := t.TempDir()
	if err := Create("github.com/acme/app", dir); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(dir, "routes/web.go"))
	s := string(content)
	if !strings.Contains(s, "github.com/acme/app/app/http") {
		t.Errorf("expected module import in routes, got:\n%s", s)
	}
	if !strings.Contains(s, "WelcomeController") {
		t.Errorf("expected WelcomeController in routes, got:\n%s", s)
	}
}

func TestCreateDotEnvContent(t *testing.T) {
	dir := t.TempDir()
	if err := Create("myapp", dir); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(dir, ".env"))
	s := string(content)
	if !strings.Contains(s, "APP_NAME=myapp") {
		t.Errorf("expected APP_NAME in .env, got:\n%s", s)
	}
	if !strings.Contains(s, "DB_CONNECTION=pgsql") {
		t.Errorf("expected DB_CONNECTION in .env, got:\n%s", s)
	}
}

func TestTmplMakeControllerContent(t *testing.T) {
	out := tmplMakeController("FooController", "example.com/app")
	if !strings.Contains(out, "package controllers") {
		t.Error("expected package controllers")
	}
	if !strings.Contains(out, "FooController") {
		t.Error("expected FooController struct")
	}
	if !strings.Contains(out, "example.com/app/app/http") {
		t.Error("expected module path in import")
	}
	for _, method := range []string{"Index", "Show", "Store", "Update", "Destroy"} {
		if !strings.Contains(out, "func (c FooController) "+method) {
			t.Errorf("expected method %s", method)
		}
	}
}

func TestTmplMakeMiddlewareContent(t *testing.T) {
	out := tmplMakeMiddleware("MyMiddleware", "example.com/app")
	if !strings.Contains(out, "package middleware") {
		t.Error("expected package middleware")
	}
	if !strings.Contains(out, "func MyMiddleware(") {
		t.Error("expected func MyMiddleware")
	}
	if !strings.Contains(out, "return next()") {
		t.Error("expected return next()")
	}
}

func TestTmplMakeRequestContent(t *testing.T) {
	out := tmplMakeRequest("CreatePostRequest")
	if !strings.Contains(out, "package requests") {
		t.Error("expected package requests")
	}
	if !strings.Contains(out, "CreatePostRequest") {
		t.Error("expected CreatePostRequest struct")
	}
}

func TestTmplMakeMigrationContent(t *testing.T) {
	out := tmplMakeMigration("CreatePostsTable_2026_01_01_000000", "posts")
	if !strings.Contains(out, "package migrations") {
		t.Error("expected package migrations")
	}
	if !strings.Contains(out, "CreatePostsTable_2026_01_01_000000") {
		t.Error("expected struct name")
	}
	if !strings.Contains(out, `"posts"`) {
		t.Error("expected table name posts")
	}
	if !strings.Contains(out, "func (m *CreatePostsTable_2026_01_01_000000) Up()") {
		t.Error("expected Up method")
	}
	if !strings.Contains(out, "func (m *CreatePostsTable_2026_01_01_000000) Down()") {
		t.Error("expected Down method")
	}
}
