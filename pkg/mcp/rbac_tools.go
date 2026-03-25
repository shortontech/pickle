package picklemcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

// DeriveRBACState returns the current RBAC state by scanning policy files.
// This is a stub that returns empty state — the real implementation will
// replay policy definitions to build derived state.
func DeriveRBACState(projectDir string) *RBACState {
	return &RBACState{}
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
