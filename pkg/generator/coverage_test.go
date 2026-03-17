package generator

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
)

// ─── core_generator.go ───────────────────────────────────────────────────────

func TestGenerateCoreConfig(t *testing.T) {
	src := string(GenerateCoreConfig("config"))
	if !strings.Contains(src, "package config") {
		t.Error("wrong package declaration")
	}
	if src == "" {
		t.Error("empty output")
	}
}

func TestGenerateCoreMigration(t *testing.T) {
	src := string(GenerateCoreMigration("migrations"))
	if !strings.Contains(src, "package migrations") {
		t.Error("wrong package declaration")
	}
	if src == "" {
		t.Error("empty output")
	}
}

// ─── dotenv.go ───────────────────────────────────────────────────────────────

func TestParseDotEnv(t *testing.T) {
	tmp := t.TempDir()
	envFile := filepath.Join(tmp, ".env")
	content := `# comment
APP_NAME=MyApp
DB_URL="postgres://localhost/mydb"
SECRET='mysecret'
EMPTY=
NO_VALUE
`
	if err := os.WriteFile(envFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	m := ParseDotEnv(envFile)

	if m["APP_NAME"] != "MyApp" {
		t.Errorf("APP_NAME = %q, want %q", m["APP_NAME"], "MyApp")
	}
	if m["DB_URL"] != "postgres://localhost/mydb" {
		t.Errorf("DB_URL = %q, want %q", m["DB_URL"], "postgres://localhost/mydb")
	}
	if m["SECRET"] != "mysecret" {
		t.Errorf("SECRET = %q, want %q", m["SECRET"], "mysecret")
	}
	if _, ok := m["EMPTY"]; !ok {
		t.Error("expected EMPTY key to exist")
	}
	if _, ok := m["#"]; ok {
		t.Error("comment line should not be parsed")
	}
}

func TestParseDotEnvMissingFile(t *testing.T) {
	m := ParseDotEnv("/nonexistent/.env")
	if m == nil {
		t.Error("expected empty map, not nil")
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}

// ─── docs.go ─────────────────────────────────────────────────────────────────

func TestDocsJSON(t *testing.T) {
	s := DocsJSON()
	if s == "" {
		t.Error("DocsJSON returned empty string")
	}
}

func TestParseDocs(t *testing.T) {
	docs, err := ParseDocs()
	if err != nil {
		t.Fatalf("ParseDocs: %v", err)
	}
	if len(docs) == 0 {
		t.Error("expected at least one doc entry")
	}
}

func TestFormatDocsMarkdownAll(t *testing.T) {
	out, err := FormatDocsMarkdown("")
	if err != nil {
		t.Fatalf("FormatDocsMarkdown: %v", err)
	}
	if !strings.Contains(out, "# Pickle Framework Documentation") {
		t.Error("missing header")
	}
}

func TestFormatDocsMarkdownFiltered(t *testing.T) {
	// Get the actual doc names to find a valid one
	docs, err := ParseDocs()
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 {
		t.Skip("no docs to test with")
	}

	name := docs[0].Name
	out, err := FormatDocsMarkdown(name)
	if err != nil {
		t.Fatalf("FormatDocsMarkdown(%q): %v", name, err)
	}
	if out == "" {
		t.Errorf("FormatDocsMarkdown(%q) returned empty string", name)
	}
}

func TestFormatDocsMarkdownNotFound(t *testing.T) {
	_, err := FormatDocsMarkdown("NonExistentTypeName12345")
	if err == nil {
		t.Error("expected error for unknown type name")
	}
}

// ─── generate.go ─────────────────────────────────────────────────────────────

func TestReadModulePath(t *testing.T) {
	tmp := t.TempDir()
	gomod := filepath.Join(tmp, "go.mod")
	content := "module github.com/example/myapp\n\ngo 1.22\n"
	os.WriteFile(gomod, []byte(content), 0o644)

	mod, err := readModulePath(gomod)
	if err != nil {
		t.Fatalf("readModulePath: %v", err)
	}
	if mod != "github.com/example/myapp" {
		t.Errorf("got %q, want %q", mod, "github.com/example/myapp")
	}
}

func TestReadModulePathMissing(t *testing.T) {
	_, err := readModulePath("/nonexistent/go.mod")
	if err == nil {
		t.Error("expected error for missing go.mod")
	}
}

func TestReadModulePathNoDirective(t *testing.T) {
	tmp := t.TempDir()
	gomod := filepath.Join(tmp, "go.mod")
	os.WriteFile(gomod, []byte("go 1.22\n"), 0o644)
	_, err := readModulePath(gomod)
	if err == nil {
		t.Error("expected error for missing module directive")
	}
}

func TestDetectProject(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module github.com/example/app\n\ngo 1.22\n"), 0o644)

	proj, err := DetectProject(tmp)
	if err != nil {
		t.Fatalf("DetectProject: %v", err)
	}
	if proj.ModulePath != "github.com/example/app" {
		t.Errorf("ModulePath = %q", proj.ModulePath)
	}
	if !strings.HasSuffix(proj.Layout.HTTPDir, filepath.Join("app", "http")) {
		t.Errorf("HTTPDir = %q", proj.Layout.HTTPDir)
	}
	if !strings.HasSuffix(proj.Layout.ModelsDir, filepath.Join("app", "models")) {
		t.Errorf("ModelsDir = %q", proj.Layout.ModelsDir)
	}
}

func TestDetectProjectRelative(t *testing.T) {
	// Just ensure the test data project resolves
	proj, err := DetectProject(filepath.Join("..", "..", "testdata", "basic-crud"))
	if err != nil {
		t.Fatalf("DetectProject: %v", err)
	}
	if proj.ModulePath == "" {
		t.Error("expected non-empty module path")
	}
}

func TestScanMigrationStructs(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "basic-crud", "database", "migrations")
	structs, err := ScanMigrationStructs(dir)
	if err != nil {
		t.Fatalf("ScanMigrationStructs: %v", err)
	}
	if len(structs) == 0 {
		t.Fatal("expected at least one migration struct")
	}
	// All should embed Migration
	for _, s := range structs {
		if s == "" {
			t.Error("empty struct name")
		}
	}
}

func TestScanMigrationStructsEmpty(t *testing.T) {
	tmp := t.TempDir()
	structs, err := ScanMigrationStructs(tmp)
	if err != nil {
		t.Fatalf("ScanMigrationStructs: %v", err)
	}
	if len(structs) != 0 {
		t.Errorf("expected 0 structs, got %d", len(structs))
	}
}

func TestResolveModelDir(t *testing.T) {
	modelsDir := "/app/models"

	// Top-level table (not in nesting map)
	dir, pkg := resolveModelDir(modelsDir, "users", nil)
	if dir != modelsDir {
		t.Errorf("top-level dir = %q, want %q", dir, modelsDir)
	}
	if pkg != "models" {
		t.Errorf("top-level pkg = %q, want %q", pkg, "models")
	}

	// Nested table
	nestingMap := map[string]SchemaRelationship{
		"posts": {ParentTable: "users", ChildTable: "posts"},
	}
	dir2, pkg2 := resolveModelDir(modelsDir, "posts", nestingMap)
	if dir2 == modelsDir {
		t.Error("nested table should not map to models root")
	}
	if pkg2 == "models" {
		t.Error("nested table should have a specific package name")
	}

	// TopLevel = true should go to models root
	nestingMapTopLevel := map[string]SchemaRelationship{
		"posts": {ParentTable: "users", ChildTable: "posts", TopLevel: true},
	}
	dir3, pkg3 := resolveModelDir(modelsDir, "posts", nestingMapTopLevel)
	if dir3 != modelsDir {
		t.Errorf("TopLevel table dir = %q, want %q", dir3, modelsDir)
	}
	if pkg3 != "models" {
		t.Errorf("TopLevel table pkg = %q, want %q", pkg3, "models")
	}
}

func TestWriteFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "subdir", "test.go")
	content := []byte("package test\n")

	if err := writeFile(path, content); err != nil {
		t.Fatalf("writeFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch")
	}
}

// ─── registry_generator.go ───────────────────────────────────────────────────

func TestStructNameToMigrationID(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"CreateUsersTable_2026_02_21_100000", "2026_02_21_100000_create_users_table"},
		{"CreatePostsTable_2026_02_21_100001", "2026_02_21_100001_create_posts_table"},
		{"AddEmailToUsers_2026_03_01_120000", "2026_03_01_120000_add_email_to_users"},
		{"NoTimestamp", ""},
	}
	for _, tt := range tests {
		got := structNameToMigrationID(tt.in)
		if got != tt.want {
			t.Errorf("structNameToMigrationID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestScanMigrationFiles(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "basic-crud", "database", "migrations")
	entries, err := ScanMigrationFiles(dir)
	if err != nil {
		t.Fatalf("ScanMigrationFiles: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one migration entry")
	}
	for _, e := range entries {
		if e.ID == "" {
			t.Errorf("entry has empty ID: %+v", e)
		}
		if e.StructName == "" {
			t.Errorf("entry has empty StructName: %+v", e)
		}
	}
}

func TestScanMigrationFilesEmpty(t *testing.T) {
	tmp := t.TempDir()
	entries, err := ScanMigrationFiles(tmp)
	if err != nil {
		t.Fatalf("ScanMigrationFiles: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestGenerateRegistry(t *testing.T) {
	entries := []MigrationFileEntry{
		{ID: "2026_02_21_100000_create_users_table", StructName: "CreateUsersTable_2026_02_21_100000"},
		{ID: "2026_02_21_100001_create_posts_table", StructName: "CreatePostsTable_2026_02_21_100001"},
	}

	out, err := GenerateRegistry("migrations", entries)
	if err != nil {
		t.Fatalf("GenerateRegistry: %v", err)
	}

	src := string(out)

	if !strings.Contains(src, "package migrations") {
		t.Error("wrong package")
	}
	if !strings.Contains(src, "var Registry") {
		t.Error("missing Registry var")
	}
	if !strings.Contains(src, "CreateUsersTable_2026_02_21_100000") {
		t.Error("missing users migration struct")
	}
	if !strings.Contains(src, "2026_02_21_100000_create_users_table") {
		t.Error("missing users migration ID")
	}
}

func TestGenerateRegistryWithImports(t *testing.T) {
	entries := []MigrationFileEntry{
		{ID: "2026_02_21_100000_create_users_table", StructName: "CreateUsersTable_2026_02_21_100000", ImportPath: "myapp/database/migrations"},
		{ID: "2026_02_21_100001_create_sessions_table", StructName: "CreateSessionsTable_2026_02_21_100001", ImportPath: "myapp/database/migrations/auth"},
	}

	out, err := GenerateRegistry("migrations", entries)
	if err != nil {
		t.Fatalf("GenerateRegistry: %v", err)
	}

	src := string(out)
	// Verify it parses as valid Go
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "registry_gen.go", src, 0); err != nil {
		t.Fatalf("generated registry does not parse: %v\n%s", err, src)
	}
}

func TestScanAllMigrationFiles(t *testing.T) {
	migrationsDir := filepath.Join("..", "..", "testdata", "basic-crud", "database", "migrations")
	dirs := []MigrationDir{
		{Dir: migrationsDir, ImportPath: "myapp/database/migrations"},
	}

	entries, err := ScanAllMigrationFiles(dirs)
	if err != nil {
		t.Fatalf("ScanAllMigrationFiles: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one entry")
	}
	for _, e := range entries {
		if e.ImportPath != "myapp/database/migrations" {
			t.Errorf("ImportPath = %q", e.ImportPath)
		}
	}
}

// ─── response_generator.go ───────────────────────────────────────────────────

func TestHasOwnership(t *testing.T) {
	t.Run("no ownership", func(t *testing.T) {
		tbl := &schema.Table{Name: "users"}
		tbl.UUID("id").PrimaryKey()
		tbl.String("name").NotNull()
		if HasOwnership(tbl) {
			t.Error("table without owner column should return false")
		}
	})

	t.Run("has owner but no visibility", func(t *testing.T) {
		tbl := &schema.Table{Name: "posts"}
		tbl.UUID("id").PrimaryKey()
		tbl.UUID("user_id").NotNull().IsOwner()
		tbl.String("title").NotNull()
		if HasOwnership(tbl) {
			t.Error("table with owner but no Public/OwnerSees should return false")
		}
	})

	t.Run("has full ownership", func(t *testing.T) {
		tbl := &schema.Table{Name: "posts"}
		tbl.UUID("id").PrimaryKey().Public()
		tbl.UUID("user_id").NotNull().IsOwner()
		tbl.String("title").NotNull().Public()
		if !HasOwnership(tbl) {
			t.Error("table with owner and Public columns should return true")
		}
	})
}

func TestGenerateResponses(t *testing.T) {
	tbl := &schema.Table{Name: "posts"}
	tbl.UUID("id").PrimaryKey().Public()
	tbl.UUID("user_id").NotNull().IsOwner()
	tbl.String("title", 255).NotNull().Public()
	tbl.Text("body").NotNull().OwnerSees()
	tbl.String("internal_notes").NotNull()
	tbl.Timestamps()

	out, err := GenerateResponses(tbl, "models")
	if err != nil {
		t.Fatalf("GenerateResponses: %v", err)
	}

	src := string(out)

	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "post_responses.go", src, 0); err != nil {
		t.Fatalf("generated code does not parse: %v\n%s", err, src)
	}

	if !strings.Contains(src, "PostPublicResponse") {
		t.Error("missing PostPublicResponse")
	}
	if !strings.Contains(src, "PostOwnerResponse") {
		t.Error("missing PostOwnerResponse")
	}
	if !strings.Contains(src, "func SerializePost(") {
		t.Error("missing SerializePost")
	}
	if !strings.Contains(src, "func SerializePosts(") {
		t.Error("missing SerializePosts")
	}
}

func TestGenerateResponsesNoOwnerColumn(t *testing.T) {
	tbl := &schema.Table{Name: "items"}
	tbl.UUID("id").PrimaryKey().Public()
	tbl.String("name").NotNull().Public()

	_, err := GenerateResponses(tbl, "models")
	if err == nil {
		t.Error("expected error when no owner column exists")
	}
}

func TestBuildOwnerMatch(t *testing.T) {
	tests := []struct {
		fieldName string
		colType   schema.ColumnType
		wantPart  string
	}{
		{"UserID", schema.UUID, "UserID.String() == ownerID"},
		{"UserID", schema.Integer, "fmt.Sprint(record.UserID) == ownerID"},
		{"UserID", schema.BigInteger, "fmt.Sprint(record.UserID) == ownerID"},
		{"UserID", schema.String, "record.UserID == ownerID"},
	}
	for _, tt := range tests {
		col := &schema.Column{Type: tt.colType}
		got := buildOwnerMatch(tt.fieldName, col)
		if !strings.Contains(got, tt.wantPart) {
			t.Errorf("buildOwnerMatch(%q, %v) = %q, want it to contain %q", tt.fieldName, tt.colType, got, tt.wantPart)
		}
	}
}

// ─── view_generator.go ───────────────────────────────────────────────────────

func TestGenerateViewModel(t *testing.T) {
	view := &schema.View{Name: "user_stats"}
	view.From("users", "u")
	view.SelectRaw("id", "u.id").UUIDType()
	view.SelectRaw("name", "u.name").StringType()
	view.SelectRaw("email", "u.email").StringType()

	out, err := GenerateViewModel(view, "models")
	if err != nil {
		t.Fatalf("GenerateViewModel: %v", err)
	}

	src := string(out)

	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "user_summary.go", src, 0); err != nil {
		t.Fatalf("generated code does not parse: %v\n%s", err, src)
	}

	if !strings.Contains(src, "type UserStat struct") {
		t.Errorf("missing struct declaration\n%s", src)
	}
	if !strings.Contains(src, "// Code generated by Pickle. DO NOT EDIT.") {
		t.Error("missing generated header")
	}
}

func TestGenerateViewModelWithTimestamp(t *testing.T) {
	view := &schema.View{Name: "post_stats"}
	view.From("posts", "p")
	// Use SelectRaw to explicitly set types (Column() doesn't know types without inspector)
	view.SelectRaw("id", "p.id").UUIDType()
	view.SelectRaw("created_at", "p.created_at").TimestampType()
	view.SelectRaw("post_count", "COUNT(*)").BigInteger()

	out, err := GenerateViewModel(view, "models")
	if err != nil {
		t.Fatalf("GenerateViewModel: %v", err)
	}

	src := string(out)

	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "post_stat.go", src, 0); err != nil {
		t.Fatalf("generated code does not parse: %v\n%s", err, src)
	}

	if !strings.Contains(src, `"time"`) {
		t.Error("expected time import for timestamp column")
	}
}

// ─── scope_generator.go (view part) ──────────────────────────────────────────

func TestGenerateViewQueryScopes(t *testing.T) {
	view := &schema.View{Name: "post_summaries"}
	view.From("posts", "p")
	view.SelectRaw("id", "p.id").UUIDType()
	view.SelectRaw("title", "p.title").StringType()
	view.SelectRaw("created_at", "p.created_at").TimestampType()

	blocks := loadScopeBlocks(t)
	out, err := GenerateViewQueryScopes(view, blocks, "models")
	if err != nil {
		t.Fatalf("GenerateViewQueryScopes: %v", err)
	}

	src := string(out)

	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "post_summary_query.go", src, 0); err != nil {
		t.Fatalf("generated code does not parse: %v\n%s", err, src)
	}

	if !strings.Contains(src, "PostSummarieQuery struct") {
		t.Errorf("missing query type\n%s", src)
	}
	if !strings.Contains(src, "func QueryPostSummarie()") {
		t.Errorf("missing query constructor\n%s", src)
	}
	if !strings.Contains(src, "// Code generated by Pickle. DO NOT EDIT.") {
		t.Error("missing generated header")
	}
}

// ─── config_generator.go ─────────────────────────────────────────────────────

func TestScanConfigs(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "basic-crud", "config")
	result, err := ScanConfigs(dir)
	if err != nil {
		t.Fatalf("ScanConfigs: %v", err)
	}
	if len(result.Configs) == 0 {
		t.Error("expected at least one config function")
	}
	for _, c := range result.Configs {
		if !strings.HasSuffix(c.ReturnType, "Config") {
			t.Errorf("ReturnType %q should end in Config", c.ReturnType)
		}
		if c.VarName == "" {
			t.Errorf("VarName should not be empty for %v", c)
		}
	}
}

func TestScanConfigsEmpty(t *testing.T) {
	tmp := t.TempDir()
	result, err := ScanConfigs(tmp)
	if err != nil {
		t.Fatalf("ScanConfigs: %v", err)
	}
	if len(result.Configs) != 0 {
		t.Errorf("expected 0 configs, got %d", len(result.Configs))
	}
}

func TestGenerateConfigGlue(t *testing.T) {
	scan := &ConfigScanResult{
		Configs: []ConfigDef{
			{FuncName: "app", ReturnType: "AppConfig", VarName: "App"},
			{FuncName: "database", ReturnType: "DatabaseConfig", VarName: "Database"},
		},
		HasDatabaseConfig: true,
	}

	out, err := GenerateConfigGlue(scan, "config")
	if err != nil {
		t.Fatalf("GenerateConfigGlue: %v", err)
	}

	src := string(out)

	if !strings.Contains(src, "var App AppConfig") {
		t.Error("missing App var")
	}
	if !strings.Contains(src, "var Database DatabaseConfig") {
		t.Error("missing Database var")
	}
	if !strings.Contains(src, "func Init()") {
		t.Error("missing Init function")
	}
	if !strings.Contains(src, "func (d DatabaseConfig) Connection(") {
		t.Error("missing Connection method (HasDatabaseConfig=true)")
	}
}

func TestGenerateConfigGlueNoDB(t *testing.T) {
	scan := &ConfigScanResult{
		Configs: []ConfigDef{
			{FuncName: "app", ReturnType: "AppConfig", VarName: "App"},
		},
		HasDatabaseConfig: false,
	}

	out, err := GenerateConfigGlue(scan, "config")
	if err != nil {
		t.Fatalf("GenerateConfigGlue: %v", err)
	}

	src := string(out)
	if strings.Contains(src, "func (d DatabaseConfig) Connection(") {
		t.Error("should not generate DB methods when HasDatabaseConfig=false")
	}
}

func TestExportName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"app", "App"},
		{"database", "Database"},
		{"", ""},
		{"API", "API"},
	}
	for _, tt := range tests {
		got := exportName(tt.in)
		if got != tt.want {
			t.Errorf("exportName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ─── command_generator.go ─────────────────────────────────────────────────────

func TestScanCommands(t *testing.T) {
	tmp := t.TempDir()
	cmdFile := `package commands

type SendEmailCommand struct{}

func (c SendEmailCommand) Name() string        { return "email:send" }
func (c SendEmailCommand) Description() string { return "Send emails" }
func (c SendEmailCommand) Run(args []string) error { return nil }
`
	os.WriteFile(filepath.Join(tmp, "send_email.go"), []byte(cmdFile), 0o644)

	cmds, err := ScanCommands(tmp)
	if err != nil {
		t.Fatalf("ScanCommands: %v", err)
	}
	if len(cmds) != 1 || cmds[0] != "SendEmailCommand" {
		t.Errorf("got %v, want [SendEmailCommand]", cmds)
	}
}

func TestScanCommandsEmpty(t *testing.T) {
	tmp := t.TempDir()
	cmds, err := ScanCommands(tmp)
	if err != nil {
		t.Fatalf("ScanCommands: %v", err)
	}
	if len(cmds) != 0 {
		t.Errorf("expected 0 commands, got %d", len(cmds))
	}
}

func TestScanRouteVars(t *testing.T) {
	tmp := t.TempDir()
	routeFile := `package routes

import pickle "myapp/app/http"

var API = pickle.Routes(func(r *pickle.Router) {})
var Internal = pickle.Routes(func(r *pickle.Router) {})
`
	os.WriteFile(filepath.Join(tmp, "web.go"), []byte(routeFile), 0o644)

	vars, err := ScanRouteVars(tmp)
	if err != nil {
		t.Fatalf("ScanRouteVars: %v", err)
	}
	if len(vars) != 2 {
		t.Errorf("got %v, want [API Internal]", vars)
	}
}

func TestGenerateCommandsGlue(t *testing.T) {
	out, err := GenerateCommandsGlue(
		"github.com/example/myapp",
		"database/migrations",
		[]string{"SendEmailCommand"},
		[]string{"API"},
		false,
	)
	if err != nil {
		t.Fatalf("GenerateCommandsGlue: %v", err)
	}

	src := string(out)

	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "pickle_gen.go", src, 0); err != nil {
		t.Fatalf("generated code does not parse: %v\n%s", err, src)
	}

	if !strings.Contains(src, "SendEmailCommand{}") {
		t.Error("missing user command")
	}
	if !strings.Contains(src, "routes.API.RegisterRoutes(mux)") {
		t.Error("missing route registration")
	}
}

func TestGenerateCommandsGlueWithAuth(t *testing.T) {
	out, err := GenerateCommandsGlue(
		"github.com/example/myapp",
		"database/migrations",
		nil,
		[]string{"API"},
		true,
	)
	if err != nil {
		t.Fatalf("GenerateCommandsGlue: %v", err)
	}

	src := string(out)
	if !strings.Contains(src, `"github.com/example/myapp/app/http/auth"`) {
		t.Error("missing auth import when hasAuth=true")
	}
}

func TestGenerateCommandsGlueDefaultRouteVar(t *testing.T) {
	out, err := GenerateCommandsGlue(
		"github.com/example/myapp",
		"database/migrations",
		nil,
		nil, // no route vars — should default to API
		false,
	)
	if err != nil {
		t.Fatalf("GenerateCommandsGlue: %v", err)
	}

	src := string(out)
	if !strings.Contains(src, "routes.API.RegisterRoutes(mux)") {
		t.Error("should default to API route var when none specified")
	}
}

// ─── model_generator.go ──────────────────────────────────────────────────────

func TestGenerateModelWithPublicProjection(t *testing.T) {
	tbl := &schema.Table{Name: "users"}
	tbl.UUID("id").PrimaryKey()
	tbl.String("name").NotNull()
	tbl.String("password").NotNull() // should be hidden (json:"-")

	out, err := GenerateModel(tbl, "models")
	if err != nil {
		t.Fatalf("GenerateModel: %v", err)
	}

	src := string(out)

	// Should have Public struct and helpers
	if !strings.Contains(src, "type UserPublic struct") {
		t.Error("missing UserPublic struct")
	}
	if !strings.Contains(src, "func (m *User) Public() UserPublic") {
		t.Error("missing Public() method")
	}
	if !strings.Contains(src, "func PublicUsers(") {
		t.Error("missing PublicUsers helper")
	}
}

func TestGenerateModelNoHiddenFields(t *testing.T) {
	tbl := &schema.Table{Name: "posts"}
	tbl.UUID("id").PrimaryKey()
	tbl.String("title").NotNull()
	tbl.Timestamps()

	out, err := GenerateModel(tbl, "models")
	if err != nil {
		t.Fatalf("GenerateModel: %v", err)
	}

	src := string(out)
	if strings.Contains(src, "PostPublic") {
		t.Error("no hidden fields → no Public struct needed")
	}
}

// ─── helpers.go ──────────────────────────────────────────────────────────────

func TestToLowerFirst(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"User", "user"},
		{"Post", "post"},
		{"", ""},
		{"a", "a"},
		{"ABC", "aBC"},
	}
	for _, tt := range tests {
		got := toLowerFirst(tt.in)
		if got != tt.want {
			t.Errorf("toLowerFirst(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ─── auth_generator.go (WriteDriverMigrations) ───────────────────────────────

func TestWriteDriverMigrationsJWT(t *testing.T) {
	tmp := t.TempDir()

	err := WriteDriverMigrations("jwt", tmp, "migrations")
	if err != nil {
		t.Fatalf("WriteDriverMigrations: %v", err)
	}

	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Error("expected at least one migration file written")
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), "_gen.go") {
			t.Errorf("expected _gen.go file, got %s", e.Name())
		}
	}
}

func TestWriteDriverMigrationsUserOverride(t *testing.T) {
	tmp := t.TempDir()

	// Create a user override file — the _gen.go should be skipped
	os.WriteFile(filepath.Join(tmp, "2026_03_03_100000_create_jwt_tokens_table.go"), []byte("package migrations\n"), 0o644)

	err := WriteDriverMigrations("jwt", tmp, "migrations")
	if err != nil {
		t.Fatalf("WriteDriverMigrations: %v", err)
	}

	// The _gen.go should NOT have been created since user override exists
	if _, err := os.Stat(filepath.Join(tmp, "2026_03_03_100000_create_jwt_tokens_table_gen.go")); err == nil {
		t.Error("_gen.go should not be created when user override exists")
	}
}

func TestWriteDriverMigrationsUnknownDriver(t *testing.T) {
	tmp := t.TempDir()
	// Unknown driver should be a no-op, not an error
	err := WriteDriverMigrations("unknown_driver", tmp, "migrations")
	if err != nil {
		t.Errorf("WriteDriverMigrations with unknown driver should not error, got: %v", err)
	}
}

// ─── schema_inspector.go ─────────────────────────────────────────────────────

func TestGenerateSchemaInspectorMultiplePackages(t *testing.T) {
	migrations := []MigrationEntry{
		{StructName: "CreateUsersTable_2026_02_21_100000", ImportPath: "myapp/database/migrations"},
		{StructName: "CreateSessionsTable_2026_02_21_100001", ImportPath: "myapp/database/migrations/auth"},
	}

	out, err := GenerateSchemaInspector(migrations)
	if err != nil {
		t.Fatalf("GenerateSchemaInspector: %v", err)
	}

	src := string(out)

	// Should have imports for both packages
	if !strings.Contains(src, `"myapp/database/migrations"`) {
		t.Error("missing first import")
	}
	if !strings.Contains(src, `"myapp/database/migrations/auth"`) {
		t.Error("missing second import")
	}
}
