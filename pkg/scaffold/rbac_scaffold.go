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
	actionFileName := actionSnake + ".go"
	gateFileName := actionSnake + "_gate.go"
	actionsDir := filepath.Join("app", "actions", modelSnake)
	structName := names.SnakeToPascal(actionSnake) + "Action"
	modelPascal := names.SnakeToPascal(modelSnake)

	actionPath, err := writeScaffold(projectDir, filepath.Join(actionsDir, actionFileName), tmplMakeAction(structName, modelPascal))
	if err != nil {
		return "", err
	}

	gateFuncName := "Can" + names.SnakeToPascal(actionSnake)
	_, err = writeScaffold(projectDir, filepath.Join(actionsDir, gateFileName), tmplMakeGate(gateFuncName, modelPascal))
	if err != nil {
		return actionPath, err
	}

	return actionPath, nil
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
	// p.CreateRole("editor").Name("Editor").Can("posts.create", "posts.edit")
	// p.AlterRole("editor").RevokeCan("posts.delete")
	// p.DropRole("viewer")
}

func (p *%s) Down() {
	// Reverse the policy
}
`, structName, structName, structName)
}

func tmplMakeAction(structName, modelName string) string {
	modelParam := strings.ToLower(modelName[:1]) + modelName[1:]
	return fmt.Sprintf(`package actions

// %s performs the %s action on a %s.
type %s struct {
	// Add action-specific fields here
}

// Execute runs the action. The generator renames this to unexported execute()
// so it can only be called through the gated model method.
func (a %s) Execute(ctx *Context, %s *%s) error {
	// TODO: implement action logic
	return nil
}
`, structName, structName, modelName, structName, structName, modelParam, modelName)
}

func tmplMakeGate(funcName, modelName string) string {
	modelParam := strings.ToLower(modelName[:1]) + modelName[1:]
	return fmt.Sprintf(`package actions

import "github.com/google/uuid"

// %s returns the authorizing role ID, or nil if denied.
func %s(ctx *Context, %s *%s) *uuid.UUID {
	// TODO: implement authorization logic
	// Example: check ctx.HasAnyRole("admin", "moderator")
	return nil
}
`, funcName, funcName, modelParam, modelName)
}

func tmplMakeScope(funcName, modelName string) string {
	return fmt.Sprintf(`package scopes

// %s filters %s records.
// The first parameter is *models.%sScopeBuilder, return is *models.%sScopeBuilder.
func %s(q *%sScopeBuilder) *%sScopeBuilder {
	// TODO: add filters
	// Example: q.WhereStatus("published")
	return q
}
`, funcName, modelName, modelName, modelName, funcName, modelName, modelName)
}

func tmplMakeGraphQLPolicy(structName string) string {
	return fmt.Sprintf(`package graphql

type %s struct {
	GraphQLPolicy
}

func (p *%s) Up() {
	// p.Expose("User", func(e *ExposeBuilder) {
	//     e.List()
	//     e.Show()
	//     e.Create()
	//     e.Update()
	//     e.Delete()
	// })
	//
	// p.ControllerAction("banUser", controllers.UserController{}.Ban)
}

func (p *%s) Down() {
	// p.Unexpose("User")
	// p.RemoveAction("banUser")
}
`, structName, structName, structName)
}
