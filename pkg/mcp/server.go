package picklemcp

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shortontech/pickle/pkg/generator"
	"github.com/shortontech/pickle/pkg/schema"
)

// Server wraps the MCP server with Pickle project context.
type Server struct {
	project *generator.Project
	server  *mcp.Server
}

// NewServer creates a Pickle MCP server with all tools registered.
func NewServer(projectDir string) (*Server, error) {
	project, err := generator.DetectProject(projectDir)
	if err != nil {
		return nil, fmt.Errorf("detecting project: %w", err)
	}

	s := &Server{project: project}
	s.server = mcp.NewServer(&mcp.Implementation{
		Name:    "pickle",
		Version: "v0.1.0",
	}, nil)

	s.registerTools()
	return s, nil
}

// Run starts the MCP server on stdio transport.
func (s *Server) Run(ctx context.Context) error {
	return s.server.Run(ctx, &mcp.StdioTransport{})
}

func (s *Server) registerTools() {
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "schema_show",
		Description: "Show database schema. Pass a table name to show a specific table, or omit for all tables.",
	}, s.schemaShow)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "routes_list",
		Description: "Show all API routes defined in routes/web.go.",
	}, s.routesList)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "requests_list",
		Description: "List all request classes with their fields and validation rules. Pass a name to show a specific request.",
	}, s.requestsList)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "migrations_list",
		Description: "List all migrations in order.",
	}, s.migrationsList)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "auth_drivers",
		Description: "List configured auth drivers.",
	}, s.authDrivers)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "config_list",
		Description: "Show application config structure.",
	}, s.configList)
}

type tableInput struct {
	Table string `json:"table,omitempty"`
}

func (s *Server) schemaShow(_ context.Context, _ *mcp.CallToolRequest, input tableInput) (*mcp.CallToolResult, any, error) {
	tables, err := generator.RunSchemaInspector(s.project)
	if err != nil {
		return errResult("schema inspection failed: " + err.Error()), nil, nil
	}

	if input.Table != "" {
		for _, t := range tables {
			if t.Name == input.Table {
				return textResult(formatTable(t)), nil, nil
			}
		}
		return errResult(fmt.Sprintf("table %q not found", input.Table)), nil, nil
	}

	var b strings.Builder
	for i, t := range tables {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(formatTable(t))
	}
	return textResult(b.String()), nil, nil
}

func (s *Server) routesList(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	routesFile := s.project.Dir + "/routes/web.go"
	data, err := os.ReadFile(routesFile)
	if err != nil {
		return errResult("could not read routes/web.go: " + err.Error()), nil, nil
	}
	return textResult(string(data)), nil, nil
}

type requestInput struct {
	Name string `json:"name,omitempty"`
}

func (s *Server) requestsList(_ context.Context, _ *mcp.CallToolRequest, input requestInput) (*mcp.CallToolResult, any, error) {
	requests, err := generator.ScanRequests(s.project.Layout.RequestsDir)
	if err != nil {
		return errResult("scanning requests: " + err.Error()), nil, nil
	}

	if input.Name != "" {
		for _, r := range requests {
			if r.Name == input.Name {
				return textResult(formatRequest(r)), nil, nil
			}
		}
		return errResult(fmt.Sprintf("request %q not found", input.Name)), nil, nil
	}

	var b strings.Builder
	for i, r := range requests {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(formatRequest(r))
	}
	return textResult(b.String()), nil, nil
}

func (s *Server) migrationsList(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	migrations, err := generator.ScanMigrationFiles(s.project.Layout.MigrationsDir)
	if err != nil {
		return errResult("scanning migrations: " + err.Error()), nil, nil
	}

	var b strings.Builder
	for _, m := range migrations {
		b.WriteString(m.ID)
		b.WriteString("\n")
	}
	return textResult(b.String()), nil, nil
}

func (s *Server) authDrivers(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	drivers, err := generator.ScanAuthDrivers(s.project.Layout.AuthDir)
	if err != nil {
		return errResult("scanning auth drivers: " + err.Error()), nil, nil
	}

	var b strings.Builder
	for _, d := range drivers {
		kind := "custom"
		if d.IsBuiltin {
			kind = "builtin"
		}
		fmt.Fprintf(&b, "%s (%s)\n", d.Name, kind)
	}
	return textResult(b.String()), nil, nil
}

func (s *Server) configList(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	result, err := generator.ScanConfigs(s.project.Layout.ConfigDir)
	if err != nil {
		return errResult("scanning config: " + err.Error()), nil, nil
	}

	var b strings.Builder
	for _, c := range result.Configs {
		fmt.Fprintf(&b, "%s → %s\n", c.VarName, c.ReturnType)
	}
	return textResult(b.String()), nil, nil
}

// --- formatting helpers ---

func formatTable(t *schema.Table) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n", t.Name)
	for _, c := range t.Columns {
		var attrs []string
		if c.IsPrimaryKey {
			attrs = append(attrs, "PK")
		}
		if !c.IsNullable {
			attrs = append(attrs, "NOT NULL")
		}
		if c.IsUnique {
			attrs = append(attrs, "UNIQUE")
		}
		if c.DefaultValue != nil {
			attrs = append(attrs, fmt.Sprintf("DEFAULT %v", c.DefaultValue))
		}
		if c.ForeignKeyTable != "" {
			attrs = append(attrs, fmt.Sprintf("FK→%s.%s", c.ForeignKeyTable, c.ForeignKeyColumn))
		}

		attrStr := ""
		if len(attrs) > 0 {
			attrStr = " [" + strings.Join(attrs, ", ") + "]"
		}
		fmt.Fprintf(&b, "  %s %s%s\n", c.Name, c.Type, attrStr)
	}
	return b.String()
}

func formatRequest(r generator.RequestDef) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n", r.Name)
	for _, f := range r.Fields {
		validate := ""
		if f.Validate != "" {
			validate = " validate:" + f.Validate
		}
		fmt.Fprintf(&b, "  %s %s (json:%s%s)\n", f.Name, f.Type, f.JSONTag, validate)
	}
	return b.String()
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

func errResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "Error: " + msg}},
		IsError: true,
	}
}
