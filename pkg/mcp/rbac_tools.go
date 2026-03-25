package picklemcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	reCreateRole = regexp.MustCompile(`CreateRole\("([^"]+)"\)`)
	reDropRole   = regexp.MustCompile(`DropRole\("([^"]+)"\)`)
	reName       = regexp.MustCompile(`\.Name\("([^"]+)"\)`)
	reCan        = regexp.MustCompile(`\.Can\(([^)]+)\)`)
	reExpose     = regexp.MustCompile(`\.?Expose\("([^"]+)"`)
	reUnexpose   = regexp.MustCompile(`Unexpose\("([^"]+)"\)`)
)

// RBACRole represents a derived role for MCP display.
type RBACRole struct {
	Slug        string
	Name        string
	Permissions []string
}

// GraphQLModel represents an exposed model with its operations.
type GraphQLModel struct {
	Model      string
	Operations []string
}

// RBACState holds the derived RBAC state for MCP tools.
// Since the MCP server may not have DB access, this works off
// replayed policy definitions rather than live database queries.
type RBACState struct {
	Roles         []RBACRole
	GraphQLModels []GraphQLModel
}

// DeriveRBACState returns the current RBAC state by scanning policy files
// and GraphQL policy files in the project directory. Parses Go source to
// extract CreateRole/AlterRole/DropRole calls and Expose/Unexpose calls.
func DeriveRBACState(projectDir string) *RBACState {
	state := &RBACState{}

	// Scan role policies
	policiesDir := filepath.Join(projectDir, "database", "policies")
	state.Roles = scanRolePolicies(policiesDir)

	// Scan GraphQL policies
	graphqlDir := filepath.Join(policiesDir, "graphql")
	state.GraphQLModels = scanGraphQLPolicies(graphqlDir)

	return state
}

// scanRolePolicies parses policy Go files to extract role definitions.
// Uses simple string matching on source — not AST — since policy files
// have //go:build ignore and can't be compiled.
func scanRolePolicies(dir string) []RBACRole {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	roles := map[string]*RBACRole{}
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

		// Extract CreateRole calls: CreateRole("slug")
		for _, match := range reCreateRole.FindAllStringSubmatch(src, -1) {
			slug := match[1]
			if roles[slug] == nil {
				roles[slug] = &RBACRole{Slug: slug}
				order = append(order, slug)
			}
		}

		// Extract .Name("display") chains
		for _, match := range reName.FindAllStringSubmatch(src, -1) {
			// Find the nearest preceding CreateRole or AlterRole
			// Simple heuristic: apply to last seen role in this file
			if len(order) > 0 {
				lastSlug := order[len(order)-1]
				if r := roles[lastSlug]; r != nil && r.Name == "" {
					r.Name = match[1]
				}
			}
		}

		// Extract .Can("action1", "action2") chains
		for _, match := range reCan.FindAllStringSubmatch(src, -1) {
			actions := strings.Split(match[1], `", "`)
			for i := range actions {
				actions[i] = strings.Trim(actions[i], `"`)
			}
			if len(order) > 0 {
				lastSlug := order[len(order)-1]
				if r := roles[lastSlug]; r != nil {
					r.Permissions = append(r.Permissions, actions...)
				}
			}
		}

		// Extract DropRole calls
		for _, match := range reDropRole.FindAllStringSubmatch(src, -1) {
			slug := match[1]
			delete(roles, slug)
			for i, s := range order {
				if s == slug {
					order = append(order[:i], order[i+1:]...)
					break
				}
			}
		}
	}

	var result []RBACRole
	for _, slug := range order {
		if r, ok := roles[slug]; ok {
			result = append(result, *r)
		}
	}
	return result
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

		// Extract operation calls: e.List(), e.Show(), etc.
		for _, op := range []string{"List", "Show", "Create", "Update", "Delete", "All"} {
			if strings.Contains(src, "e."+op+"()") || strings.Contains(src, "."+op+"()") {
				// Apply to any model exposed in this file
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

		// Extract Unexpose("Model") calls
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
		Description: "List all RBAC roles derived from policy definitions.",
	}, s.rolesList)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "roles_show",
		Description: "Show a single RBAC role with its permissions and visibility info. Pass a role slug.",
	}, s.rolesShow)

	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "graphql_list",
		Description: "List exposed GraphQL models with their operations.",
	}, s.graphqlList)
}

func (s *Server) rolesList(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	state := DeriveRBACState(s.project.Dir)

	if len(state.Roles) == 0 {
		return textResult("No roles defined. Create a policy with `make:policy` to get started."), nil, nil
	}

	var b strings.Builder
	for _, role := range state.Roles {
		fmt.Fprintf(&b, "## %s\n", role.Slug)
		if role.Name != "" {
			fmt.Fprintf(&b, "  Name: %s\n", role.Name)
		}
		if len(role.Permissions) > 0 {
			fmt.Fprintf(&b, "  Permissions: %s\n", strings.Join(role.Permissions, ", "))
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
			if role.Name != "" {
				fmt.Fprintf(&b, "Name: %s\n", role.Name)
			}
			if len(role.Permissions) > 0 {
				fmt.Fprintf(&b, "Permissions:\n")
				for _, p := range role.Permissions {
					fmt.Fprintf(&b, "  - %s\n", p)
				}
			} else {
				fmt.Fprintf(&b, "Permissions: (none)\n")
			}
			return textResult(b.String()), nil, nil
		}
	}

	return errResult(fmt.Sprintf("role %q not found", input.Slug)), nil, nil
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
