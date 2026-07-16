package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/generator"
)

func TestHelpRequested(t *testing.T) {
	for _, args := range [][]string{
		{"--help"},
		{"-h"},
		{"--project", "/tmp/dill", "--help"},
	} {
		if !helpRequested(args) {
			t.Errorf("helpRequested(%q) = false, want true", args)
		}
	}

	if helpRequested([]string{"CRMSeeder", "--dry-run"}) {
		t.Error("ordinary db:seed arguments requested help")
	}
}

func TestRunRowPolicyCommandsUseNormalizedPolicyModel(t *testing.T) {
	project := rowPolicyCLIProject(t)
	for _, tc := range []struct {
		command string
		args    []string
		wants   []string
	}{
		{"policies:rows", []string{"--project", project}, []string{"## users", "Required identities: user_id", "Rule IDs: owner"}},
		{"policies:row", []string{"users", "--project", project}, []string{"Table: users", "Identity: user_id (uuid)", "select={equal(column(id), identity(user_id))}"}},
		{"policies:explain", []string{"users", "select", "authenticated", "--project", project}, []string{"Operation: select", "Rule owner (authenticated)", "PostgreSQL RLS: equivalent generated policy"}},
	} {
		t.Run(tc.command, func(t *testing.T) {
			var out bytes.Buffer
			if err := runRowPolicyCommand(tc.command, tc.args, &out); err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.wants {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output missing %q:\n%s", want, out.String())
				}
			}
		})
	}
}

func TestRunRowPolicyCommandRejectsUnknownTable(t *testing.T) {
	var out bytes.Buffer
	err := runRowPolicyCommand("policies:row", []string{"accounts", "--project", rowPolicyCLIProject(t)}, &out)
	if err == nil || !strings.Contains(err.Error(), "no row policy protects table accounts") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func rowPolicyCLIProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":      "module example.com/policycli\n\ngo 1.24\n",
		"pickle.yaml": "app:\n  name: policy-cli\n",
		"database/migrations/2026_07_16_000000_create_users.go": `package migrations
type CreateUsers_2026_07_16_000000 struct{ Migration }
func (m *CreateUsers_2026_07_16_000000) Up(){ m.CreateTable("users", func(t *Table){ t.UUID("id").PrimaryKey() }) }
func (m *CreateUsers_2026_07_16_000000) Down(){ m.DropTableIfExists("users") }
`,
		"database/policies/2026_07_16_000001_protect_users.go": `package policies
type ProtectUsers_2026_07_16_000001 struct{ Policy }
func (p *ProtectUsers_2026_07_16_000001) Up(){
 p.IdentityUUID("user_id")
 p.Protect("users", func(rows *Rows){ rows.Rule("owner").ForAuthenticated().Select(Owner("id", Identity("user_id"))) })
}
`,
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"app/http/requests", "app/http/controllers", "app/http/middleware", "routes"} {
		if err := os.MkdirAll(filepath.Join(dir, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	project, err := generator.DetectProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := generator.Generate(project, findPicklePkgDir()); err != nil {
		t.Fatal(err)
	}
	return dir
}
