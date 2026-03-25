package generator

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// ActionDef describes a user-defined action parsed from database/actions/.
type ActionDef struct {
	Name       string // PascalCase action name, e.g. "Ban"
	StructName string // e.g. "BanAction"
	SourceFile string // relative path
	HasResult  bool   // true if Execute returns (*ResultStruct, error) instead of error
	ResultType string // e.g. "*TransferResult" (empty if HasResult is false)
}

// GateDef describes a gate function for an action.
type GateDef struct {
	Name       string // e.g. "CanBan"
	ActionName string // e.g. "Ban"
	SourceFile string
	IsGenerated bool  // true if from _gate_gen.go
}

// ActionSet groups actions and gates for a model.
type ActionSet struct {
	Model   string // directory name / model name
	Actions []ActionDef
	Gates   []GateDef
}

// ScanActions scans database/actions/{model}/ for action and gate files.
func ScanActions(actionsDir string) (map[string]*ActionSet, error) {
	result := map[string]*ActionSet{}

	entries, err := os.ReadDir(actionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		modelDir := entry.Name()
		modelPath := filepath.Join(actionsDir, modelDir)

		actionSet, err := parseActionDir(modelPath, modelDir)
		if err != nil {
			return nil, fmt.Errorf("parsing actions for %s: %w", modelDir, err)
		}
		if len(actionSet.Actions) > 0 || len(actionSet.Gates) > 0 {
			result[modelDir] = actionSet
		}
	}

	return result, nil
}

func parseActionDir(dir, modelDir string) (*ActionSet, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		return nil, err
	}

	set := &ActionSet{Model: modelDir}

	for _, pkg := range pkgs {
		for filename, file := range pkg.Files {
			baseName := filepath.Base(filename)
			relPath := filepath.Join("database/actions", modelDir, baseName)

			isGate := strings.HasSuffix(baseName, "_gate.go") || strings.HasSuffix(baseName, "_gate_gen.go")

			if isGate {
				// Parse gate functions
				for _, decl := range file.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok || fn.Recv != nil || !fn.Name.IsExported() {
						continue
					}
					if !strings.HasPrefix(fn.Name.Name, "Can") {
						continue
					}
					actionName := fn.Name.Name[3:] // "CanBan" → "Ban"
					set.Gates = append(set.Gates, GateDef{
						Name:        fn.Name.Name,
						ActionName:  actionName,
						SourceFile:  relPath,
						IsGenerated: strings.HasSuffix(baseName, "_gen.go"),
					})
				}
			} else {
				// Parse action structs with a method matching their name.
				// BanAction must have a Ban() method, PublishAction must have Publish(), etc.
				for _, decl := range file.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok || fn.Recv == nil || !fn.Name.IsExported() {
						continue
					}
					structName := actionReceiverTypeName(fn.Recv)
					if structName == "" || !strings.HasSuffix(structName, "Action") {
						continue
					}
					actionName := strings.TrimSuffix(structName, "Action")
					// The method name must match the action name
					if fn.Name.Name != actionName {
						continue
					}

					action := ActionDef{
						Name:       actionName,
						StructName: structName,
						SourceFile: relPath,
					}

					// Check return type for result struct
					if fn.Type.Results != nil && len(fn.Type.Results.List) == 2 {
						action.HasResult = true
						action.ResultType = exprToString(fn.Type.Results.List[0].Type)
					}

					set.Actions = append(set.Actions, action)
				}
			}
		}
	}

	return set, nil
}

func actionReceiverTypeName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	switch t := recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if ident, ok := t.X.(*ast.Ident); ok {
			return ident.Name
		}
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// ValidateActions checks that every action has a corresponding gate.
// Returns an error listing all ungated actions.
func ValidateActions(set *ActionSet) error {
	gateIndex := map[string]bool{}
	for _, g := range set.Gates {
		gateIndex[g.ActionName] = true
	}

	var missing []string
	for _, a := range set.Actions {
		if !gateIndex[a.Name] {
			missing = append(missing, a.Name)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("actions without gates in %s: %s (create %s_gate.go for each)",
			set.Model, strings.Join(missing, ", "),
			strings.ToLower(missing[0]))
	}
	return nil
}

// GenerateActionWiring produces a Go source file with gated action methods on the model struct.
func GenerateActionWiring(set *ActionSet, packageName, actionImportPath string) ([]byte, error) {
	if len(set.Actions) == 0 && len(set.Gates) == 0 {
		return nil, nil
	}

	structName := tableToStructName(set.Model + "s") // model dir is singular, table is plural
	// Actually, for actions, the dir name IS the model name (singular-ish)
	// Let's use the dir name directly with PascalCase
	structName = tableToStructName(set.Model)

	var buf bytes.Buffer
	buf.WriteString("// Code generated by Pickle. DO NOT EDIT.\n")
	buf.WriteString(fmt.Sprintf("package %s\n\n", packageName))

	needsImport := len(set.Actions) > 0
	if needsImport {
		buf.WriteString(fmt.Sprintf("import actions %q\n\n", actionImportPath))
	}

	// Build gate lookup for source comments
	gateFiles := map[string]string{}
	for _, g := range set.Gates {
		gateFiles[g.ActionName] = g.SourceFile
	}

	for _, action := range set.Actions {
		gateFile := gateFiles[action.Name]

		buf.WriteString(fmt.Sprintf("// %s — action source: %s\n", action.Name, action.SourceFile))
		if gateFile != "" {
			buf.WriteString(fmt.Sprintf("//         gate source:   %s\n", gateFile))
		}

		if action.HasResult {
			buf.WriteString(fmt.Sprintf("func (m *%s) %s(ctx *Context, action actions.%s) (%s, error) {\n",
				structName, action.Name, action.StructName, action.ResultType))
			buf.WriteString(fmt.Sprintf("\troleID := actions.Can%s(ctx, m)\n", action.Name))
			buf.WriteString("\tif roleID == nil {\n")
			buf.WriteString(fmt.Sprintf("\t\treturn nil, ErrUnauthorized\n"))
			buf.WriteString("\t}\n")
			buf.WriteString(fmt.Sprintf("\treturn action.%s(ctx, m)\n", toLowerFirst(action.Name)))
		} else {
			buf.WriteString(fmt.Sprintf("func (m *%s) %s(ctx *Context, action actions.%s) error {\n",
				structName, action.Name, action.StructName))
			buf.WriteString(fmt.Sprintf("\troleID := actions.Can%s(ctx, m)\n", action.Name))
			buf.WriteString("\tif roleID == nil {\n")
			buf.WriteString("\t\treturn ErrUnauthorized\n")
			buf.WriteString("\t}\n")
			buf.WriteString(fmt.Sprintf("\treturn action.%s(ctx, m)\n", toLowerFirst(action.Name)))
		}
		buf.WriteString("}\n\n")
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return buf.Bytes(), fmt.Errorf("formatting action wiring for %s: %w\n%s", set.Model, err, buf.String())
	}
	return formatted, nil
}
