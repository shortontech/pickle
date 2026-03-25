package scaffold

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/shortontech/pickle/pkg/names"
)

// MakePolicy scaffolds a new policy file in database/policies/.
func MakePolicy(name, projectDir string) (string, error) {
	if err := sanitizeName(name); err != nil {
		return "", err
	}
	snake := names.PascalToSnake(name)
	if strings.Contains(name, "_") {
		snake = strings.ToLower(name)
	}
	ts := time.Now().Format("2006_01_02_150405")
	structName := names.SnakeToPascal(snake) + "Policy_" + ts
	fileName := ts + "_" + snake + ".go"
	relPath := filepath.Join("database", "policies", fileName)
	return writeScaffold(projectDir, relPath, tmplMakePolicy(structName))
}

// MakeAction scaffolds a new action + gate file.
// The name format is "model/action", e.g. "Post/publish".
func MakeAction(name, projectDir string) (string, error) {
	model, action, err := splitModelSlash(name, "action")
	if err != nil {
		return "", err
	}
	modelSnake := names.PascalToSnake(model)
	actionSnake := names.PascalToSnake(action)
	if strings.Contains(action, "_") {
		actionSnake = strings.ToLower(action)
	}
	fileName := actionSnake + ".go"
	relPath := filepath.Join("app", "actions", modelSnake, fileName)
	structName := names.SnakeToPascal(actionSnake) + "Action"
	return writeScaffold(projectDir, relPath, tmplMakeAction(structName, names.SnakeToPascal(modelSnake)))
}

// MakeScope scaffolds a new scope file.
// The name format is "model/scope", e.g. "Post/published".
func MakeScope(name, projectDir string) (string, error) {
	model, scope, err := splitModelSlash(name, "scope")
	if err != nil {
		return "", err
	}
	modelSnake := names.PascalToSnake(model)
	scopeSnake := names.PascalToSnake(scope)
	if strings.Contains(scope, "_") {
		scopeSnake = strings.ToLower(scope)
	}
	fileName := scopeSnake + ".go"
	relPath := filepath.Join("app", "scopes", modelSnake, fileName)
	funcName := names.SnakeToPascal(scopeSnake)
	return writeScaffold(projectDir, relPath, tmplMakeScope(funcName, names.SnakeToPascal(modelSnake)))
}

// MakeGraphQLPolicy scaffolds a new GraphQL policy file in database/policies/graphql/.
func MakeGraphQLPolicy(name, projectDir string) (string, error) {
	if err := sanitizeName(name); err != nil {
		return "", err
	}
	snake := names.PascalToSnake(name)
	if strings.Contains(name, "_") {
		snake = strings.ToLower(name)
	}
	ts := time.Now().Format("2006_01_02_150405")
	structName := names.SnakeToPascal(snake) + "GraphQLPolicy_" + ts
	fileName := ts + "_" + snake + ".go"
	relPath := filepath.Join("database", "policies", "graphql", fileName)
	return writeScaffold(projectDir, relPath, tmplMakeGraphQLPolicy(structName))
}

// splitModelSlash parses "Model/name" into (model, name).
func splitModelSlash(input, kind string) (string, string, error) {
	parts := strings.SplitN(input, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid %s name %q: expected format \"Model/%s\"", kind, input, kind)
	}
	if err := sanitizeName(parts[0]); err != nil {
		return "", "", err
	}
	if err := sanitizeName(parts[1]); err != nil {
		return "", "", err
	}
	return parts[0], parts[1], nil
}

func tmplMakePolicy(structName string) string {
	return fmt.Sprintf(`package policies

type %s struct {
	Policy
}

func (p *%s) Up() {
	// Define roles and permissions
	// p.Role("admin", func(r *RoleBuilder) {
	//     r.Can("users.create", "users.delete")
	// })
}

func (p *%s) Down() {
	// Reverse the policy
}
`, structName, structName, structName)
}

func tmplMakeAction(structName, modelName string) string {
	return fmt.Sprintf(`package actions

// %s defines the gate for the %s action on %s.
type %s struct{}

func (a %s) Authorize(userID string, %s interface{}) bool {
	// TODO: implement authorization logic
	return false
}

func (a %s) Handle(%s interface{}) error {
	// TODO: implement action logic
	return nil
}
`, structName, structName, modelName, structName, structName, strings.ToLower(modelName[:1])+modelName[1:], structName, strings.ToLower(modelName[:1])+modelName[1:])
}

func tmplMakeScope(funcName, modelName string) string {
	return fmt.Sprintf(`package scopes

// %s is a query scope for %s.
func %s() {
	// TODO: implement scope logic
	// Example: return q.Where("status", "=", "published")
}
`, funcName, modelName, funcName)
}

func tmplMakeGraphQLPolicy(structName string) string {
	return fmt.Sprintf(`package graphql

type %s struct {
	GraphQLPolicy
}

func (p *%s) Up() {
	// Expose models and operations via GraphQL
	// p.Expose("User", func(e *ExposeBuilder) {
	//     e.Operations("query", "create", "update")
	//     e.Roles("admin", "viewer")
	// })
}

func (p *%s) Down() {
	// Reverse the policy
}
`, structName, structName, structName)
}
