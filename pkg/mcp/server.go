package picklemcp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shortontech/pickle/pkg/generator"
	"github.com/shortontech/pickle/pkg/scaffold"
	"github.com/shortontech/pickle/pkg/schema"
	"github.com/shortontech/pickle/pkg/squeeze"
)

// Server wraps the MCP server with Pickle project context.
type Server struct {
	project      *generator.Project
	server       *mcp.Server
	picklePkgDir string
}

// NewServer creates a Pickle MCP server with all tools registered.
func NewServer(projectDir string) (*Server, error) {
	project, err := generator.DetectProject(projectDir)
	if err != nil {
		return nil, fmt.Errorf("detecting project: %w", err)
	}

	s := &Server{project: project, picklePkgDir: findPicklePkgDir()}
	s.server = mcp.NewServer(&mcp.Implementation{
		Name:    "pickle",
		Version: "v0.2.0",
	}, nil)

	s.registerTools()
	return s, nil
}

// Run starts the MCP server on stdio transport.
func (s *Server) Run(ctx context.Context) error {
	return s.server.Run(ctx, &mcp.StdioTransport{})
}

// RunHTTP starts the MCP server as a Streamable HTTP server on the given address.
func (s *Server) RunHTTP(addr string) error {
	handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return s.server
	}, nil)

	fmt.Fprintf(os.Stderr, "pickle mcp: listening on %s\n", addr)
	return http.ListenAndServe(addr, handler)
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
		Name:        "seeders_list",
		Description: "List compiled root scenarios and row seeders without executing them.",
	}, s.seedersList)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "seeders_show",
		Description: "Show one seeder's kind, source, repeat policy, and redacted graph declarations.",
	}, s.seedersShow)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "seeders_plan",
		Description: "Build a read-only static seed plan with graph counts, relationships, identities, and field providers. Never inserts rows.",
	}, s.seedersPlan)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "auth_drivers",
		Description: "List configured auth drivers.",
	}, s.authDrivers)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "config_list",
		Description: "Show application config structure.",
	}, s.configList)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "docs_show",
		Description: "Show Pickle framework API documentation. Pass a type name to filter (e.g. Context, Router, Response, QueryBuilder).",
	}, s.docsShow)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "make_controller",
		Description: "Scaffold a new controller. Pass a name like 'User' or 'UserController'.",
	}, s.makeController)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "make_migration",
		Description: "Scaffold a new migration. Pass a name like 'create_posts_table'.",
	}, s.makeMigration)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "make_request",
		Description: "Scaffold a new request class. Pass a name like 'CreateUser' or 'CreateUserRequest'.",
	}, s.makeRequest)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "make_middleware",
		Description: "Scaffold a new middleware. Pass a name like 'RateLimit'.",
	}, s.makeMiddleware)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "project_create",
		Description: "Create a new Pickle project. Scaffolds the full directory structure, generates code, and runs go mod tidy. The name is used as both the directory name and Go module path.",
	}, s.projectCreate)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "generate",
		Description: "Run Pickle code generation. Reads migrations, requests, config, and auth drivers, then generates models, query scopes, request bindings, and all framework glue code. Run this after changing migrations, requests, or config files.",
	}, s.generate)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "squeeze",
		Description: "Run static analysis on the project. Returns security and correctness findings that generic linters miss: ownership scoping, enum validation, UUID error handling, public projections, required fields, and printf usage in controllers.",
	}, s.squeeze)

	s.registerRBACTools()
}

type tableInput struct {
	Table string `json:"table,omitempty"`
}

func (s *Server) schemaShow(_ context.Context, _ *mcp.CallToolRequest, input tableInput) (*mcp.CallToolResult, any, error) {
	tables, views, _, err := generator.RunSchemaInspector(s.project)
	if err != nil {
		return errResult("schema inspection failed: " + err.Error()), nil, nil
	}

	// Get GraphQL models for exposure annotations
	rbacState := DeriveRBACState(s.project.Dir)

	if input.Table != "" {
		for _, t := range tables {
			if t.Name == input.Table {
				return textResult(enhanceSchemaWithVisibility(t, rbacState.GraphQLModels)), nil, nil
			}
		}
		for _, v := range views {
			if v.Name == input.Table {
				return textResult(formatView(v)), nil, nil
			}
		}
		return errResult(fmt.Sprintf("table or view %q not found", input.Table)), nil, nil
	}

	var b strings.Builder
	for i, t := range tables {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(enhanceSchemaWithVisibility(t, rbacState.GraphQLModels))
	}
	for _, v := range views {
		b.WriteString("\n")
		b.WriteString(formatView(v))
	}
	return textResult(b.String()), nil, nil
}

func (s *Server) routesList(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	analysis, err := squeeze.Analyze(s.project.Dir)
	if err != nil {
		return errResult("could not analyze routes: " + err.Error()), nil, nil
	}
	return textResult(formatRoutes(analysis.Routes, analysis.Methods, analysis.Requests)), nil, nil
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

type seederInput struct {
	Name string `json:"name,omitempty"`
	Seed int64  `json:"seed,omitempty"`
}

func (s *Server) scanSeeders() ([]generator.SeederDefinition, error) {
	return generator.ScanSeeders(filepath.Join(s.project.Dir, "database", "seeders"))
}

func (s *Server) seedersList(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	definitions, err := s.scanSeeders()
	if err != nil {
		return errResult("scanning seeders: " + err.Error()), nil, nil
	}
	if len(definitions) == 0 {
		return textResult("No seeders defined."), nil, nil
	}
	var out strings.Builder
	for _, definition := range definitions {
		fmt.Fprintf(&out, "%s (%s)", definition.Name, definition.Kind)
		if definition.Table != "" {
			fmt.Fprintf(&out, " -> %s", definition.Table)
		}
		if definition.Policy != "" {
			fmt.Fprintf(&out, " policy:%s", definition.Policy)
		}
		out.WriteByte('\n')
	}
	return textResult(out.String()), nil, nil
}

func (s *Server) seedersShow(_ context.Context, _ *mcp.CallToolRequest, input seederInput) (*mcp.CallToolResult, any, error) {
	if input.Name == "" {
		return errResult("name is required"), nil, nil
	}
	definitions, err := s.scanSeeders()
	if err != nil {
		return errResult("scanning seeders: " + err.Error()), nil, nil
	}
	for _, definition := range definitions {
		if definition.Name == input.Name {
			return textResult(formatSeederDefinition(definition, s.project.Dir)), nil, nil
		}
	}
	return errResult(fmt.Sprintf("seeder %q not found", input.Name)), nil, nil
}

func (s *Server) seedersPlan(_ context.Context, _ *mcp.CallToolRequest, input seederInput) (*mcp.CallToolResult, any, error) {
	if input.Name == "" {
		return errResult("name is required"), nil, nil
	}
	definitions, err := s.scanSeeders()
	if err != nil {
		return errResult("scanning seeders: " + err.Error()), nil, nil
	}
	var scenario *generator.SeederDefinition
	for index := range definitions {
		if definitions[index].Name == input.Name && definitions[index].Kind == "scenario" {
			scenario = &definitions[index]
			break
		}
	}
	if scenario == nil {
		return errResult(fmt.Sprintf("root seed scenario %q not found", input.Name)), nil, nil
	}
	tables, _, _, err := generator.RunSchemaInspector(s.project)
	if err != nil {
		return errResult("schema inspection failed: " + err.Error()), nil, nil
	}
	return textResult(formatSeederPlan(*scenario, tables, input.Seed, s.project.Dir)), nil, nil
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

type docsInput struct {
	Type string `json:"type,omitempty"`
}

func (s *Server) docsShow(_ context.Context, _ *mcp.CallToolRequest, input docsInput) (*mcp.CallToolResult, any, error) {
	result, err := generator.FormatDocsMarkdown(input.Type)
	if err != nil {
		return errResult(err.Error()), nil, nil
	}
	return textResult(result), nil, nil
}

type createInput struct {
	Name   string `json:"name"`
	Module string `json:"module,omitempty"`
}

func (s *Server) projectCreate(_ context.Context, _ *mcp.CallToolRequest, input createInput) (*mcp.CallToolResult, any, error) {
	if input.Name == "" {
		return errResult("name is required"), nil, nil
	}

	// Validate project name: must be a simple name, not a path
	if strings.Contains(input.Name, "..") || strings.ContainsAny(input.Name, "/\\") {
		return errResult(fmt.Sprintf("project name %q must not contain path separators or '..'", input.Name)), nil, nil
	}

	targetDir, err := filepath.Abs(input.Name)
	if err != nil {
		return errResult("invalid path: " + err.Error()), nil, nil
	}

	if _, err := os.Stat(targetDir); err == nil {
		return errResult(fmt.Sprintf("directory %q already exists", input.Name)), nil, nil
	}

	moduleName := input.Name
	if input.Module != "" {
		moduleName = input.Module
	}

	var log strings.Builder
	fmt.Fprintf(&log, "Creating project %q (module: %s)\n\n", input.Name, moduleName)

	if err := scaffold.Create(moduleName, targetDir); err != nil {
		return errResult("scaffold failed: " + err.Error()), nil, nil
	}
	log.WriteString("Scaffolded project structure.\n")

	// Generate code
	project, err := generator.DetectProject(targetDir)
	if err != nil {
		return errResult("detecting project: " + err.Error()), nil, nil
	}

	if err := generator.Generate(project, s.picklePkgDir); err != nil {
		fmt.Fprintf(&log, "Warning: generate failed: %v\n", err)
	} else {
		log.WriteString("Generated code.\n")
	}

	// Run go mod tidy
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = targetDir
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(&log, "Warning: go mod tidy failed: %v\n%s\n", err, out)
	} else {
		log.WriteString("Ran go mod tidy.\n")
	}

	fmt.Fprintf(&log, "\nProject created at %s\n", targetDir)
	return textResult(log.String()), nil, nil
}

type makeInput struct {
	Name string `json:"name"`
}

func (s *Server) makeController(_ context.Context, _ *mcp.CallToolRequest, input makeInput) (*mcp.CallToolResult, any, error) {
	if input.Name == "" {
		return errResult("name is required"), nil, nil
	}
	relPath, err := scaffold.MakeController(input.Name, s.project.Dir, s.project.ModulePath)
	if err != nil {
		return errResult(err.Error()), nil, nil
	}
	return textResult("Created " + relPath), nil, nil
}

func (s *Server) makeMigration(_ context.Context, _ *mcp.CallToolRequest, input makeInput) (*mcp.CallToolResult, any, error) {
	if input.Name == "" {
		return errResult("name is required"), nil, nil
	}
	relPath, err := scaffold.MakeMigration(input.Name, s.project.Dir)
	if err != nil {
		return errResult(err.Error()), nil, nil
	}
	return textResult("Created " + relPath), nil, nil
}

func (s *Server) makeRequest(_ context.Context, _ *mcp.CallToolRequest, input makeInput) (*mcp.CallToolResult, any, error) {
	if input.Name == "" {
		return errResult("name is required"), nil, nil
	}
	relPath, err := scaffold.MakeRequest(input.Name, s.project.Dir, s.project.ModulePath)
	if err != nil {
		return errResult(err.Error()), nil, nil
	}
	return textResult("Created " + relPath), nil, nil
}

func (s *Server) makeMiddleware(_ context.Context, _ *mcp.CallToolRequest, input makeInput) (*mcp.CallToolResult, any, error) {
	if input.Name == "" {
		return errResult("name is required"), nil, nil
	}
	relPath, err := scaffold.MakeMiddleware(input.Name, s.project.Dir, s.project.ModulePath)
	if err != nil {
		return errResult(err.Error()), nil, nil
	}
	return textResult("Created " + relPath), nil, nil
}

// findPicklePkgDir locates the pkg/ directory of the pickle installation.
func findPicklePkgDir() string {
	// Use runtime.Caller to find this source file's location:
	// thisFile = .../pkg/mcp/server.go → pkg/ = .../pkg/
	_, thisFile, _, ok := runtime.Caller(0)
	if ok {
		pkgDir := filepath.Join(filepath.Dir(thisFile), "..")
		if abs, err := filepath.Abs(pkgDir); err == nil {
			if _, err := os.Stat(filepath.Join(abs, "cooked")); err == nil {
				return abs
			}
		}
	}
	return ""
}

func (s *Server) generate(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	// Re-detect the project to pick up any changes since the server started.
	project, err := generator.DetectProject(s.project.Dir)
	if err != nil {
		return errResult("detecting project: " + err.Error()), nil, nil
	}
	s.project = project

	if err := generator.Generate(s.project, s.picklePkgDir); err != nil {
		return errResult("generate failed: " + err.Error()), nil, nil
	}
	return textResult("Generated successfully."), nil, nil
}

func (s *Server) squeeze(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	result, err := squeeze.RunWithOptions(s.project.Dir, squeeze.RunOptions{})
	if err != nil {
		return errResult("squeeze failed: " + err.Error()), nil, nil
	}
	findings := result.Findings

	if len(findings) == 0 {
		if result.Suppressed > 0 {
			return textResult(fmt.Sprintf("No findings.\nsuppressed: %d\n", result.Suppressed)), nil, nil
		}
		return textResult("No findings."), nil, nil
	}

	var b strings.Builder
	errors, warnings := 0, 0
	for _, f := range findings {
		if f.Severity == squeeze.SeverityError {
			errors++
		} else {
			warnings++
		}
		fmt.Fprintf(&b, "%s\n", f)
	}
	fmt.Fprintf(&b, "\nFound %d error(s), %d warning(s), suppressed: %d\n", errors, warnings, result.Suppressed)
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
		if c.IsPublic {
			attrs = append(attrs, "PUBLIC")
		}
		if c.IsOwnerSees {
			attrs = append(attrs, "OWNER_SEES")
		}
		if c.IsOwnerColumn {
			attrs = append(attrs, "OWNER")
		}
		if c.Seeder != nil {
			attrs = append(attrs, formatSeedSpec(c.Seeder))
		}

		attrStr := ""
		if len(attrs) > 0 {
			attrStr = " [" + strings.Join(attrs, ", ") + "]"
		}
		fmt.Fprintf(&b, "  %s %s%s\n", c.Name, c.Type, attrStr)
	}
	writeCompositeKeyTuples(&b, t)
	return b.String()
}

func formatSeedSpec(seed *schema.SeedSpec) string {
	if seed == nil {
		return ""
	}
	detail := seed.Kind
	if seed.Reference != "" {
		detail += ":" + seed.Reference
	} else if len(seed.Fields) > 0 {
		detail += ":" + strings.Join(seed.Fields, "+")
	} else if len(seed.Arguments) > 0 {
		detail += ":" + strings.Join(seed.Arguments, ",")
	}
	return "SEED(" + detail + ")"
}

func formatSeederDefinition(definition generator.SeederDefinition, projectDir string) string {
	var out strings.Builder
	fmt.Fprintf(&out, "## %s\n", definition.Name)
	fmt.Fprintf(&out, "kind: %s\n", definition.Kind)
	if definition.Table != "" {
		fmt.Fprintf(&out, "table: %s\n", definition.Table)
	}
	if definition.ReturnType != "" {
		fmt.Fprintf(&out, "returns: %s\n", definition.ReturnType)
	}
	policy := definition.Policy
	if policy == "" && definition.Kind == "scenario" {
		policy = "InsertOnly"
	}
	if policy != "" {
		fmt.Fprintf(&out, "policy: %s\n", policy)
	}
	if relative, err := filepath.Rel(projectDir, definition.File); err == nil {
		fmt.Fprintf(&out, "source: %s\n", relative)
	}
	if len(definition.GraphCalls) > 0 {
		out.WriteString("graph:\n")
		for _, call := range definition.GraphCalls {
			fmt.Fprintf(&out, "  line %d: %s(%s)\n", call.Line, call.Method, strings.Join(call.Arguments, ", "))
		}
	}
	return out.String()
}

func formatSeederPlan(definition generator.SeederDefinition, tables []*schema.Table, rootSeed int64, projectDir string) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# Seed plan: %s\n", definition.Name)
	if rootSeed == 0 {
		out.WriteString("root seed: generated and printed before execution\n")
	} else {
		fmt.Fprintf(&out, "root seed: %d\n", rootSeed)
	}
	policy := definition.Policy
	if policy == "" {
		policy = "InsertOnly"
	}
	fmt.Fprintf(&out, "policy: %s\ntransaction: one root scenario\nmutation: none (static plan)\n", policy)
	if relative, err := filepath.Rel(projectDir, definition.File); err == nil {
		fmt.Fprintf(&out, "source: %s\n", relative)
	}
	if len(definition.GraphCalls) > 0 {
		out.WriteString("\ngraph declarations (values redacted):\n")
		for _, call := range definition.GraphCalls {
			detail := strings.Join(call.Arguments, ", ")
			fmt.Fprintf(&out, "  %s(%s)\n", call.Method, detail)
		}
	}
	out.WriteString("\nfield providers:\n")
	providerCount := 0
	for _, table := range tables {
		for _, column := range table.Columns {
			if column.Seeder == nil || column.Seeder.Kind == "none" {
				continue
			}
			providerCount++
			detail := formatSeedSpec(column.Seeder)
			if column.Seeder.Kind == "password" {
				detail = "SEED(password:[REDACTED COMPOSITE])"
			}
			fmt.Fprintf(&out, "  %s.%s %s\n", table.Name, column.Name, detail)
		}
	}
	if providerCount == 0 {
		out.WriteString("  none\n")
	}
	out.WriteString("\nsafety: sensitive values are redacted; use the compiled db:seed --dry-run command for fully resolved row counts.\n")
	return out.String()
}

func formatRoutes(routes []squeeze.AnalyzedRoute, methods map[string]*squeeze.ControllerMethod, requests []generator.RequestDef) string {
	var b strings.Builder
	for _, route := range routes {
		fmt.Fprintf(&b, "%s %s -> %s.%s\n", route.Method, route.Path, route.ControllerType, route.MethodName)
		method := methods[route.ControllerType+"."+route.MethodName]
		if method == nil {
			continue
		}
		origins := squeeze.FindResourceIDOrigins(method.Body, requests)
		var params []string
		for name := range origins.Params {
			if strings.Contains(route.Path, ":"+name) {
				params = append(params, name)
			}
		}
		sort.Strings(params)
		for _, name := range params {
			fmt.Fprintf(&b, "  param %s ResourceID\n", name)
		}
		var requestVars []string
		for variable := range origins.RequestVar {
			requestVars = append(requestVars, variable)
		}
		sort.Strings(requestVars)
		for _, variable := range requestVars {
			var fields []string
			for field := range origins.RequestVar[variable] {
				fields = append(fields, field)
			}
			sort.Strings(fields)
			for _, field := range fields {
				fmt.Fprintf(&b, "  request %s.%s ResourceID\n", variable, field)
			}
		}
	}
	return b.String()
}

func formatView(v *schema.View) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s (view)\n", v.Name)

	// Sources
	for _, src := range v.Sources {
		if src.JoinType == "" {
			fmt.Fprintf(&b, "  FROM %s %s\n", src.Table, src.Alias)
		} else {
			fmt.Fprintf(&b, "  %s %s %s ON %s\n", src.JoinType, src.Table, src.Alias, src.JoinCondition)
		}
	}

	// Columns
	for _, c := range v.Columns {
		source := ""
		if c.RawExpr != "" {
			source = fmt.Sprintf(" = %s", c.RawExpr)
		} else if c.SourceAlias != "" {
			source = fmt.Sprintf(" (%s.%s)", c.SourceAlias, c.SourceColumn)
		}
		fmt.Fprintf(&b, "  %s %s%s\n", c.OutputName(), c.Type, source)
	}

	if len(v.GroupByCols) > 0 {
		fmt.Fprintf(&b, "  GROUP BY %s\n", strings.Join(v.GroupByCols, ", "))
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
