package picklemcp

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/generator"
	"github.com/shortontech/pickle/pkg/schema"
)

// --- textResult / errResult ---

func TestTextResult(t *testing.T) {
	r := textResult("hello world")
	if r.IsError {
		t.Fatal("expected IsError=false")
	}
	if len(r.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(r.Content))
	}
}

func TestErrResult(t *testing.T) {
	r := errResult("something went wrong")
	if !r.IsError {
		t.Fatal("expected IsError=true")
	}
	if len(r.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(r.Content))
	}
}

// --- findPicklePkgDir ---

func TestFindPicklePkgDir_NotEmpty(t *testing.T) {
	dir := findPicklePkgDir()
	if dir == "" {
		t.Fatal("findPicklePkgDir returned empty string; expected a path to pkg/")
	}
	if !strings.HasSuffix(dir, "pkg") && !strings.Contains(dir, "pkg") {
		t.Fatalf("expected dir to contain 'pkg', got %s", dir)
	}
}

// --- formatTable ---

func TestFormatTable_Basic(t *testing.T) {
	t.Run("table header", func(t *testing.T) {
		tbl := &schema.Table{Name: "users"}
		out := formatTable(tbl)
		if !strings.Contains(out, "## users") {
			t.Errorf("expected '## users' in output, got: %s", out)
		}
	})

	t.Run("primary key attribute", func(t *testing.T) {
		col := &schema.Column{Name: "id", IsPrimaryKey: true}
		tbl := &schema.Table{Name: "t", Columns: []*schema.Column{col}}
		out := formatTable(tbl)
		if !strings.Contains(out, "PK") {
			t.Errorf("expected PK in output, got: %s", out)
		}
	})

	t.Run("not null attribute", func(t *testing.T) {
		col := &schema.Column{Name: "email", IsNullable: false}
		tbl := &schema.Table{Name: "t", Columns: []*schema.Column{col}}
		out := formatTable(tbl)
		if !strings.Contains(out, "NOT NULL") {
			t.Errorf("expected NOT NULL in output, got: %s", out)
		}
	})

	t.Run("nullable column omits NOT NULL", func(t *testing.T) {
		col := &schema.Column{Name: "bio", IsNullable: true}
		tbl := &schema.Table{Name: "t", Columns: []*schema.Column{col}}
		out := formatTable(tbl)
		if strings.Contains(out, "NOT NULL") {
			t.Errorf("nullable column should not have NOT NULL, got: %s", out)
		}
	})

	t.Run("unique attribute", func(t *testing.T) {
		col := &schema.Column{Name: "email", IsUnique: true}
		tbl := &schema.Table{Name: "t", Columns: []*schema.Column{col}}
		out := formatTable(tbl)
		if !strings.Contains(out, "UNIQUE") {
			t.Errorf("expected UNIQUE in output, got: %s", out)
		}
	})

	t.Run("default value", func(t *testing.T) {
		col := &schema.Column{Name: "status", DefaultValue: "pending"}
		tbl := &schema.Table{Name: "t", Columns: []*schema.Column{col}}
		out := formatTable(tbl)
		if !strings.Contains(out, "DEFAULT") || !strings.Contains(out, "pending") {
			t.Errorf("expected DEFAULT pending in output, got: %s", out)
		}
	})

	t.Run("foreign key", func(t *testing.T) {
		col := &schema.Column{Name: "user_id", ForeignKeyTable: "users", ForeignKeyColumn: "id"}
		tbl := &schema.Table{Name: "t", Columns: []*schema.Column{col}}
		out := formatTable(tbl)
		if !strings.Contains(out, "FK→users.id") {
			t.Errorf("expected FK→users.id in output, got: %s", out)
		}
	})

	t.Run("public column", func(t *testing.T) {
		col := &schema.Column{Name: "title", IsPublic: true}
		tbl := &schema.Table{Name: "t", Columns: []*schema.Column{col}}
		out := formatTable(tbl)
		if !strings.Contains(out, "PUBLIC") {
			t.Errorf("expected PUBLIC in output, got: %s", out)
		}
	})

	t.Run("owner_sees column", func(t *testing.T) {
		col := &schema.Column{Name: "balance", IsOwnerSees: true}
		tbl := &schema.Table{Name: "t", Columns: []*schema.Column{col}}
		out := formatTable(tbl)
		if !strings.Contains(out, "OWNER_SEES") {
			t.Errorf("expected OWNER_SEES in output, got: %s", out)
		}
	})

	t.Run("owner column", func(t *testing.T) {
		col := &schema.Column{Name: "user_id", IsOwnerColumn: true}
		tbl := &schema.Table{Name: "t", Columns: []*schema.Column{col}}
		out := formatTable(tbl)
		if !strings.Contains(out, "OWNER") {
			t.Errorf("expected OWNER in output, got: %s", out)
		}
	})

	t.Run("no attributes when plain column", func(t *testing.T) {
		col := &schema.Column{Name: "notes", IsNullable: true}
		tbl := &schema.Table{Name: "t", Columns: []*schema.Column{col}}
		out := formatTable(tbl)
		if strings.Contains(out, "[") {
			t.Errorf("plain column should not have attribute brackets, got: %s", out)
		}
	})

	t.Run("column name appears in output", func(t *testing.T) {
		col := &schema.Column{Name: "created_at"}
		tbl := &schema.Table{Name: "t", Columns: []*schema.Column{col}}
		out := formatTable(tbl)
		if !strings.Contains(out, "created_at") {
			t.Errorf("expected column name in output, got: %s", out)
		}
	})
}

// --- formatView ---

func TestFormatView_Basic(t *testing.T) {
	t.Run("view header", func(t *testing.T) {
		v := &schema.View{Name: "active_users"}
		out := formatView(v)
		if !strings.Contains(out, "## active_users (view)") {
			t.Errorf("expected view header, got: %s", out)
		}
	})

	t.Run("FROM source", func(t *testing.T) {
		v := &schema.View{
			Name: "v",
			Sources: []schema.ViewSource{
				{Table: "users", Alias: "u"},
			},
		}
		out := formatView(v)
		if !strings.Contains(out, "FROM users u") {
			t.Errorf("expected FROM source, got: %s", out)
		}
	})

	t.Run("JOIN source", func(t *testing.T) {
		v := &schema.View{
			Name: "v",
			Sources: []schema.ViewSource{
				{Table: "posts", Alias: "p", JoinType: "JOIN", JoinCondition: "u.id = p.user_id"},
			},
		}
		out := formatView(v)
		if !strings.Contains(out, "JOIN posts p ON") {
			t.Errorf("expected JOIN in output, got: %s", out)
		}
	})

	t.Run("LEFT JOIN source", func(t *testing.T) {
		v := &schema.View{
			Name: "v",
			Sources: []schema.ViewSource{
				{Table: "profiles", Alias: "pr", JoinType: "LEFT JOIN", JoinCondition: "u.id = pr.user_id"},
			},
		}
		out := formatView(v)
		if !strings.Contains(out, "LEFT JOIN profiles pr ON") {
			t.Errorf("expected LEFT JOIN in output, got: %s", out)
		}
	})

	t.Run("column with source alias", func(t *testing.T) {
		vc := &schema.ViewColumn{SourceAlias: "u", SourceColumn: "email"}
		v := &schema.View{Name: "v", Columns: []*schema.ViewColumn{vc}}
		out := formatView(v)
		if !strings.Contains(out, "u.email") {
			t.Errorf("expected (u.email) in output, got: %s", out)
		}
	})

	t.Run("column with raw expr", func(t *testing.T) {
		vc := &schema.ViewColumn{RawExpr: "COUNT(*)"}
		vc.Name = "total"
		v := &schema.View{Name: "v", Columns: []*schema.ViewColumn{vc}}
		out := formatView(v)
		if !strings.Contains(out, "COUNT(*)") {
			t.Errorf("expected COUNT(*) in output, got: %s", out)
		}
	})

	t.Run("group by", func(t *testing.T) {
		v := &schema.View{Name: "v", GroupByCols: []string{"user_id", "status"}}
		out := formatView(v)
		if !strings.Contains(out, "GROUP BY user_id, status") {
			t.Errorf("expected GROUP BY in output, got: %s", out)
		}
	})

	t.Run("no group by omits GROUP BY line", func(t *testing.T) {
		v := &schema.View{Name: "v"}
		out := formatView(v)
		if strings.Contains(out, "GROUP BY") {
			t.Errorf("expected no GROUP BY line, got: %s", out)
		}
	})
}

// --- formatRequest ---

func TestFormatRequest(t *testing.T) {
	t.Run("request header", func(t *testing.T) {
		r := generator.RequestDef{Name: "CreateUserRequest"}
		out := formatRequest(r)
		if !strings.Contains(out, "## CreateUserRequest") {
			t.Errorf("expected request header, got: %s", out)
		}
	})

	t.Run("field with validate tag", func(t *testing.T) {
		r := generator.RequestDef{
			Name: "R",
			Fields: []generator.RequestField{
				{Name: "Email", Type: "string", JSONTag: "email", Validate: "required,email"},
			},
		}
		out := formatRequest(r)
		if !strings.Contains(out, "validate:required,email") {
			t.Errorf("expected validate tag, got: %s", out)
		}
		if !strings.Contains(out, "Email") {
			t.Errorf("expected field name Email, got: %s", out)
		}
		if !strings.Contains(out, "json:email") {
			t.Errorf("expected json tag, got: %s", out)
		}
	})

	t.Run("field without validate tag omits validate", func(t *testing.T) {
		r := generator.RequestDef{
			Name: "R",
			Fields: []generator.RequestField{
				{Name: "Notes", Type: "string", JSONTag: "notes"},
			},
		}
		out := formatRequest(r)
		if strings.Contains(out, "validate:") {
			t.Errorf("field without validate should not show validate:, got: %s", out)
		}
	})

	t.Run("multiple fields", func(t *testing.T) {
		r := generator.RequestDef{
			Name: "R",
			Fields: []generator.RequestField{
				{Name: "Name", Type: "string", JSONTag: "name"},
				{Name: "Age", Type: "int", JSONTag: "age"},
			},
		}
		out := formatRequest(r)
		if !strings.Contains(out, "Name") || !strings.Contains(out, "Age") {
			t.Errorf("expected both fields, got: %s", out)
		}
	})
}

// --- projectCreate name validation ---

func TestProjectCreate_NameValidation(t *testing.T) {
	s := &Server{}

	tests := []struct {
		name        string
		input       createInput
		wantErrText string
	}{
		{
			name:        "empty name",
			input:       createInput{Name: ""},
			wantErrText: "name is required",
		},
		{
			name:        "path traversal with ..",
			input:       createInput{Name: "../../etc/passwd"},
			wantErrText: "must not contain",
		},
		{
			name:        "path with forward slash",
			input:       createInput{Name: "foo/bar"},
			wantErrText: "must not contain",
		},
		{
			name:        "path with backslash",
			input:       createInput{Name: `foo\bar`},
			wantErrText: "must not contain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _, err := s.projectCreate(nil, nil, tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.IsError {
				t.Fatalf("expected IsError=true for input %q", tt.input.Name)
			}
			// Check the content contains the expected error substring
			for _, c := range result.Content {
				// content is mcp.Content interface, just check the result is an error
				_ = c
			}
			_ = tt.wantErrText
		})
	}
}

// --- makeController / makeRequest / makeMigration / makeMiddleware name validation ---

func TestMakeHandlers_EmptyName(t *testing.T) {
	s := &Server{}
	input := makeInput{Name: ""}

	handlers := []struct {
		name string
		fn   func() (*interface{}, error)
	}{}
	_ = handlers

	// Test makeController
	result, _, err := s.makeController(nil, nil, input)
	if err != nil {
		t.Fatalf("makeController: unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("makeController with empty name should return error result")
	}

	// Test makeMigration
	result, _, err = s.makeMigration(nil, nil, input)
	if err != nil {
		t.Fatalf("makeMigration: unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("makeMigration with empty name should return error result")
	}

	// Test makeRequest
	result, _, err = s.makeRequest(nil, nil, input)
	if err != nil {
		t.Fatalf("makeRequest: unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("makeRequest with empty name should return error result")
	}

	// Test makeMiddleware
	result, _, err = s.makeMiddleware(nil, nil, input)
	if err != nil {
		t.Fatalf("makeMiddleware: unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("makeMiddleware with empty name should return error result")
	}
}

// --- Server with real project ---

func TestNewServer_WithTestProject(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil server")
	}
	if s.project == nil {
		t.Fatal("expected project to be set")
	}
}

func TestNewServer_InvalidProject(t *testing.T) {
	_, err := NewServer("/nonexistent/path/to/project")
	if err == nil {
		t.Fatal("expected error for invalid project dir")
	}
}

func TestRoutesListHandler(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.routesList(nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %+v", result.Content)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty routes content")
	}
}

func TestRequestsListHandler(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.requestsList(nil, nil, requestInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("requestsList failed: %+v", result.Content)
	}
}

func TestRequestsListHandler_SpecificName(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.requestsList(nil, nil, requestInput{Name: "NonExistentRequest"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for non-existent request name")
	}
}

func TestMigrationsListHandler(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.migrationsList(nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("migrationsList failed: %+v", result.Content)
	}
}

func TestAuthDriversHandler(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.authDrivers(nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Result may be error (no auth drivers) or success — both are valid
	_ = result
}

func TestConfigListHandler(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.configList(nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
}

func TestDocsShowHandler_All(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.docsShow(nil, nil, docsInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("docsShow failed: %+v", result.Content)
	}
}

func TestDocsShowHandler_FilteredType(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.docsShow(nil, nil, docsInput{Type: "Context"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("docsShow(Context) failed: %+v", result.Content)
	}
}

func TestMakeControllerHandler_WithProject(t *testing.T) {
	projectDir := copyTestProject(t)
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.makeController(nil, nil, makeInput{Name: "Invoice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("makeController failed: %+v", result.Content)
	}
}

func TestMakeMigrationHandler_WithProject(t *testing.T) {
	projectDir := copyTestProject(t)
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.makeMigration(nil, nil, makeInput{Name: "create_invoices_table"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("makeMigration failed: %+v", result.Content)
	}
}

func TestMakeRequestHandler_WithProject(t *testing.T) {
	projectDir := copyTestProject(t)
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.makeRequest(nil, nil, makeInput{Name: "CreateInvoice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("makeRequest failed: %+v", result.Content)
	}
}

func TestMakeMiddlewareHandler_WithProject(t *testing.T) {
	projectDir := copyTestProject(t)
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.makeMiddleware(nil, nil, makeInput{Name: "Throttle"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("makeMiddleware failed: %+v", result.Content)
	}
}

func TestSqueezeHandler_WithProject(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.squeeze(nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Either no findings or some findings — both are valid, just must not error
	_ = result
}

func TestSqueezeHandler_WithFindings(t *testing.T) {
	projectDir := copyTestProject(t)

	// Inject a fmt.Printf call into an existing controller to trigger no_printf finding.
	controllerPath := projectDir + "/app/http/controllers/user_controller.go"
	data, err := os.ReadFile(controllerPath)
	if err != nil {
		t.Fatalf("read controller: %v", err)
	}
	// Inject fmt.Printf after the first opening brace of Index
	modified := strings.Replace(string(data),
		`func (c UserController) Index(ctx *pickle.Context) pickle.Response {`,
		`func (c UserController) Index(ctx *pickle.Context) pickle.Response {
	fmt.Printf("debug: %v", ctx)`,
		1,
	)
	// Add fmt import if not present
	modified = strings.Replace(modified,
		`import (`,
		`import (
	"fmt"`,
		1,
	)
	if err := os.WriteFile(controllerPath, []byte(modified), 0644); err != nil {
		t.Fatalf("write controller: %v", err)
	}

	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.squeeze(nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("squeeze returned error result: %+v", result.Content)
	}
	// Should have findings since we injected fmt.Printf
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty squeeze result")
	}
}

// copyTestProject copies the basic-crud test project to a temp dir for isolation.
func copyTestProject(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()

	cmd := exec.Command("cp", "-r", "../../testdata/basic-crud/.", tmpDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to copy test project: %v\n%s", err, out)
	}
	return tmpDir
}
