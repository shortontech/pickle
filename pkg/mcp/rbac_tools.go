package picklemcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shortontech/pickle/pkg/generator"
	"github.com/shortontech/pickle/pkg/schema"
)

var (
	reControllerAction = regexp.MustCompile(`ControllerAction\("([^"]+)"`)
	reRemoveAction     = regexp.MustCompile(`RemoveAction\("([^"]+)"`)
	reExpose           = regexp.MustCompile(`\.?Expose\("([^"]+)"`)
	reUnexpose         = regexp.MustCompile(`Unexpose\("([^"]+)"\)`)
)

// GraphQLModel represents an exposed model with its operations.
type GraphQLModel struct {
	Model      string
	Operations []string
}

// GraphQLAction represents a controller action exposed as a GraphQL mutation.
type GraphQLAction struct {
	Name string
}

// RBACState holds the derived RBAC state for MCP tools.
type RBACState struct {
	Roles         []generator.DerivedRole
	Policies      []generator.StaticPolicyOps
	GraphQLModels []GraphQLModel
	GraphQLActions []GraphQLAction
}

// DeriveRBACState returns the current RBAC state by scanning policy files
// in the project directory using AST-based parsing.
func DeriveRBACState(projectDir string) *RBACState {
	state := &RBACState{}

	policiesDir := filepath.Join(projectDir, "database", "policies")

	// Parse role policies via AST
	policies, err := generator.ParsePolicyOps(policiesDir)
	if err == nil && len(policies) > 0 {
		state.Policies = policies
		state.Roles = generator.StaticDeriveRoles(policies)
	}

	// Scan GraphQL policies (regex-based since they use closures the AST parser doesn't handle)
	graphqlDir := filepath.Join(policiesDir, "graphql")
	state.GraphQLModels = scanGraphQLPolicies(graphqlDir)
	state.GraphQLActions = scanGraphQLActions(graphqlDir)

	return state
}

// scanGraphQLPolicies parses GraphQL policy files to extract exposed models.
func scanGraphQLPolicies(dir string) []GraphQLModel {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	exposures := map[string]*GraphQLModel{}
	var order []string

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		src := string(data)

		// Extract Expose("Model", ...) calls
		for _, match := range reExpose.FindAllStringSubmatch(src, -1) {
			model := match[1]
			if exposures[model] == nil {
				exposures[model] = &GraphQLModel{Model: model}
				order = append(order, model)
			}
		}

		// Extract operation calls
		for _, op := range []string{"List", "Show", "Create", "Update", "Delete", "All"} {
			if strings.Contains(src, "e."+op+"()") || strings.Contains(src, "."+op+"()") {
				for _, match := range reExpose.FindAllStringSubmatch(src, -1) {
					model := match[1]
					if m := exposures[model]; m != nil {
						if op == "All" {
							m.Operations = addUnique(m.Operations, "list", "show", "create", "update", "delete")
						} else {
							m.Operations = addUnique(m.Operations, strings.ToLower(op))
						}
					}
				}
			}
		}

		// Extract Unexpose calls
		for _, match := range reUnexpose.FindAllStringSubmatch(src, -1) {
			model := match[1]
			delete(exposures, model)
			for i, m := range order {
				if m == model {
					order = append(order[:i], order[i+1:]...)
					break
				}
			}
		}
	}

	var result []GraphQLModel
	for _, model := range order {
		if m, ok := exposures[model]; ok {
			result = append(result, *m)
		}
	}
	return result
}

// scanGraphQLActions parses GraphQL policy files to extract controller actions.
func scanGraphQLActions(dir string) []GraphQLAction {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	actions := map[string]bool{}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		src := string(data)

		for _, match := range reControllerAction.FindAllStringSubmatch(src, -1) {
			actions[match[1]] = true
		}
		for _, match := range reRemoveAction.FindAllStringSubmatch(src, -1) {
			delete(actions, match[1])
		}
	}

	var result []GraphQLAction
	var names []string
	for name := range actions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		result = append(result, GraphQLAction{Name: name})
	}
	return result
}

func addUnique(slice []string, items ...string) []string {
	seen := map[string]bool{}
	for _, s := range slice {
		seen[s] = true
	}
	for _, item := range items {
		if !seen[item] {
			slice = append(slice, item)
			seen[item] = true
		}
	}
	return slice
}

func (s *Server) registerRBACTools() {
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "roles_list",
		Description: "List all RBAC roles with slug, display name, manages flag, default flag, and birth policy.",
	}, s.rolesList)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "roles_show",
		Description: "Show a single RBAC role with permissions, column visibility per table, and action grants. Pass a role slug.",
	}, s.rolesShow)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "roles_history",
		Description: "Show the full policy changelog: which policies were applied and what role operations they performed.",
	}, s.rolesHistory)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "graphql_list",
		Description: "List exposed GraphQL models with their operations.",
	}, s.graphqlList)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "graphql_actions",
		Description: "List controller actions exposed as GraphQL mutations.",
	}, s.graphqlActions)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "graphql_schema",
		Description: "Show the current generated GraphQL SDL schema.",
	}, s.graphqlSchema)
}

func (s *Server) rolesList(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	state := DeriveRBACState(s.project.Dir)

	if len(state.Roles) == 0 {
		return textResult("No roles defined. Create a policy with `make:policy` to get started."), nil, nil
	}

	var b strings.Builder
	for _, role := range state.Roles {
		fmt.Fprintf(&b, "## %s\n", role.Slug)
		if role.DisplayName != "" {
			fmt.Fprintf(&b, "  Name: %s\n", role.DisplayName)
		}
		if role.IsManages {
			b.WriteString("  Manages: true\n")
		}
		if role.IsDefault {
			b.WriteString("  Default: true\n")
		}
		if role.BirthTimestamp != "" {
			fmt.Fprintf(&b, "  Birth Policy: %s\n", role.BirthTimestamp)
		}
		if len(role.Actions) > 0 {
			fmt.Fprintf(&b, "  Actions: %s\n", strings.Join(role.Actions, ", "))
		}
	}
	return textResult(b.String()), nil, nil
}

type roleInput struct {
	Slug string `json:"slug"`
}

func (s *Server) rolesShow(_ context.Context, _ *mcp.CallToolRequest, input roleInput) (*mcp.CallToolResult, any, error) {
	if input.Slug == "" {
		return errResult("slug is required"), nil, nil
	}

	state := DeriveRBACState(s.project.Dir)

	for _, role := range state.Roles {
		if role.Slug == input.Slug {
			var b strings.Builder
			fmt.Fprintf(&b, "## %s\n", role.Slug)
			if role.DisplayName != "" {
				fmt.Fprintf(&b, "Name: %s\n", role.DisplayName)
			}
			if role.IsManages {
				b.WriteString("Manages: true\n")
			}
			if role.IsDefault {
				b.WriteString("Default: true\n")
			}
			if role.BirthTimestamp != "" {
				fmt.Fprintf(&b, "Birth Policy: %s\n", role.BirthTimestamp)
			}

			// Actions
			if len(role.Actions) > 0 {
				b.WriteString("Actions:\n")
				for _, a := range role.Actions {
					fmt.Fprintf(&b, "  - %s\n", a)
				}
			} else {
				b.WriteString("Actions: (none)\n")
			}

			// Column visibility per table
			visibility := s.columnVisibilityForRole(role.Slug)
			if len(visibility) > 0 {
				b.WriteString("Tables:\n")
				var tableNames []string
				for tbl := range visibility {
					tableNames = append(tableNames, tbl)
				}
				sort.Strings(tableNames)
				for _, tbl := range tableNames {
					fmt.Fprintf(&b, "  %s: [%s]\n", tbl, strings.Join(visibility[tbl], ", "))
				}
			}

			return textResult(b.String()), nil, nil
		}
	}

	return errResult(fmt.Sprintf("role %q not found", input.Slug)), nil, nil
}

// columnVisibilityForRole returns columns visible to a role, per table.
// For manages roles, all columns are visible. For non-manages roles,
// only columns with VisibleTo containing the role slug (or public/PK columns) are included.
func (s *Server) columnVisibilityForRole(slug string) map[string][]string {
	tables, _, _, err := generator.RunSchemaInspector(s.project)
	if err != nil {
		return nil
	}

	// Check if the role is a manages role
	state := DeriveRBACState(s.project.Dir)
	isManages := false
	for _, r := range state.Roles {
		if r.Slug == slug && r.IsManages {
			isManages = true
			break
		}
	}

	result := map[string][]string{}
	for _, tbl := range tables {
		var cols []string
		for _, col := range tbl.Columns {
			if isManages {
				// Manages roles see all columns
				cols = append(cols, col.Name)
			} else if col.IsPrimaryKey || col.IsPublic {
				cols = append(cols, col.Name)
			} else if col.VisibleTo != nil && col.VisibleTo[slug] {
				cols = append(cols, col.Name)
			}
		}
		if len(cols) > 0 {
			result[tbl.Name] = cols
		}
	}
	return result
}

func (s *Server) rolesHistory(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	state := DeriveRBACState(s.project.Dir)

	if len(state.Policies) == 0 {
		return textResult("No policies found."), nil, nil
	}

	var b strings.Builder
	for _, policy := range state.Policies {
		fmt.Fprintf(&b, "## %s\n", policy.PolicyID)
		fmt.Fprintf(&b, "  Source: %s\n", policy.SourceFile)
		for _, op := range policy.Ops {
			switch op.Type {
			case "create":
				fmt.Fprintf(&b, "  + CreateRole(%q)", op.Slug)
				if op.DisplayName != "" {
					fmt.Fprintf(&b, " Name(%q)", op.DisplayName)
				}
				if op.IsManages {
					b.WriteString(" Manages()")
				}
				if op.IsDefault {
					b.WriteString(" Default()")
				}
				if len(op.Actions) > 0 {
					fmt.Fprintf(&b, " Can(%s)", strings.Join(op.Actions, ", "))
				}
				b.WriteString("\n")
			case "alter":
				fmt.Fprintf(&b, "  ~ AlterRole(%q)", op.Slug)
				if op.DisplayName != "" {
					fmt.Fprintf(&b, " Name(%q)", op.DisplayName)
				}
				if op.IsManages {
					b.WriteString(" Manages()")
				}
				if op.RemoveManages {
					b.WriteString(" RemoveManages()")
				}
				if op.IsDefault {
					b.WriteString(" Default()")
				}
				if op.RemoveDefault {
					b.WriteString(" RemoveDefault()")
				}
				if len(op.Actions) > 0 {
					fmt.Fprintf(&b, " Can(%s)", strings.Join(op.Actions, ", "))
				}
				if len(op.RevokeActions) > 0 {
					fmt.Fprintf(&b, " RevokeCan(%s)", strings.Join(op.RevokeActions, ", "))
				}
				b.WriteString("\n")
			case "drop":
				fmt.Fprintf(&b, "  - DropRole(%q)\n", op.Slug)
			}
		}
	}
	return textResult(b.String()), nil, nil
}

func (s *Server) graphqlList(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	state := DeriveRBACState(s.project.Dir)

	if len(state.GraphQLModels) == 0 {
		return textResult("No GraphQL models exposed. Create a GraphQL policy with `make:graphql-policy` to get started."), nil, nil
	}

	var b strings.Builder
	for _, m := range state.GraphQLModels {
		fmt.Fprintf(&b, "## %s\n", m.Model)
		if len(m.Operations) > 0 {
			fmt.Fprintf(&b, "  Operations: %s\n", strings.Join(m.Operations, ", "))
		}
	}
	return textResult(b.String()), nil, nil
}

func (s *Server) graphqlActions(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	state := DeriveRBACState(s.project.Dir)

	if len(state.GraphQLActions) == 0 {
		return textResult("No GraphQL controller actions defined."), nil, nil
	}

	var b strings.Builder
	for _, a := range state.GraphQLActions {
		fmt.Fprintf(&b, "- %s\n", a.Name)
	}
	return textResult(b.String()), nil, nil
}

func (s *Server) graphqlSchema(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	tables, _, relationships, err := generator.RunSchemaInspector(s.project)
	if err != nil {
		return errResult("schema inspection failed: " + err.Error()), nil, nil
	}

	requests, err := generator.ScanRequests(s.project.Layout.RequestsDir)
	if err != nil {
		return errResult("scanning requests: " + err.Error()), nil, nil
	}

	sdl := generator.BuildSDL(tables, relationships, requests)
	return textResult(sdl), nil, nil
}

// enhanceSchemaWithVisibility adds visible_to annotations and GraphQL exposure to schema output.
func enhanceSchemaWithVisibility(tbl *schema.Table, graphqlModels []GraphQLModel) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n", tbl.Name)

	// GraphQL exposure status
	var graphqlOps []string
	for _, m := range graphqlModels {
		if m.Model == tbl.Name {
			graphqlOps = m.Operations
			break
		}
	}
	if len(graphqlOps) > 0 {
		fmt.Fprintf(&b, "  GraphQL: exposed (%s)\n", strings.Join(graphqlOps, ", "))
	}

	for _, c := range tbl.Columns {
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

		// visible_to roles
		if len(c.VisibleTo) > 0 {
			var roles []string
			for slug := range c.VisibleTo {
				roles = append(roles, slug)
			}
			sort.Strings(roles)
			attrs = append(attrs, fmt.Sprintf("VISIBLE_TO(%s)", strings.Join(roles, ", ")))
		}

		attrStr := ""
		if len(attrs) > 0 {
			attrStr = " [" + strings.Join(attrs, ", ") + "]"
		}
		fmt.Fprintf(&b, "  %s %s%s\n", c.Name, c.Type, attrStr)
	}
	return b.String()
}
