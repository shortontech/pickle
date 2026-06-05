package exporter

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/shortontech/pickle/pkg/generator"
	"github.com/shortontech/pickle/pkg/schema"
	"github.com/shortontech/pickle/pkg/squeeze"
)

// Options controls project export.
type Options struct {
	ProjectDir   string
	OutDir       string
	ORM          string
	Force        bool
	DryRun       bool
	ReportPath   string
	PicklePkgDir string
}

// Result describes an export run.
type Result struct {
	OutDir       string
	ReportPath   string
	FilesWritten int
	Findings     []Finding
}

// Finding records an export boundary that needs review or cannot be lowered.
type Finding struct {
	File    string
	Line    int
	Rule    string
	Message string
}

// Export lowers a Pickle project into a standalone Go project.
func Export(opts Options) (*Result, error) {
	if opts.ProjectDir == "" {
		opts.ProjectDir = "."
	}
	if opts.ORM == "" {
		opts.ORM = "gorm"
	}
	if opts.ORM != "gorm" {
		return nil, fmt.Errorf("unsupported orm %q", opts.ORM)
	}
	if opts.OutDir == "" {
		return nil, errors.New("--out is required")
	}

	project, err := generator.DetectProject(opts.ProjectDir)
	if err != nil {
		return nil, err
	}
	cfg, err := squeeze.LoadConfig(project.Dir)
	if err != nil {
		return nil, fmt.Errorf("loading pickle.yaml: %w", err)
	}
	if cfg.IsMonorepo() {
		return nil, errors.New("pickle export does not support multi-app monorepos yet")
	}
	if cfg.IsMultiService() {
		configureMultiServiceProject(project, cfg)
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return nil, err
	}
	if opts.ReportPath == "" {
		opts.ReportPath = filepath.Join(outDir, "EXPORT_REPORT.md")
	}

	res := &Result{OutDir: outDir, ReportPath: opts.ReportPath}
	ex := &exporter{
		project:      project,
		outDir:       outDir,
		modulePath:   exportModulePath(project.ModulePath),
		sourceModule: project.ModulePath,
		result:       res,
		dryRun:       opts.DryRun,
	}

	if !opts.DryRun {
		if err := ex.prepareOutDir(opts.Force); err != nil {
			return nil, err
		}
	}
	if opts.PicklePkgDir != "" {
		if err := generator.Generate(project, opts.PicklePkgDir); err != nil {
			return nil, fmt.Errorf("generate before export: %w", err)
		}
	}

	analysis, err := squeeze.Analyze(project.Dir)
	if err != nil {
		return nil, fmt.Errorf("analyze project: %w", err)
	}
	tables := analysis.Tables
	views := analysis.Views
	ex.migrations = analysis.Migrations
	ex.models = modelSet(tables)
	ex.hasEncryptedColumns = tablesHaveEncryptedColumns(tables)
	ex.integrityModels = integrityModelSet(tables)
	for _, view := range views {
		ex.models[tableToStruct(view.Name)] = true
	}

	if err := ex.writeGoMod(); err != nil {
		return nil, err
	}
	if err := ex.copyAndRewriteUserSource(); err != nil {
		return nil, err
	}
	if err := ex.writeHTTPX(); err != nil {
		return nil, err
	}
	if err := ex.writeModels(tables, views); err != nil {
		return nil, err
	}
	if err := ex.writeActions(); err != nil {
		return nil, err
	}
	if err := ex.writeGraphQLPackage(tables, views); err != nil {
		return nil, err
	}
	if err := ex.writeSQLMigrations(tables, views); err != nil {
		return nil, err
	}
	if err := ex.writePolicySupport(); err != nil {
		return nil, err
	}
	if err := ex.writeCommandsSupport(); err != nil {
		return nil, err
	}
	if err := ex.writeBindings(); err != nil {
		return nil, err
	}
	if err := ex.writeJobsSupport(); err != nil {
		return nil, err
	}
	if err := ex.writeConfigSupport(); err != nil {
		return nil, err
	}
	if err := ex.writeAuthSupport(); err != nil {
		return nil, err
	}
	if err := ex.writeServerMain(); err != nil {
		return nil, err
	}
	if err := ex.tidyModule(); err != nil {
		return nil, err
	}
	ex.addSchemaFindings(tables)
	if err := ex.writeReport(opts.ORM); err != nil {
		return nil, err
	}

	return res, nil
}

type exporter struct {
	project             *generator.Project
	outDir              string
	modulePath          string
	sourceModule        string
	result              *Result
	dryRun              bool
	models              map[string]bool
	migrations          []generator.MigrationOps
	sqlMigrations       []sqlMigration
	hasEncryptedColumns bool
	integrityModels     map[string]integrityModelInfo
}

type integrityModelInfo struct {
	Table       *schema.Table
	Immutable   bool
	AppendOnly  bool
	SoftDeletes bool
}

func configureMultiServiceProject(project *generator.Project, cfg *squeeze.Config) {
	project.Services = nil
	var names []string
	for name := range cfg.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		svc := cfg.Services[name]
		absDir := filepath.Join(project.Dir, svc.Dir)
		project.Services = append(project.Services, generator.ServiceLayout{
			Name:        name,
			Dir:         absDir,
			HTTPDir:     filepath.Join(absDir, "http"),
			HTTPPkg:     "pickle",
			RequestsDir: filepath.Join(absDir, "http", "requests"),
			CommandsDir: filepath.Join(absDir, "commands"),
		})
	}
}

type exportError struct {
	File    string
	Line    int
	Message string
}

func (e exportError) Error() string {
	return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Message)
}

func (e *exporter) prepareOutDir(force bool) error {
	entries, err := os.ReadDir(e.outDir)
	if err == nil && len(entries) > 0 && !force {
		return fmt.Errorf("output directory %q is not empty; use --force", e.outDir)
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(e.outDir, 0o755)
}

func (e *exporter) writeGoMod() error {
	var b strings.Builder
	fmt.Fprintf(&b, "module %s\n\n", e.modulePath)
	b.WriteString("go 1.24.0\n\n")
	b.WriteString("require (\n")
	if e.hasGraphQLPackage() {
		b.WriteString("\tgithub.com/99designs/gqlgen v0.17.90\n")
	}
	b.WriteString("\tgithub.com/go-playground/validator/v10 v10.30.1\n")
	b.WriteString("\tgithub.com/google/uuid v1.6.0\n")
	b.WriteString("\tgorm.io/driver/mysql v1.6.0\n")
	b.WriteString("\tgorm.io/driver/postgres v1.6.0\n")
	b.WriteString("\tgorm.io/driver/sqlite v1.6.0\n")
	b.WriteString("\tgorm.io/gorm v1.31.1\n")
	b.WriteString(")\n")
	content := b.String()
	return e.writeFile("go.mod", []byte(content))
}

func (e *exporter) tidyModule() error {
	if e.dryRun {
		return nil
	}
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = e.outDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("running go mod tidy in exported app: %w\n%s", err, out)
	}
	return nil
}

func (e *exporter) copyAndRewriteUserSource() error {
	return filepath.WalkDir(e.project.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(e.project.Dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if shouldSkipDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldSkipFile(rel) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".go") {
			data, err = e.rewriteGoFile(path, data)
			if err != nil {
				return err
			}
		}
		return e.writeFile(rel, data)
	})
}

func shouldSkipDir(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	skip := map[string]bool{
		".git":              true,
		".claude":           true,
		"app/models":        true,
		"app/graphql":       true,
		"app/commands":      true,
		"app/http/auth":     true,
		"database/policies": true,
		"database/actions":  true,
	}
	if skip[filepath.ToSlash(rel)] {
		return true
	}
	for _, p := range parts {
		if p == "vendor" || p == "node_modules" {
			return true
		}
	}
	return false
}

func shouldSkipFile(rel string) bool {
	base := filepath.Base(rel)
	if base == "go.mod" || base == "go.sum" {
		return true
	}
	if strings.HasSuffix(base, "_gen.go") || strings.HasSuffix(base, "_query.go") || strings.HasSuffix(base, "_test.go") {
		return true
	}
	if strings.HasPrefix(filepath.ToSlash(rel), "database/migrations/") && strings.HasSuffix(base, ".go") {
		return true
	}
	if filepath.ToSlash(rel) == "cmd/server/main.go" {
		return true
	}
	return false
}

func (e *exporter) rewriteGoFile(path string, data []byte) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, data, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	modelAlias := "models"
	var actionImportAliases []string
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if p == e.sourceModule+"/app/models" {
			if imp.Name != nil {
				modelAlias = imp.Name.Name
			}
			break
		}
	}
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		switch {
		case p == e.sourceModule+"/app/http":
			imp.Path.Value = strconv.Quote(e.modulePath + "/internal/httpx")
			imp.Name = ast.NewIdent("httpx")
		case strings.HasPrefix(p, e.sourceModule+"/database/actions/"):
			imp.Path.Value = strconv.Quote(e.modulePath + "/app/models")
			if imp.Name != nil {
				actionImportAliases = append(actionImportAliases, imp.Name.Name)
			} else {
				actionImportAliases = append(actionImportAliases, filepath.Base(p))
			}
			imp.Name = ast.NewIdent(modelAlias)
		case e.isServiceHTTPImport(p):
			imp.Path.Value = strconv.Quote(e.modulePath + "/internal/httpx")
			imp.Name = ast.NewIdent("httpx")
		case strings.HasPrefix(p, e.sourceModule+"/"):
			imp.Path.Value = strconv.Quote(e.modulePath + strings.TrimPrefix(p, e.sourceModule))
		}
	}

	ast.Inspect(f, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if s, err := strconv.Unquote(lit.Value); err == nil && strings.Contains(s, "PICKLE_") {
				lit.Value = strconv.Quote(strings.ReplaceAll(s, "PICKLE_", "APP_"))
			}
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == "pickle" {
			id.Name = "httpx"
		}
		for _, alias := range actionImportAliases {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == alias {
				id.Name = modelAlias
			}
		}
		return true
	})
	dedupeImports(f)

	if err := e.rewriteQueryStatements(path, fset, f); err != nil {
		return nil, err
	}
	if err := e.rejectRemainingPickleQueries(path, fset, f); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func dedupeImports(f *ast.File) {
	seen := map[string]bool{}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			continue
		}
		var specs []ast.Spec
		for _, spec := range gen.Specs {
			imp := spec.(*ast.ImportSpec)
			name := ""
			if imp.Name != nil {
				name = imp.Name.Name
			}
			key := name + "\x00" + imp.Path.Value
			if seen[key] {
				continue
			}
			seen[key] = true
			specs = append(specs, spec)
		}
		gen.Specs = specs
	}
}

func (e *exporter) isServiceHTTPImport(path string) bool {
	if e.project == nil {
		return false
	}
	for _, svc := range e.project.Services {
		rel, err := filepath.Rel(e.project.Dir, svc.HTTPDir)
		if err != nil {
			continue
		}
		if path == e.sourceModule+"/"+filepath.ToSlash(rel) {
			return true
		}
	}
	return false
}

func (e *exporter) rewriteQueryStatements(path string, fset *token.FileSet, f *ast.File) error {
	var firstErr error
	ast.Inspect(f, func(n ast.Node) bool {
		if firstErr != nil {
			return false
		}
		block, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		queryVars := map[string]string{}
		for i, stmt := range block.List {
			rewritten, err := e.rewriteStmt(path, fset, stmt, queryVars)
			if err != nil {
				firstErr = err
				return false
			}
			block.List[i] = rewritten
		}
		return true
	})
	return firstErr
}

func (e *exporter) rewriteStmt(path string, fset *token.FileSet, stmt ast.Stmt, queryVars map[string]string) (ast.Stmt, error) {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		if len(s.Rhs) == 1 {
			call, ok := s.Rhs[0].(*ast.CallExpr)
			if ok {
				if terminal, ok, err := parseQueryVarTerminal(call, queryVars); err != nil {
					return nil, exportError{File: path, Line: fset.Position(call.Pos()).Line, Message: err.Error()}
				} else if ok {
					expr, err := e.gormVarTerminalExpr(terminal)
					if err != nil {
						return nil, exportError{File: path, Line: fset.Position(call.Pos()).Line, Message: err.Error()}
					}
					s.Rhs[0] = expr
					return stmt, nil
				}

				if len(s.Lhs) == 1 {
					chain, chainOK, err := parseQueryBuilderChain(call)
					if err != nil {
						return nil, exportError{File: path, Line: fset.Position(call.Pos()).Line, Message: err.Error()}
					}
					if chainOK && chain.Terminal == "" {
						ident, identOK := s.Lhs[0].(*ast.Ident)
						if identOK {
							expr, err := e.gormBuilderExpr(chain)
							if err != nil {
								return nil, exportError{File: path, Line: fset.Position(call.Pos()).Line, Message: err.Error()}
							}
							s.Rhs[0] = expr
							queryVars[ident.Name] = chain.Model
							return stmt, nil
						}
					}
				}

				chain, ok, err := parseQueryChain(call)
				if err != nil {
					return nil, exportError{File: path, Line: fset.Position(call.Pos()).Line, Message: err.Error()}
				}
				if ok {
					expr, err := e.gormExpr(chain)
					if err != nil {
						return nil, exportError{File: path, Line: fset.Position(call.Pos()).Line, Message: err.Error()}
					}
					s.Rhs[0] = expr
				}
			}
		}
	case *ast.ExprStmt:
		call, ok := s.X.(*ast.CallExpr)
		if !ok {
			return stmt, nil
		}
		assign, ok, err := rewriteQueryVarMutation(call, queryVars)
		if err != nil {
			return nil, exportError{File: path, Line: fset.Position(call.Pos()).Line, Message: err.Error()}
		}
		if ok {
			return assign, nil
		}
	case *ast.ReturnStmt:
		for i, result := range s.Results {
			call, ok := result.(*ast.CallExpr)
			if !ok {
				continue
			}
			chain, chainOK, err := parseQueryChain(call)
			if err != nil {
				return nil, exportError{File: path, Line: fset.Position(call.Pos()).Line, Message: err.Error()}
			}
			if chainOK {
				expr, err := e.gormExpr(chain)
				if err != nil {
					return nil, exportError{File: path, Line: fset.Position(call.Pos()).Line, Message: err.Error()}
				}
				s.Results[i] = expr
				continue
			}
			terminal, ok, err := parseQueryVarTerminal(call, queryVars)
			if err != nil {
				return nil, exportError{File: path, Line: fset.Position(call.Pos()).Line, Message: err.Error()}
			}
			if !ok {
				continue
			}
			expr, err := e.gormVarTerminalExpr(terminal)
			if err != nil {
				return nil, exportError{File: path, Line: fset.Position(call.Pos()).Line, Message: err.Error()}
			}
			s.Results[i] = expr
		}
	case *ast.IfStmt:
		if s.Init != nil {
			rewritten, err := e.rewriteStmt(path, fset, s.Init, queryVars)
			if err != nil {
				return nil, err
			}
			s.Init = rewritten
		}
	}
	return stmt, nil
}

func (e *exporter) rejectRemainingPickleQueries(path string, fset *token.FileSet, f *ast.File) error {
	var firstErr error
	ast.Inspect(f, func(n ast.Node) bool {
		if firstErr != nil {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isQueryRootCall(call) {
			pos := fset.Position(call.Pos())
			firstErr = exportError{File: path, Line: pos.Line, Message: "unsupported Pickle query chain; exporter requires clean GORM lowering"}
			return false
		}
		return true
	})
	return firstErr
}

type queryChain struct {
	Model    string
	Terminal string
	Filters  []queryFilter
	Preloads []string
	Limit    ast.Expr
	Offset   ast.Expr
	Arg      ast.Expr
}

type queryFilter struct {
	Column string
	Op     string
	Arg    ast.Expr
}

func parseQueryChain(call *ast.CallExpr) (queryChain, bool, error) {
	return parseQueryChainWithTerminal(call, true)
}

func parseQueryBuilderChain(call *ast.CallExpr) (queryChain, bool, error) {
	return parseQueryChainWithTerminal(call, false)
}

func parseQueryChainWithTerminal(call *ast.CallExpr, requireTerminal bool) (queryChain, bool, error) {
	var methods []struct {
		name string
		args []ast.Expr
	}
	cur := ast.Expr(call)
	for {
		c, ok := cur.(*ast.CallExpr)
		if !ok {
			return queryChain{}, false, nil
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			return queryChain{}, false, nil
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == "models" && strings.HasPrefix(sel.Sel.Name, "Query") {
			return buildQueryChain(strings.TrimPrefix(sel.Sel.Name, "Query"), methods, requireTerminal)
		}
		methods = append(methods, struct {
			name string
			args []ast.Expr
		}{name: sel.Sel.Name, args: c.Args})
		if root, ok := sel.X.(*ast.SelectorExpr); ok {
			if id, ok := root.X.(*ast.Ident); ok && id.Name == "models" && strings.HasPrefix(root.Sel.Name, "Query") {
				return buildQueryChain(strings.TrimPrefix(root.Sel.Name, "Query"), methods, requireTerminal)
			}
		}
		if rootCall, ok := sel.X.(*ast.CallExpr); ok {
			if root, ok := rootCall.Fun.(*ast.SelectorExpr); ok {
				if id, ok := root.X.(*ast.Ident); ok && id.Name == "models" && strings.HasPrefix(root.Sel.Name, "Query") {
					return buildQueryChain(strings.TrimPrefix(root.Sel.Name, "Query"), methods, requireTerminal)
				}
			}
		}
		cur = sel.X
	}
}

func buildQueryChain(model string, methods []struct {
	name string
	args []ast.Expr
}, requireTerminal bool) (queryChain, bool, error) {
	qc := queryChain{Model: model}
	for i := len(methods) - 1; i >= 0; i-- {
		m := methods[i]
		switch {
		case m.name == "AnyOwner":
		case m.name == "Limit":
			if len(m.args) != 1 {
				return qc, true, fmt.Errorf("Limit requires one argument")
			}
			qc.Limit = m.args[0]
		case m.name == "Offset":
			if len(m.args) != 1 {
				return qc, true, fmt.Errorf("Offset requires one argument")
			}
			qc.Offset = m.args[0]
		case m.name == "SelectAll":
			if len(m.args) != 0 {
				return qc, true, fmt.Errorf("SelectAll does not accept arguments")
			}
		case strings.HasPrefix(m.name, "Where"):
			if len(m.args) != 1 {
				return qc, true, fmt.Errorf("%s requires one argument", m.name)
			}
			col, op, ok := whereMethodColumn(m.name, model)
			if !ok {
				return qc, true, fmt.Errorf("unsupported query method %s", m.name)
			}
			qc.Filters = append(qc.Filters, queryFilter{Column: col, Op: op, Arg: m.args[0]})
		case strings.HasPrefix(m.name, "With"):
			if len(m.args) != 0 {
				return qc, true, fmt.Errorf("%s does not accept arguments", m.name)
			}
			qc.Preloads = append(qc.Preloads, strings.TrimPrefix(m.name, "With"))
		case m.name == "First" || m.name == "All" || m.name == "Count" || strings.HasPrefix(m.name, "Sum") || strings.HasPrefix(m.name, "Avg"):
			if len(m.args) != 0 {
				return qc, true, fmt.Errorf("%s does not accept arguments", m.name)
			}
			qc.Terminal = m.name
		case m.name == "Create" || m.name == "Update" || m.name == "Delete":
			if len(m.args) != 1 {
				return qc, true, fmt.Errorf("%s requires one argument", m.name)
			}
			qc.Terminal = m.name
			qc.Arg = m.args[0]
		default:
			return qc, true, fmt.Errorf("unsupported query method %s", m.name)
		}
	}
	if requireTerminal && qc.Terminal == "" {
		return qc, true, fmt.Errorf("query chain has no terminal operation")
	}
	return qc, true, nil
}

type queryVarTerminal struct {
	Var      string
	Model    string
	Terminal string
}

func parseQueryVarTerminal(call *ast.CallExpr, queryVars map[string]string) (queryVarTerminal, bool, error) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return queryVarTerminal{}, false, nil
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return queryVarTerminal{}, false, nil
	}
	model, ok := queryVars[id.Name]
	if !ok {
		return queryVarTerminal{}, false, nil
	}
	if sel.Sel.Name != "First" && sel.Sel.Name != "All" && sel.Sel.Name != "Count" && !strings.HasPrefix(sel.Sel.Name, "Sum") && !strings.HasPrefix(sel.Sel.Name, "Avg") {
		return queryVarTerminal{}, false, nil
	}
	if len(call.Args) != 0 {
		return queryVarTerminal{}, true, fmt.Errorf("%s does not accept arguments", sel.Sel.Name)
	}
	return queryVarTerminal{Var: id.Name, Model: model, Terminal: sel.Sel.Name}, true, nil
}

func rewriteQueryVarMutation(call *ast.CallExpr, queryVars map[string]string) (ast.Stmt, bool, error) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false, nil
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return nil, false, nil
	}
	model, ok := queryVars[id.Name]
	if !ok {
		return nil, false, nil
	}

	var expr ast.Expr
	var err error
	switch {
	case strings.HasPrefix(sel.Sel.Name, "Where"):
		if len(call.Args) != 1 {
			return nil, true, fmt.Errorf("%s requires one argument", sel.Sel.Name)
		}
		col, op, ok := whereMethodColumn(sel.Sel.Name, model)
		if !ok {
			return nil, true, fmt.Errorf("unsupported query method %s", sel.Sel.Name)
		}
		expr, err = gormVarWhereExpr(id.Name, col, op, call.Args[0])
	case strings.HasPrefix(sel.Sel.Name, "With"):
		if len(call.Args) != 0 {
			return nil, true, fmt.Errorf("%s does not accept arguments", sel.Sel.Name)
		}
		expr, err = parseExpr(fmt.Sprintf("%s.Preload(%q)", id.Name, strings.TrimPrefix(sel.Sel.Name, "With")))
	case sel.Sel.Name == "Limit" || sel.Sel.Name == "Offset":
		if len(call.Args) != 1 {
			return nil, true, fmt.Errorf("%s requires one argument", sel.Sel.Name)
		}
		arg, argErr := exprString(call.Args[0])
		if argErr != nil {
			return nil, true, argErr
		}
		expr, err = parseExpr(fmt.Sprintf("%s.%s(%s)", id.Name, sel.Sel.Name, arg))
	case strings.HasPrefix(sel.Sel.Name, "OrderBy") && sel.Sel.Name != "OrderBy":
		if len(call.Args) != 1 {
			return nil, true, fmt.Errorf("%s requires one argument", sel.Sel.Name)
		}
		arg, argErr := exprString(call.Args[0])
		if argErr != nil {
			return nil, true, argErr
		}
		column := pascalToSnake(strings.TrimPrefix(sel.Sel.Name, "OrderBy"))
		expr, err = parseExpr(fmt.Sprintf("%s.Order(%q + \" \" + %s)", id.Name, column, arg))
	case sel.Sel.Name == "OrderBy":
		if len(call.Args) != 2 {
			return nil, true, fmt.Errorf("OrderBy requires two arguments")
		}
		col, colErr := exprString(call.Args[0])
		if colErr != nil {
			return nil, true, colErr
		}
		dir, dirErr := exprString(call.Args[1])
		if dirErr != nil {
			return nil, true, dirErr
		}
		expr, err = parseExpr(fmt.Sprintf("%s.Order(%s + \" \" + %s)", id.Name, col, dir))
	case sel.Sel.Name == "SelectAll" || sel.Sel.Name == "AnyOwner":
		if len(call.Args) != 0 {
			return nil, true, fmt.Errorf("%s does not accept arguments", sel.Sel.Name)
		}
		return &ast.EmptyStmt{}, true, nil
	default:
		return nil, true, fmt.Errorf("unsupported query method %s", sel.Sel.Name)
	}
	if err != nil {
		return nil, true, err
	}
	return &ast.AssignStmt{Lhs: []ast.Expr{id}, Tok: token.ASSIGN, Rhs: []ast.Expr{expr}}, true, nil
}

func (e *exporter) gormExpr(q queryChain) (ast.Expr, error) {
	if !e.models[q.Model] {
		return nil, fmt.Errorf("unknown exported model %s", q.Model)
	}
	switch {
	case q.Terminal == "First":
		return parseExpr(fmt.Sprintf("func() (*models.%s, error) { var record models.%s; err := %s.First(&record).Error; return &record, err }()", q.Model, q.Model, e.gormChain(q)))
	case q.Terminal == "All":
		return parseExpr(fmt.Sprintf("func() ([]models.%s, error) { var records []models.%s; err := %s.Find(&records).Error; return records, err }()", q.Model, q.Model, e.gormChain(q)))
	case q.Terminal == "Count":
		return parseExpr(fmt.Sprintf("func() (int64, error) { var count int64; err := %s.Count(&count).Error; return count, err }()", e.gormChain(q)))
	case strings.HasPrefix(q.Terminal, "Sum") || strings.HasPrefix(q.Terminal, "Avg"):
		fn := "SUM"
		field := strings.TrimPrefix(q.Terminal, "Sum")
		if strings.HasPrefix(q.Terminal, "Avg") {
			fn = "AVG"
			field = strings.TrimPrefix(q.Terminal, "Avg")
		}
		if field == "" {
			return nil, fmt.Errorf("unsupported aggregate query method %s", q.Terminal)
		}
		column := pascalToSnake(field)
		return parseExpr(fmt.Sprintf("func() (*float64, error) { var value *float64; err := %s.Select(%q).Scan(&value).Error; return value, err }()", e.gormChain(q), fn+"("+column+")"))
	case q.Terminal == "Create":
		arg, err := exprString(q.Arg)
		if err != nil {
			return nil, err
		}
		if _, ok := e.integrityModels[q.Model]; ok {
			return parseExpr(fmt.Sprintf("models.Create%s(%s)", q.Model, arg))
		}
		return parseExpr(fmt.Sprintf("models.DB.Create(%s).Error", arg))
	case q.Terminal == "Update":
		arg, err := exprString(q.Arg)
		if err != nil {
			return nil, err
		}
		if _, ok := e.integrityModels[q.Model]; ok {
			return parseExpr(fmt.Sprintf("models.Update%s(%s)", q.Model, arg))
		}
		return parseExpr(fmt.Sprintf("models.DB.Save(%s).Error", arg))
	case q.Terminal == "Delete":
		arg, err := exprString(q.Arg)
		if err != nil {
			return nil, err
		}
		if _, ok := e.integrityModels[q.Model]; ok {
			return parseExpr(fmt.Sprintf("models.Delete%s(%s)", q.Model, arg))
		}
		return parseExpr(fmt.Sprintf("models.DB.Delete(%s).Error", arg))
	default:
		return nil, fmt.Errorf("unsupported terminal query method %s", q.Terminal)
	}
}

func (e *exporter) gormBuilderExpr(q queryChain) (ast.Expr, error) {
	if !e.models[q.Model] {
		return nil, fmt.Errorf("unknown exported model %s", q.Model)
	}
	return parseExpr(e.gormChain(q))
}

func (e *exporter) gormVarTerminalExpr(q queryVarTerminal) (ast.Expr, error) {
	if !e.models[q.Model] {
		return nil, fmt.Errorf("unknown exported model %s", q.Model)
	}
	switch {
	case q.Terminal == "First":
		return parseExpr(fmt.Sprintf("func() (*models.%s, error) { var record models.%s; err := %s.First(&record).Error; return &record, err }()", q.Model, q.Model, q.Var))
	case q.Terminal == "All":
		return parseExpr(fmt.Sprintf("func() ([]models.%s, error) { var records []models.%s; err := %s.Find(&records).Error; return records, err }()", q.Model, q.Model, q.Var))
	case q.Terminal == "Count":
		return parseExpr(fmt.Sprintf("func() (int64, error) { var count int64; err := %s.Count(&count).Error; return count, err }()", q.Var))
	case strings.HasPrefix(q.Terminal, "Sum") || strings.HasPrefix(q.Terminal, "Avg"):
		fn := "SUM"
		field := strings.TrimPrefix(q.Terminal, "Sum")
		if strings.HasPrefix(q.Terminal, "Avg") {
			fn = "AVG"
			field = strings.TrimPrefix(q.Terminal, "Avg")
		}
		if field == "" {
			return nil, fmt.Errorf("unsupported aggregate query method %s", q.Terminal)
		}
		column := pascalToSnake(field)
		return parseExpr(fmt.Sprintf("func() (*float64, error) { var value *float64; err := %s.Select(%q).Scan(&value).Error; return value, err }()", q.Var, fn+"("+column+")"))
	default:
		return nil, fmt.Errorf("unsupported terminal query method %s", q.Terminal)
	}
}

func gormVarWhereExpr(varName, col, op string, arg ast.Expr) (ast.Expr, error) {
	if col == "__owner__" {
		col = "user_id"
	}
	argStr, err := exprString(arg)
	if err != nil {
		return nil, err
	}
	return parseExpr(fmt.Sprintf("%s.Where(%q, %s)", varName, col+" "+op+" ?", argStr))
}

func (e *exporter) gormChain(q queryChain) string {
	chain := fmt.Sprintf("models.DB.Model(&models.%s{})", q.Model)
	if info, ok := e.integrityModels[q.Model]; ok && info.Immutable && !q.hasVersionFilter() {
		table := info.Table.Name
		chain += fmt.Sprintf(".Where(%q)", "version_id = (SELECT MAX(version_id) FROM "+table+" latest WHERE latest.id = "+table+".id)")
	}
	for _, f := range q.Filters {
		col := f.Column
		if col == "__owner__" {
			col = q.ownerColumn()
		}
		arg, _ := exprString(f.Arg)
		chain += fmt.Sprintf(".Where(%q, %s)", col+" "+f.Op+" ?", arg)
	}
	for _, p := range q.Preloads {
		chain += fmt.Sprintf(".Preload(%q)", p)
	}
	if q.Limit != nil {
		arg, _ := exprString(q.Limit)
		chain += ".Limit(" + arg + ")"
	}
	if q.Offset != nil {
		arg, _ := exprString(q.Offset)
		chain += ".Offset(" + arg + ")"
	}
	return chain
}

func (q queryChain) hasVersionFilter() bool {
	for _, f := range q.Filters {
		if f.Column == "version_id" {
			return true
		}
	}
	return false
}

func (q queryChain) ownerColumn() string {
	return "user_id"
}

func whereMethodColumn(method, model string) (string, string, bool) {
	if method == "WhereOwnedBy" {
		return "__owner__", "=", true
	}
	name := strings.TrimPrefix(method, "Where")
	op := "="
	for _, suffix := range []struct {
		name string
		op   string
	}{
		{"NotIn", "NOT IN"},
		{"In", "IN"},
		{"GTE", ">="},
		{"GT", ">"},
		{"LTE", "<="},
		{"LT", "<"},
		{"Not", "<>"},
	} {
		if strings.HasSuffix(name, suffix.name) {
			name = strings.TrimSuffix(name, suffix.name)
			op = suffix.op
			break
		}
	}
	if name == "" {
		return "", "", false
	}
	return pascalToSnake(name), op, true
}

func isQueryRootCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "models" && strings.HasPrefix(sel.Sel.Name, "Query")
}

func parseExpr(src string) (ast.Expr, error) {
	expr, err := parser.ParseExpr(src)
	if err != nil {
		return nil, err
	}
	return expr, nil
}

func exprString(expr ast.Expr) (string, error) {
	var b bytes.Buffer
	if err := format.Node(&b, token.NewFileSet(), expr); err != nil {
		return "", err
	}
	return b.String(), nil
}

func (e *exporter) writeHTTPX() error {
	return e.writeFile("internal/httpx/httpx.go", []byte(httpxSource))
}

func (e *exporter) writeModels(tables []*schema.Table, views []*schema.View) error {
	if err := e.writeFile("app/models/db.go", []byte("package models\n\nimport \"gorm.io/gorm\"\n\nvar DB *gorm.DB\n\nfunc SetDB(db *gorm.DB) { DB = db }\n")); err != nil {
		return err
	}
	hasEncrypted := false
	for _, table := range tables {
		if tableHasEncryptedColumns(table) {
			hasEncrypted = true
		}
		data, err := generateModelFile(table)
		if err != nil {
			return err
		}
		if err := e.writeFile(filepath.Join("app", "models", modelFileName(table.Name)+".go"), data); err != nil {
			return err
		}
	}
	for _, view := range views {
		data, err := generateViewModelFile(view)
		if err != nil {
			return err
		}
		if err := e.writeFile(filepath.Join("app", "models", modelFileName(view.Name)+".go"), data); err != nil {
			return err
		}
	}
	if hasEncrypted {
		data, err := format.Source([]byte(exportedEncryptionSupportSource))
		if err != nil {
			return fmt.Errorf("formatting exported encryption support: %w", err)
		}
		if err := e.writeFile(filepath.Join("app", "models", "encryption_support.go"), data); err != nil {
			return err
		}
	}
	if len(e.integrityModels) > 0 {
		data, err := generateIntegritySupport(tables)
		if err != nil {
			return err
		}
		if err := e.writeFile(filepath.Join("app", "models", "integrity_support.go"), data); err != nil {
			return err
		}
	}
	return nil
}

func (e *exporter) writeActions() error {
	actionsDir := filepath.Join(e.project.Dir, "database", "actions")
	sets, err := generator.ScanActions(actionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("scanning actions: %w", err)
	}
	if len(sets) == 0 {
		return nil
	}
	var models []string
	for model := range sets {
		models = append(models, model)
	}
	sort.Strings(models)
	for _, model := range models {
		set := sets[model]
		if err := e.writeActionSet(set); err != nil {
			return err
		}
	}
	if err := e.writeActionAuditSupport(sets); err != nil {
		return err
	}
	return nil
}

func (e *exporter) hasGraphQLPackage() bool {
	_, err := os.Stat(filepath.Join(e.project.Dir, "app", "graphql"))
	return err == nil
}

func (e *exporter) writeGraphQLPackage(tables []*schema.Table, views []*schema.View) error {
	graphqlDir := filepath.Join(e.project.Dir, "app", "graphql")
	if _, err := os.Stat(graphqlDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := filepath.WalkDir(graphqlDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		data, err = e.rewriteGeneratedGraphQLSource(path, data)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(e.project.Dir, path)
		if err != nil {
			return err
		}
		return e.writeFile(rel, data)
	}); err != nil {
		return err
	}
	return e.writeGraphQLQuerySupport(tables, views)
}

func (e *exporter) rewriteGeneratedGraphQLSource(path string, data []byte) ([]byte, error) {
	if filepath.Base(path) == "handler_gen.go" {
		src := fmt.Sprintf(exportedGQLGenHandlerSource, e.modulePath, e.modulePath)
		formatted, err := format.Source([]byte(src))
		if err != nil {
			return nil, fmt.Errorf("formatting exported gqlgen handler %s: %w", path, err)
		}
		return formatted, nil
	}
	replaced := strings.ReplaceAll(string(data), e.sourceModule+"/", e.modulePath+"/")
	if filepath.Base(path) == "pickle_gen.go" {
		var err error
		replaced, err = rewriteExportedGraphQLCoreSource(replaced)
		if err != nil {
			return nil, fmt.Errorf("rewriting exported GraphQL core %s: %w", path, err)
		}
	}
	formatted, err := format.Source([]byte(replaced))
	if err != nil {
		return nil, fmt.Errorf("formatting exported GraphQL source %s: %w", path, err)
	}
	return formatted, nil
}

func rewriteExportedGraphQLCoreSource(src string) (string, error) {
	replacements := []struct {
		name string
		old  string
		new  string
	}{
		{
			name: "auth claims",
			old: `type AuthClaims struct {
	UserID string
	Role   string
}`,
			new: `type AuthClaims struct {
	UserID  string
	Role    string
	Roles   []string
	Manages bool
}`,
		},
		{
			name: "role matching",
			old: `func (c *ResolveContext) HasRole(role string) bool {
	if c.auth == nil {
		return false
	}
	return c.auth.Role == role
}`,
			new: `func (c *ResolveContext) HasRole(role string) bool {
	if c.auth == nil {
		return false
	}
	for _, assigned := range c.auth.Roles {
		if assigned == role {
			return true
		}
	}
	return c.auth.Role == role
}`,
		},
		{
			name: "owner visibility",
			old: `func (c *ResolveContext) CanSeeOwnerFields(ownerID string) bool {
	if c.auth == nil {
		return false
	}
	return c.auth.UserID == ownerID || c.auth.Role == "admin"
}`,
			new: `func (c *ResolveContext) CanSeeOwnerFields(ownerID string) bool {
	if c.auth == nil {
		return false
	}
	return c.auth.UserID == ownerID || c.auth.Role == "admin" || c.auth.Manages
}`,
		},
		{
			name: "visibility tier",
			old: `func (c *ResolveContext) Visibility() VisibilityTier {
	if c.auth == nil {
		return VisibilityPublic
	}
	if c.auth.Role == "admin" {
		return VisibilityAll
	}
	return VisibilityOwner
}`,
			new: `func (c *ResolveContext) Visibility() VisibilityTier {
	if c.auth == nil {
		return VisibilityPublic
	}
	if c.auth.Role == "admin" || c.auth.Manages {
		return VisibilityAll
	}
	return VisibilityOwner
}`,
		},
	}
	for _, replacement := range replacements {
		if !strings.Contains(src, replacement.old) {
			return "", fmt.Errorf("expected %s block was not found", replacement.name)
		}
		src = strings.Replace(src, replacement.old, replacement.new, 1)
	}
	return src, nil
}

func (e *exporter) writeGraphQLQuerySupport(tables []*schema.Table, views []*schema.View) error {
	var b strings.Builder
	b.WriteString("package models\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t\"strings\"\n")
	b.WriteString("\n")
	b.WriteString("\t\"gorm.io/gorm\"\n")
	b.WriteString(")\n\n")
	for _, table := range tables {
		writeGraphQLModelQuerySupport(&b, table.Name, table.Columns, false)
	}
	for _, view := range views {
		var cols []*schema.Column
		for i := range view.Columns {
			cols = append(cols, &view.Columns[i].Column)
		}
		writeGraphQLModelQuerySupport(&b, view.Name, cols, true)
	}
	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return fmt.Errorf("formatting exported GraphQL query support: %w", err)
	}
	return e.writeFile(filepath.Join("app", "models", "graphql_query_support.go"), formatted)
}

func writeGraphQLModelQuerySupport(b *strings.Builder, tableName string, columns []*schema.Column, readOnly bool) {
	structName := tableToStruct(tableName)
	queryName := structName + "Query"
	publicCols := graphQLVisibilitySelectColumns(columns, func(col *schema.Column) bool {
		return col.IsPublic
	})
	ownerCols := graphQLVisibilitySelectColumns(columns, func(col *schema.Column) bool {
		return col.IsPublic || col.IsOwnerSees
	})
	b.WriteString(fmt.Sprintf("type %s struct { db *gorm.DB }\n\n", queryName))
	b.WriteString(fmt.Sprintf("func Query%s() *%s { return &%s{db: DB.Model(&%s{})} }\n\n", structName, queryName, queryName, structName))
	b.WriteString(fmt.Sprintf("func (q *%s) SelectPublic() *%s { q.db = q.db.Select([]string{%s}); return q }\n", queryName, queryName, quotedStringList(publicCols)))
	b.WriteString(fmt.Sprintf("func (q *%s) SelectOwner() *%s { q.db = q.db.Select([]string{%s}); return q }\n", queryName, queryName, quotedStringList(ownerCols)))
	b.WriteString(fmt.Sprintf("func (q *%s) SelectAll() *%s { q.db = q.db.Select(\"*\"); return q }\n", queryName, queryName))
	b.WriteString(fmt.Sprintf("func (q *%s) AnyOwner() *%s { return q }\n", queryName, queryName))
	b.WriteString(fmt.Sprintf("func (q *%s) Limit(n int) *%s { q.db = q.db.Limit(n); return q }\n", queryName, queryName))
	b.WriteString(fmt.Sprintf("func (q *%s) Offset(n int) *%s { q.db = q.db.Offset(n); return q }\n", queryName, queryName))
	b.WriteString(fmt.Sprintf("func (q *%s) OrderBy(column, direction string) *%s {\n", queryName, queryName))
	b.WriteString("\tdir := strings.ToUpper(direction)\n")
	b.WriteString("\tif dir != \"DESC\" { dir = \"ASC\" }\n")
	b.WriteString("\tswitch column {\n")
	for _, col := range columns {
		b.WriteString(fmt.Sprintf("\tcase %q:\n", col.Name))
	}
	b.WriteString("\tdefault:\n")
	b.WriteString("\t\treturn q\n")
	b.WriteString("\t}\n")
	b.WriteString("\tq.db = q.db.Order(column + \" \" + dir)\n")
	b.WriteString("\treturn q\n")
	b.WriteString("}\n")
	for _, col := range columns {
		fieldName := snakeToPascal(col.Name)
		for _, suffix := range []string{"", "Like", "In", "After", "Before", "GTE", "GT", "LTE", "LT", "Not", "NotIn"} {
			b.WriteString(fmt.Sprintf("func (q *%s) Where%s%s(value any) *%s { q.db = q.db.Where(%q, value); return q }\n", queryName, fieldName, suffix, queryName, col.Name+" "+whereSuffixOperator(suffix)+" ?"))
		}
	}
	b.WriteString(fmt.Sprintf("func (q *%s) First() (*%s, error) { var record %s; err := q.db.First(&record).Error; return &record, err }\n", queryName, structName, structName))
	b.WriteString(fmt.Sprintf("func (q *%s) All() ([]%s, error) { var records []%s; err := q.db.Find(&records).Error; return records, err }\n", queryName, structName, structName))
	b.WriteString(fmt.Sprintf("func (q *%s) Count() (int64, error) { var count int64; err := q.db.Count(&count).Error; return count, err }\n", queryName))
	if !readOnly {
		b.WriteString(fmt.Sprintf("func (q *%s) Create(record *%s) error { return DB.Create(record).Error }\n", queryName, structName))
		b.WriteString(fmt.Sprintf("func (q *%s) Update(record *%s) error { return DB.Save(record).Error }\n", queryName, structName))
		b.WriteString(fmt.Sprintf("func (q *%s) Delete(record *%s) error { return DB.Delete(record).Error }\n", queryName, structName))
	}
	b.WriteByte('\n')
}

func graphQLVisibilitySelectColumns(columns []*schema.Column, visible func(*schema.Column) bool) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, col := range columns {
		if visible(col) || col.IsPrimaryKey || col.IsOwnerColumn || col.ForeignKeyTable != "" {
			add(graphQLSelectColumnName(col))
		}
	}
	if len(out) == 0 {
		add("*")
	}
	return out
}

func graphQLSelectColumnName(col *schema.Column) string {
	if col.IsEncrypted || col.IsSealed {
		return col.Name + "_encrypted"
	}
	return col.Name
}

const exportedGQLGenHandlerSource = `// Code generated by Pickle. DO NOT EDIT.
package graphql

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	gqlgen "github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"

	appauth "%s/app/http/auth"
	appmodels "%s/app/models"
)

// Handler returns a production GraphQL handler backed by gqlgen's parser,
// validator, transport handling, and executable schema runtime.
func Handler() http.Handler {
	srv := handler.New(pickleExecutableSchema{})
	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.POST{})
	srv.SetQueryCache(lru.New[*ast.QueryDocument](1000))
	srv.SetParserTokenLimit(maxQueryInputNodes * 20)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Query().Get("sdl") != "" {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(SchemaSDL))
			return
		}
		if r.Method == http.MethodPost && r.Header.Get("Content-Type") == "" {
			r.Header.Set("Content-Type", "application/json")
		}
		srv.ServeHTTP(graphQLStatusWriter{ResponseWriter: w}, r)
	})
}

type graphQLStatusWriter struct {
	http.ResponseWriter
}

func (w graphQLStatusWriter) WriteHeader(status int) {
	if status == http.StatusBadRequest || status == http.StatusUnprocessableEntity {
		status = http.StatusOK
	}
	w.ResponseWriter.WriteHeader(status)
}

type pickleExecutableSchema struct{}

type graphQLRoleRow struct {
	Slug    string
	Manages bool
}

func (pickleExecutableSchema) Schema() *ast.Schema {
	return parsedSchema
}

func (pickleExecutableSchema) Complexity(ctx context.Context, typeName, fieldName string, childComplexity int, args map[string]any) (int, bool) {
	cost, ok := generatedFieldCosts[typeName+"."+fieldName]
	if !ok {
		return childComplexity + 1, true
	}
	base := cost.BaseCost
	if base <= 0 {
		base = 1
	}
	if cost.IsList {
		limit := defaultGraphQLPageSize
		if pageArg, ok := args["page"].(map[string]any); ok {
			if n, ok := pageArg["first"].(int); ok && n > 0 {
				limit = n
			}
		}
		base *= limit
	}
	return childComplexity + base, true
}

func (pickleExecutableSchema) Exec(ctx context.Context) gqlgen.ResponseHandler {
	return gqlgen.OneShot(execGQLGenOperation(ctx))
}

func execGQLGenOperation(ctx context.Context) *gqlgen.Response {
	opCtx := gqlgen.GetOperationContext(ctx)
	doc, err := documentFromOperationContext(opCtx)
	if err != nil {
		return gqlgenErrorResponse(err.Error(), CodeBadUserInput)
	}
	auth, err := extractAuthFromHeaders(opCtx.Headers)
	if err != nil {
		return gqlgenErrorResponse(err.Error(), CodeUnauthenticated)
	}
	resolveCtx := &ResolveContext{
		auth:      auth,
		variables: opCtx.Variables,
	}
	resolveCtx.loaders = newDataLoaderRegistry(resolveCtx.Visibility())

	stats, err := enforceQueryBudget(doc, defaultQueryBudget())
	if err != nil {
		return gqlgenErrorResponse(err.Error(), CodeBadUserInput)
	}
	resolveCtx.queryStats = stats

	data, gqlErrs := execute(resolveCtx, &RootResolver{}, doc)
	raw, err := json.Marshal(data)
	if err != nil {
		return gqlgenErrorResponse(err.Error(), CodeInternalServerError)
	}
	return &gqlgen.Response{
		Data:   raw,
		Errors: convertGraphQLErrors(gqlErrs),
	}
}

func documentFromOperationContext(opCtx *gqlgen.OperationContext) (*Document, error) {
	if opCtx == nil || opCtx.Operation == nil {
		return nil, BadInput("operation is required")
	}
	return &Document{
		Operation: strings.ToLower(string(opCtx.Operation.Operation)),
		Name:      opCtx.Operation.Name,
		Fields:    convertSelectionSetWithVariables(opCtx.Operation.SelectionSet, opCtx.Variables),
		Variables: opCtx.Variables,
	}, nil
}

func convertSelectionSetWithVariables(ss ast.SelectionSet, variables map[string]any) []Field {
	fields := make([]Field, 0, len(ss))
	for _, sel := range ss {
		switch s := sel.(type) {
		case *ast.Field:
			fields = append(fields, Field{
				Name:       s.Name,
				Alias:      s.Alias,
				Args:       convertArgumentsWithVariables(s.Arguments, variables),
				Selections: convertSelectionSetWithVariables(s.SelectionSet, variables),
			})
		case *ast.InlineFragment:
			fields = append(fields, convertSelectionSetWithVariables(s.SelectionSet, variables)...)
		}
	}
	return fields
}

func convertArgumentsWithVariables(args ast.ArgumentList, variables map[string]any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	m := make(map[string]any, len(args))
	for _, arg := range args {
		m[arg.Name] = valueToGoWithVariables(arg.Value, variables)
	}
	return m
}

func valueToGoWithVariables(v *ast.Value, variables map[string]any) any {
	if v == nil {
		return nil
	}
	if v.Kind == ast.Variable {
		return variables[v.Raw]
	}
	switch v.Kind {
	case ast.ListValue:
		list := make([]any, len(v.Children))
		for i, child := range v.Children {
			list[i] = valueToGoWithVariables(child.Value, variables)
		}
		return list
	case ast.ObjectValue:
		obj := make(map[string]any, len(v.Children))
		for _, child := range v.Children {
			obj[child.Name] = valueToGoWithVariables(child.Value, variables)
		}
		return obj
	default:
		return valueToGo(v)
	}
}

func extractAuthFromHeaders(headers http.Header) (*AuthClaims, error) {
	if len(headers) == 0 || headers.Get("Authorization") == "" {
		return nil, nil
	}
	info, err := appauth.Authenticate(&http.Request{Header: headers})
	if err != nil {
		return nil, err
	}
	claims := &AuthClaims{UserID: info.UserID, Role: info.Role}
	if err := loadGraphQLRBACClaims(claims); err != nil {
		return nil, err
	}
	return claims, nil
}

func loadGraphQLRBACClaims(claims *AuthClaims) error {
	if claims == nil || claims.UserID == "" || appmodels.DB == nil {
		return nil
	}
	var roles []graphQLRoleRow
	err := appmodels.DB.Raw("SELECT r.slug, r.manages FROM roles r JOIN role_user ru ON ru.role_id = r.id WHERE ru.user_id = ?", claims.UserID).Scan(&roles).Error
	if err != nil {
		if isMissingRBACTableError(err) {
			return nil
		}
		return err
	}
	if len(roles) == 0 {
		return nil
	}
	claims.Roles = claims.Roles[:0]
	for _, role := range roles {
		if role.Slug == "" {
			continue
		}
		claims.Roles = append(claims.Roles, role.Slug)
		if role.Manages {
			claims.Manages = true
		}
	}
	if claims.Role == "" && len(claims.Roles) > 0 {
		claims.Role = claims.Roles[0]
	}
	return nil
}

func isMissingRBACTableError(err error) bool {
	if err == nil {
		return false
	}
	for err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "no such table") ||
			strings.Contains(msg, "does not exist") ||
			strings.Contains(msg, "doesn't exist") ||
			strings.Contains(msg, "unknown table") {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

func gqlgenErrorResponse(message, code string) *gqlgen.Response {
	return &gqlgen.Response{
		Data: json.RawMessage("null"),
		Errors: gqlerror.List{
			{Message: message, Extensions: map[string]any{"code": code}},
		},
	}
}

func convertGraphQLErrors(in []map[string]any) gqlerror.List {
	if len(in) == 0 {
		return nil
	}
	out := make(gqlerror.List, 0, len(in))
	for _, item := range in {
		err := &gqlerror.Error{}
		if msg, ok := item["message"].(string); ok {
			err.Message = msg
		}
		if extensions, ok := item["extensions"].(map[string]any); ok {
			err.Extensions = extensions
		}
		out = append(out, err)
	}
	return out
}
`

func whereSuffixOperator(suffix string) string {
	switch suffix {
	case "Like":
		return "LIKE"
	case "In":
		return "IN"
	case "After", "GT":
		return ">"
	case "Before", "LT":
		return "<"
	case "GTE":
		return ">="
	case "LTE":
		return "<="
	case "Not":
		return "<>"
	case "NotIn":
		return "NOT IN"
	default:
		return "="
	}
}

func (e *exporter) writeActionSet(set *generator.ActionSet) error {
	if err := generator.ValidateActions(set); err != nil {
		e.result.Findings = append(e.result.Findings, Finding{File: filepath.Join("database", "actions", set.Model), Rule: "action_export_unsupported_signature", Message: err.Error()})
	}
	seen := map[string]bool{}
	for _, action := range set.Actions {
		if err := e.copyActionSource(action.SourceFile, set.Model, seen); err != nil {
			return err
		}
	}
	for _, gate := range set.Gates {
		if err := e.copyActionSource(gate.SourceFile, set.Model, seen); err != nil {
			return err
		}
	}
	if len(seen) > 0 {
		if err := e.writeActionSupport(set.Model); err != nil {
			return err
		}
	}
	if len(set.Actions) > 0 {
		data, err := e.generateActionModelWiring(set)
		if err != nil {
			return err
		}
		if err := e.writeFile(filepath.Join("app", "models", modelFileName(set.Model)+"_actions.go"), data); err != nil {
			return err
		}
	}
	return nil
}

func (e *exporter) copyActionSource(sourceFile, model string, seen map[string]bool) error {
	if seen[sourceFile] {
		return nil
	}
	seen[sourceFile] = true
	data, err := os.ReadFile(filepath.Join(e.project.Dir, sourceFile))
	if err != nil {
		return err
	}
	rewritten, err := e.rewriteGoFile(filepath.Join(e.project.Dir, sourceFile), data)
	if err != nil {
		return err
	}
	rewritten, err = e.rewriteActionSourceToModels(filepath.Join(e.project.Dir, sourceFile), rewritten)
	if err != nil {
		return err
	}
	return e.writeFile(filepath.Join("app", "models", modelFileName(model)+"_"+filepath.Base(sourceFile)), rewritten)
}

func (e *exporter) writeActionSupport(model string) error {
	src := fmt.Sprintf(`package models

import (
	"errors"
	"fmt"
	"log"
	"net"

	"%s/internal/httpx"
)

type Context = httpx.Context

var ErrUnauthorized = errors.New("unauthorized")

type AuditFunc func(ctx *httpx.Context, action, model string, resourceID any, extra string)

var OnAuditPerformed AuditFunc
var OnAuditDenied AuditFunc
var OnAuditFailed AuditFunc

func AuditPerformed(ctx *httpx.Context, action, model string, resourceID any) {
	if OnAuditPerformed != nil {
		OnAuditPerformed(ctx, action, model, resourceID, "")
		return
	}
	log.Printf("audit.performed user_id=%%s roles=%%v action=%%s model=%%s resource_id=%%v ip=%%s request_id=%%s",
		auditUserID(ctx), auditRoles(ctx), action, model, resourceID, auditContextIP(ctx), auditContextRequestID(ctx))
}

func AuditDenied(ctx *httpx.Context, action, model string, resourceID any, reason string) {
	if OnAuditDenied != nil {
		OnAuditDenied(ctx, action, model, resourceID, reason)
		return
	}
	log.Printf("audit.denied user_id=%%s roles=%%v action=%%s model=%%s resource_id=%%v reason=%%s ip=%%s request_id=%%s",
		auditUserID(ctx), auditRoles(ctx), action, model, resourceID, reason, auditContextIP(ctx), auditContextRequestID(ctx))
}

func AuditFailed(ctx *httpx.Context, action, model string, resourceID any, err error) {
	if OnAuditFailed != nil {
		OnAuditFailed(ctx, action, model, resourceID, err.Error())
		return
	}
	log.Printf("audit.failed user_id=%%s roles=%%v action=%%s model=%%s resource_id=%%v error=%%v ip=%%s request_id=%%s",
		auditUserID(ctx), auditRoles(ctx), action, model, resourceID, err, auditContextIP(ctx), auditContextRequestID(ctx))
}

func auditUserID(ctx *httpx.Context) string {
	if ctx == nil || ctx.Auth() == nil {
		return ""
	}
	return ctx.Auth().UserID
}

func auditRoles(ctx *httpx.Context) string {
	if ctx == nil {
		return "[]"
	}
	return fmt.Sprintf("%%v", ctx.Roles())
}

func auditContextIP(ctx *httpx.Context) string {
	if ctx == nil || ctx.Request() == nil {
		return ""
	}
	req := ctx.Request()
	if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	if xri := req.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}

func auditContextRequestID(ctx *httpx.Context) string {
	if ctx == nil || ctx.Request() == nil {
		return ""
	}
	if id := ctx.Request().Header.Get("X-Request-ID"); id != "" {
		return id
	}
	return ctx.Request().Header.Get("X-Request-Id")
}
`, e.modulePath)
	formatted, err := format.Source([]byte(src))
	if err != nil {
		return err
	}
	return e.writeFile(filepath.Join("app", "models", "actions_support.go"), formatted)
}

func (e *exporter) rewriteActionSourceToModels(path string, data []byte) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, data, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	f.Name.Name = "models"
	f.Comments = filterPickleComments(f.Comments)
	var modelAliases []string
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if p == e.modulePath+"/app/models" {
			if imp.Name != nil {
				modelAliases = append(modelAliases, imp.Name.Name)
			} else {
				modelAliases = append(modelAliases, "models")
			}
		}
	}
	if len(modelAliases) > 0 {
		removeImportsByPath(f, e.modulePath+"/app/models")
	}
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, f); err != nil {
		return nil, err
	}
	out := buf.String()
	for _, alias := range modelAliases {
		out = strings.ReplaceAll(out, alias+".", "")
	}
	if strings.HasSuffix(filepath.Base(path), "_gate_gen.go") {
		out = strings.ReplaceAll(out, "model interface{ OwnerID() string }", "model any")
		out = strings.ReplaceAll(out, "model interface {\n\tOwnerID() string\n}", "model any")
	}
	formatted, err := format.Source([]byte(out))
	if err != nil {
		return []byte(out), err
	}
	return formatted, nil
}

func filterPickleComments(groups []*ast.CommentGroup) []*ast.CommentGroup {
	var out []*ast.CommentGroup
	for _, group := range groups {
		if strings.Contains(group.Text(), "Pickle") {
			continue
		}
		out = append(out, group)
	}
	return out
}

func removeImportsByPath(f *ast.File, path string) {
	var decls []ast.Decl
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			decls = append(decls, decl)
			continue
		}
		var specs []ast.Spec
		for _, spec := range gen.Specs {
			imp := spec.(*ast.ImportSpec)
			p, _ := strconv.Unquote(imp.Path.Value)
			if p != path {
				specs = append(specs, spec)
			}
		}
		gen.Specs = specs
		if len(gen.Specs) > 0 {
			decls = append(decls, gen)
		}
	}
	f.Decls = decls
}

func (e *exporter) generateActionModelWiring(set *generator.ActionSet) ([]byte, error) {
	structName := tableToStruct(set.Model)
	var b strings.Builder
	b.WriteString("package models\n\n")
	b.WriteString("import (\n")
	b.WriteString(fmt.Sprintf("\t\"%s/internal/httpx\"\n", e.modulePath))
	b.WriteString(")\n\n")
	for _, action := range set.Actions {
		resultType := action.ResultType
		b.WriteString(fmt.Sprintf("func (m *%s) %s(ctx *httpx.Context, action %s) ", structName, action.Name, action.StructName))
		if action.HasResult {
			b.WriteString(fmt.Sprintf("(%s, error) {\n", resultType))
		} else {
			b.WriteString("error {\n")
		}
		b.WriteString(fmt.Sprintf("\troleID := Can%s(ctx, m)\n", action.Name))
		b.WriteString("\tif roleID == nil {\n")
		b.WriteString(fmt.Sprintf("\t\tAuditDenied(ctx, %q, %q, m.ID, \"gate denied\")\n", action.Name, structName))
		if action.HasResult {
			b.WriteString("\t\treturn nil, ErrUnauthorized\n")
		} else {
			b.WriteString("\t\treturn ErrUnauthorized\n")
		}
		b.WriteString("\t}\n")
		if action.HasResult {
			b.WriteString(fmt.Sprintf("\tvar result %s\n", resultType))
			b.WriteString(fmt.Sprintf("\terr := runAuditedAction(ctx, %q, %q, m.ID, actionVersionID(m), roleID, func() error {\n", structName, action.Name))
			b.WriteString(fmt.Sprintf("\t\tvar execErr error\n\t\tresult, execErr = action.%s(ctx, m)\n\t\treturn execErr\n\t})\n", action.Name))
			b.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
			b.WriteString("\treturn result, nil\n")
		} else {
			b.WriteString(fmt.Sprintf("\treturn runAuditedAction(ctx, %q, %q, m.ID, actionVersionID(m), roleID, func() error {\n", structName, action.Name))
			b.WriteString(fmt.Sprintf("\t\treturn action.%s(ctx, m)\n\t})\n", action.Name))
		}
		b.WriteString("}\n\n")
	}
	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return []byte(b.String()), fmt.Errorf("formatting exported action wiring for %s: %w", set.Model, err)
	}
	return formatted, nil
}

func (e *exporter) writeActionAuditSupport(sets map[string]*generator.ActionSet) error {
	data, err := e.generateActionAuditSupport(sets)
	if err != nil {
		return err
	}
	return e.writeFile(filepath.Join("app", "models", "action_audit_support.go"), data)
}

func (e *exporter) generateActionAuditSupport(sets map[string]*generator.ActionSet) ([]byte, error) {
	type actionSeed struct {
		Model       string
		Action      string
		ModelTypeID int
		ActionID    int
	}
	var models []string
	for model, set := range sets {
		if len(set.Actions) > 0 {
			models = append(models, model)
		}
	}
	sort.Strings(models)
	var seeds []actionSeed
	modelIDs := map[string]int{}
	nextModelID := 1
	nextActionID := 1
	for _, model := range models {
		set := sets[model]
		structName := tableToStruct(set.Model)
		modelID := modelIDs[structName]
		if modelID == 0 {
			modelID = nextModelID
			nextModelID++
			modelIDs[structName] = modelID
		}
		sort.Slice(set.Actions, func(i, j int) bool { return set.Actions[i].Name < set.Actions[j].Name })
		for _, action := range set.Actions {
			seeds = append(seeds, actionSeed{Model: structName, Action: action.Name, ModelTypeID: modelID, ActionID: nextActionID})
			nextActionID++
		}
	}
	var modelNames []string
	for name := range modelIDs {
		modelNames = append(modelNames, name)
	}
	sort.Strings(modelNames)

	var b strings.Builder
	b.WriteString(`package models

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"` + e.modulePath + `/internal/httpx"
)

type actionAuditModelSeed struct {
	ID int
	Name string
}

type actionAuditActionSeed struct {
	ID int
	ModelTypeID int
	Model string
	Action string
}

var actionAuditMu sync.Mutex

var actionAuditModelSeeds = []actionAuditModelSeed{
`)
	for _, name := range modelNames {
		fmt.Fprintf(&b, "\t{ID: %d, Name: %q},\n", modelIDs[name], name)
	}
	b.WriteString(`}

var actionAuditActionSeeds = []actionAuditActionSeed{
`)
	for _, seed := range seeds {
		fmt.Fprintf(&b, "\t{ID: %d, ModelTypeID: %d, Model: %q, Action: %q},\n", seed.ActionID, seed.ModelTypeID, seed.Model, seed.Action)
	}
	b.WriteString(`}

func ensureActionAuditSchema(db *gorm.DB) error {
	stmts := []string{
		` + "`" + `CREATE TABLE IF NOT EXISTS model_types (id INTEGER PRIMARY KEY, name VARCHAR(100) NOT NULL UNIQUE)` + "`" + `,
		` + "`" + `CREATE TABLE IF NOT EXISTS action_types (id INTEGER PRIMARY KEY, model_type_id INTEGER NOT NULL, name VARCHAR(100) NOT NULL, UNIQUE(model_type_id, name))` + "`" + `,
		` + "`" + `CREATE TABLE IF NOT EXISTS user_actions (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, action_type_id INTEGER NOT NULL, resource_id TEXT NOT NULL, resource_version_id TEXT, role_id TEXT, ip_address VARCHAR(45), request_id VARCHAR(100), created_at DATETIME NOT NULL)` + "`" + `,
	}
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil { return err }
	}
	for _, model := range actionAuditModelSeeds {
		if err := db.Exec(actionAuditModelTypeUpsertSQL(db), model.ID, model.Name).Error; err != nil { return err }
	}
	for _, action := range actionAuditActionSeeds {
		if err := db.Exec(actionAuditActionTypeUpsertSQL(db), action.ID, action.ModelTypeID, action.Action).Error; err != nil { return err }
	}
	return nil
}

func actionAuditModelTypeUpsertSQL(db *gorm.DB) string {
	return actionAuditModelTypeUpsertSQLForDialect(gormDialectName(db))
}

func actionAuditModelTypeUpsertSQLForDialect(dialect string) string {
	if dialect == "mysql" {
		return "INSERT INTO model_types (id, name) VALUES (?, ?) ON DUPLICATE KEY UPDATE name = VALUES(name)"
	}
	return "INSERT INTO model_types (id, name) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET name = excluded.name"
}

func actionAuditActionTypeUpsertSQL(db *gorm.DB) string {
	return actionAuditActionTypeUpsertSQLForDialect(gormDialectName(db))
}

func actionAuditActionTypeUpsertSQLForDialect(dialect string) string {
	if dialect == "mysql" {
		return "INSERT INTO action_types (id, model_type_id, name) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE model_type_id = VALUES(model_type_id), name = VALUES(name)"
	}
	return "INSERT INTO action_types (id, model_type_id, name) VALUES (?, ?, ?) ON CONFLICT(id) DO UPDATE SET model_type_id = excluded.model_type_id, name = excluded.name"
}

func gormDialectName(db *gorm.DB) string {
	if db == nil || db.Dialector == nil { return "" }
	return db.Dialector.Name()
}

func actionTypeID(model, action string) (int, bool) {
	for _, seed := range actionAuditActionSeeds {
		if seed.Model == model && seed.Action == action {
			return seed.ID, true
		}
	}
	return 0, false
}

func runAuditedAction(ctx *httpx.Context, model, action string, resourceID uuid.UUID, resourceVersionID *uuid.UUID, roleID *uuid.UUID, fn func() error) error {
	if DB == nil {
		return errors.New("models: DB is nil")
	}
	actionID, ok := actionTypeID(model, action)
	if !ok {
		return fmt.Errorf("models: missing action audit seed for %s.%s", model, action)
	}
	actionAuditMu.Lock()
	defer actionAuditMu.Unlock()
	previous := DB
	err := previous.Transaction(func(tx *gorm.DB) error {
		DB = tx
		defer func() { DB = previous }()
		if err := ensureActionAuditSchema(tx); err != nil {
			return err
		}
		if err := fn(); err != nil {
			AuditFailed(ctx, action, model, resourceID, err)
			return err
		}
		return recordActionPerformed(tx, ctx, actionID, resourceID, resourceVersionID, roleID)
	})
	DB = previous
	if err == nil {
		AuditPerformed(ctx, action, model, resourceID)
	}
	return err
}

func actionVersionID(record any) *uuid.UUID {
	if record == nil {
		return nil
	}
	rv := reflect.ValueOf(record)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	field := rv.FieldByName("VersionID")
	if !field.IsValid() {
		return nil
	}
	if id, ok := field.Interface().(uuid.UUID); ok && id != uuid.Nil {
		return &id
	}
	return nil
}

func recordActionPerformed(db *gorm.DB, ctx *httpx.Context, actionTypeID int, resourceID uuid.UUID, resourceVersionID *uuid.UUID, roleID *uuid.UUID) error {
	userID, err := uuid.Parse(ctx.Auth().UserID)
	if err != nil {
		return fmt.Errorf("audit user id: %w", err)
	}
	ip := auditContextIP(ctx)
	requestID := auditContextRequestID(ctx)
	return db.Exec("INSERT INTO user_actions (id, user_id, action_type_id, resource_id, resource_version_id, role_id, ip_address, request_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		uuid.New().String(), userID.String(), actionTypeID, resourceID.String(), nullableUUID(resourceVersionID), nullableUUID(roleID), nullableString(ip), nullableString(requestID), time.Now().UTC()).Error
}

func nullableUUID(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return id.String()
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
`)
	return format.Source([]byte(b.String()))
}

func (e *exporter) writeSQLMigrations(tables []*schema.Table, views []*schema.View) error {
	migrations, err := e.generateSQLMigrations(tables, views)
	if err != nil {
		return err
	}
	e.sqlMigrations = migrations
	for _, migration := range migrations {
		if err := e.writeFile(filepath.Join("database", "migrations", migration.Name+".up.sql"), []byte(migration.Up)); err != nil {
			return err
		}
		if err := e.writeFile(filepath.Join("database", "migrations", migration.Name+".down.sql"), []byte(migration.Down)); err != nil {
			return err
		}
	}
	return nil
}

func (e *exporter) writeCommandsSupport() error {
	if !e.hasCommands() {
		return nil
	}
	migrations, err := generateSQLMigrationSupport(e.sqlMigrations)
	if err != nil {
		return err
	}
	if err := e.writeFile(filepath.Join("database", "migrations", "support.go"), migrations); err != nil {
		return err
	}
	commands, err := e.generateCommandsSupport()
	if err != nil {
		return err
	}
	return e.writeFile(filepath.Join("app", "commands", "support.go"), commands)
}

func (e *exporter) hasCommands() bool {
	_, err := os.Stat(filepath.Join(e.project.Dir, "app", "commands"))
	return err == nil
}

func (e *exporter) writePolicySupport() error {
	if !e.hasPolicySupport() {
		return nil
	}
	data, err := e.generatePolicySupport()
	if err != nil {
		return err
	}
	if err := e.writeFile(filepath.Join("database", "policies", "support.go"), data); err != nil {
		return err
	}
	if e.hasRolePolicies() {
		data := fmt.Sprintf(rbacMiddlewareSupportSource, e.modulePath, e.modulePath)
		formatted, err := format.Source([]byte(data))
		if err != nil {
			return fmt.Errorf("formatting exported RBAC middleware support: %w", err)
		}
		if err := e.writeFile(filepath.Join("app", "http", "middleware", "rbac_support.go"), formatted); err != nil {
			return err
		}
	}
	return nil
}

func (e *exporter) hasPolicySupport() bool {
	return e.hasRolePolicies() || e.hasGraphQLPolicies()
}

func (e *exporter) hasRolePolicies() bool {
	dir := filepath.Join(e.project.Dir, "database", "policies")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_gen.go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		return true
	}
	return false
}

func (e *exporter) hasGraphQLPolicies() bool {
	dir := filepath.Join(e.project.Dir, "database", "policies", "graphql")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_gen.go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		return true
	}
	return false
}

func (e *exporter) generatePolicySupport() ([]byte, error) {
	var roles []generator.DerivedRole
	var rolePolicyIDs []string
	var graphQLPolicyIDs []string
	if e.hasRolePolicies() {
		policies, err := generator.ParsePolicyOps(filepath.Join(e.project.Dir, "database", "policies"))
		if err != nil {
			return nil, err
		}
		roles = generator.StaticDeriveRoles(policies)
		for _, policy := range policies {
			rolePolicyIDs = append(rolePolicyIDs, policy.PolicyID)
		}
	}
	var graphQLState generator.DerivedGraphQLState
	if e.hasGraphQLPolicies() {
		graphQLState = generator.DeriveGraphQLStateFromDir(filepath.Join(e.project.Dir, "database", "policies", "graphql"))
		entries, err := generator.ScanGraphQLPolicyFiles(filepath.Join(e.project.Dir, "database", "policies", "graphql"))
		if err == nil {
			for _, entry := range entries {
				graphQLPolicyIDs = append(graphQLPolicyIDs, entry.ID)
			}
		}
	}
	sort.Strings(rolePolicyIDs)
	sort.Strings(graphQLPolicyIDs)

	var b strings.Builder
	b.WriteString(`package policies

import (
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type PolicyStatus struct {
	ID string
	Batch int
	State string
	Applied bool
}

type roleSeed struct {
	Slug string
	Name string
	Manages bool
	Default bool
	BirthPolicy string
	Actions []string
}

type graphQLExposureSeed struct {
	Model string
	Operation string
}

type graphQLActionSeed struct {
	Name string
}

var roleSeeds = []roleSeed{
`)
	for _, role := range roles {
		name := role.DisplayName
		if name == "" {
			name = role.Slug
		}
		fmt.Fprintf(&b, "\t{Slug: %q, Name: %q, Manages: %t, Default: %t, BirthPolicy: %q, Actions: []string{%s}},\n", role.Slug, name, role.IsManages, role.IsDefault, role.BirthTimestamp, quotedStringList(role.Actions))
	}
	b.WriteString(`}

var graphQLExposureSeeds = []graphQLExposureSeed{
`)
	for _, exposure := range graphQLState.Exposures {
		for _, op := range exposure.Operations {
			fmt.Fprintf(&b, "\t{Model: %q, Operation: %q},\n", exposure.Model, op)
		}
	}
	b.WriteString(`}

var graphQLActionSeeds = []graphQLActionSeed{
`)
	for _, action := range graphQLState.Actions {
		fmt.Fprintf(&b, "\t{Name: %q},\n", action.Name)
	}
	b.WriteString(`}

var rolePolicyIDs = []string{
`)
	for _, id := range rolePolicyIDs {
		fmt.Fprintf(&b, "\t%q,\n", id)
	}
	b.WriteString(`}

var graphQLPolicyIDs = []string{
`)
	for _, id := range graphQLPolicyIDs {
		fmt.Fprintf(&b, "\t%q,\n", id)
	}
	b.WriteString(`}

func Migrate(db *gorm.DB, driver string) error {
	if len(roleSeeds) > 0 {
		if err := ensureRBACSchema(db); err != nil { return err }
		if err := seedRoles(db); err != nil { return err }
		if err := markPoliciesApplied(db, "rbac_changelog", rolePolicyIDs); err != nil { return err }
	}
	if len(graphQLExposureSeeds) > 0 || len(graphQLActionSeeds) > 0 {
		if err := ensureGraphQLPolicySchema(db); err != nil { return err }
		if err := seedGraphQLPolicies(db); err != nil { return err }
		if err := markPoliciesApplied(db, "graphql_changelog", graphQLPolicyIDs); err != nil { return err }
	}
	return nil
}

func Rollback(db *gorm.DB, driver string) error {
	if len(graphQLExposureSeeds) > 0 || len(graphQLActionSeeds) > 0 {
		if err := db.Exec("DELETE FROM graphql_actions").Error; err != nil { return err }
		if err := db.Exec("DELETE FROM graphql_exposures").Error; err != nil { return err }
		if err := db.Exec("DELETE FROM graphql_changelog").Error; err != nil { return err }
	}
	if len(roleSeeds) > 0 {
		if err := db.Exec("DELETE FROM role_actions").Error; err != nil { return err }
		if err := db.Exec("DELETE FROM role_user").Error; err != nil { return err }
		if err := db.Exec("DELETE FROM roles").Error; err != nil { return err }
		if err := db.Exec("DELETE FROM rbac_changelog").Error; err != nil { return err }
	}
	return nil
}

func Fresh(db *gorm.DB, driver string) error {
	_ = db.Exec("DROP TABLE IF EXISTS graphql_actions").Error
	_ = db.Exec("DROP TABLE IF EXISTS graphql_exposures").Error
	_ = db.Exec("DROP TABLE IF EXISTS graphql_changelog").Error
	_ = db.Exec("DROP TABLE IF EXISTS role_actions").Error
	_ = db.Exec("DROP TABLE IF EXISTS role_user").Error
	_ = db.Exec("DROP TABLE IF EXISTS roles").Error
	_ = db.Exec("DROP TABLE IF EXISTS rbac_changelog").Error
	return Migrate(db, driver)
}

func Status(db *gorm.DB, driver string) ([]PolicyStatus, error) {
	var statuses []PolicyStatus
	if len(roleSeeds) > 0 {
		if err := ensureRBACSchema(db); err != nil { return nil, err }
		applied, err := appliedPolicyRows(db, "rbac_changelog")
		if err != nil { return nil, err }
		for _, id := range rolePolicyIDs {
			status := PolicyStatus{ID: id, State: "pending"}
			if batch, ok := applied[id]; ok {
				status.Applied = true
				status.Batch = batch
				status.State = "applied"
			}
			statuses = append(statuses, status)
		}
	}
	if len(graphQLExposureSeeds) > 0 || len(graphQLActionSeeds) > 0 {
		if err := ensureGraphQLPolicySchema(db); err != nil { return nil, err }
		applied, err := appliedPolicyRows(db, "graphql_changelog")
		if err != nil { return nil, err }
		for _, id := range graphQLPolicyIDs {
			status := PolicyStatus{ID: "graphql:" + id, State: "pending"}
			if batch, ok := applied[id]; ok {
				status.Applied = true
				status.Batch = batch
				status.State = "applied"
			}
			statuses = append(statuses, status)
		}
	}
	return statuses, nil
}

func PrintStatus(statuses []PolicyStatus) {
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].ID < statuses[j].ID })
	for _, status := range statuses {
		state := "pending"
		if status.Applied { state = fmt.Sprintf("applied (batch %d)", status.Batch) }
		fmt.Printf("%-50s %s\n", status.ID, state)
	}
}

func ensureRBACSchema(db *gorm.DB) error {
	stmts := []string{
		` + "`" + `CREATE TABLE IF NOT EXISTS roles (id TEXT PRIMARY KEY, slug VARCHAR(50) NOT NULL UNIQUE, name VARCHAR(100) NOT NULL, manages BOOLEAN NOT NULL DEFAULT false, is_default BOOLEAN NOT NULL DEFAULT false, birth_policy VARCHAR(100) NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)` + "`" + `,
		` + "`" + `CREATE TABLE IF NOT EXISTS role_actions (id TEXT PRIMARY KEY, role_slug VARCHAR(50) NOT NULL, action VARCHAR(100) NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL, UNIQUE(role_slug, action))` + "`" + `,
		` + "`" + `CREATE TABLE IF NOT EXISTS role_user (user_id TEXT NOT NULL, role_id TEXT NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL, PRIMARY KEY(user_id, role_id))` + "`" + `,
		` + "`" + `CREATE TABLE IF NOT EXISTS rbac_changelog (id VARCHAR(255) PRIMARY KEY, batch INTEGER NOT NULL, state VARCHAR(20) NOT NULL, error TEXT, started_at DATETIME, completed_at DATETIME)` + "`" + `,
	}
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil { return err }
	}
	return nil
}

func ensureGraphQLPolicySchema(db *gorm.DB) error {
	stmts := []string{
		` + "`" + `CREATE TABLE IF NOT EXISTS graphql_changelog (id VARCHAR(255) PRIMARY KEY, batch INTEGER NOT NULL, state VARCHAR(20) NOT NULL, error TEXT, started_at DATETIME, completed_at DATETIME)` + "`" + `,
		` + "`" + `CREATE TABLE IF NOT EXISTS graphql_exposures (id TEXT PRIMARY KEY, model VARCHAR(100) NOT NULL, operation VARCHAR(20) NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL, UNIQUE(model, operation))` + "`" + `,
		` + "`" + `CREATE TABLE IF NOT EXISTS graphql_actions (id TEXT PRIMARY KEY, name VARCHAR(100) NOT NULL UNIQUE, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)` + "`" + `,
	}
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil { return err }
	}
	return nil
}

func seedRoles(db *gorm.DB) error {
	now := time.Now().UTC()
	for _, role := range roleSeeds {
		if err := db.Exec(roleUpsertSQL(db), uuid.New().String(), role.Slug, role.Name, role.Manages, role.Default, role.BirthPolicy, now, now).Error; err != nil { return err }
		if err := db.Exec("DELETE FROM role_actions WHERE role_slug = ?", role.Slug).Error; err != nil { return err }
		for _, action := range role.Actions {
			if err := db.Exec("INSERT INTO role_actions (id, role_slug, action, created_at, updated_at) VALUES (?, ?, ?, ?, ?)", uuid.New().String(), role.Slug, action, now, now).Error; err != nil { return err }
		}
	}
	return nil
}

func roleUpsertSQL(db *gorm.DB) string {
	return roleUpsertSQLForDialect(gormDialectName(db))
}

func roleUpsertSQLForDialect(dialect string) string {
	if dialect == "mysql" {
		return "INSERT INTO roles (id, slug, name, manages, is_default, birth_policy, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE name = VALUES(name), manages = VALUES(manages), is_default = VALUES(is_default), birth_policy = VALUES(birth_policy), updated_at = VALUES(updated_at)"
	}
	return "INSERT INTO roles (id, slug, name, manages, is_default, birth_policy, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(slug) DO UPDATE SET name = excluded.name, manages = excluded.manages, is_default = excluded.is_default, birth_policy = excluded.birth_policy, updated_at = excluded.updated_at"
}

func seedGraphQLPolicies(db *gorm.DB) error {
	now := time.Now().UTC()
	if err := db.Exec("DELETE FROM graphql_exposures").Error; err != nil { return err }
	for _, exposure := range graphQLExposureSeeds {
		if err := db.Exec("INSERT INTO graphql_exposures (id, model, operation, created_at, updated_at) VALUES (?, ?, ?, ?, ?)", uuid.New().String(), exposure.Model, exposure.Operation, now, now).Error; err != nil { return err }
	}
	if err := db.Exec("DELETE FROM graphql_actions").Error; err != nil { return err }
	for _, action := range graphQLActionSeeds {
		if err := db.Exec("INSERT INTO graphql_actions (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)", uuid.New().String(), action.Name, now, now).Error; err != nil { return err }
	}
	return nil
}

func markPoliciesApplied(db *gorm.DB, table string, ids []string) error {
	now := time.Now().UTC()
	for _, id := range ids {
		if err := db.Exec(policyAppliedUpsertSQL(db, table), id, 1, "applied", now, now).Error; err != nil { return err }
	}
	return nil
}

func policyAppliedUpsertSQL(db *gorm.DB, table string) string {
	return policyAppliedUpsertSQLForDialect(gormDialectName(db), table)
}

func policyAppliedUpsertSQLForDialect(dialect string, table string) string {
	if dialect == "mysql" {
		return "INSERT INTO " + table + " (id, batch, state, started_at, completed_at) VALUES (?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE state = VALUES(state), completed_at = VALUES(completed_at)"
	}
	return "INSERT INTO " + table + " (id, batch, state, started_at, completed_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET state = excluded.state, completed_at = excluded.completed_at"
}

func gormDialectName(db *gorm.DB) string {
	if db == nil || db.Dialector == nil { return "" }
	return db.Dialector.Name()
}

func appliedPolicyRows(db *gorm.DB, table string) (map[string]int, error) {
	rows, err := db.Raw("SELECT id, batch FROM " + table + " WHERE state = 'applied'").Rows()
	if err != nil { return nil, err }
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var batch int
		if err := rows.Scan(&id, &batch); err != nil { return nil, err }
		out[id] = batch
	}
	return out, rows.Err()
}
`)
	return format.Source([]byte(b.String()))
}

type sqlMigration struct {
	Name string
	Up   string
	Down string
}

func generateSQLMigrationSupport(migrations []sqlMigration) ([]byte, error) {
	var b strings.Builder
	b.WriteString(`package migrations

import (
	"embed"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

//go:embed *.sql
var migrationFiles embed.FS

type MigrationEntry struct {
	ID string
	UpFile string
	DownFile string
}

type MigrationStatus struct {
	ID string
	Batch int
	Applied bool
}

var Registry = []MigrationEntry{
`)
	for _, migration := range migrations {
		fmt.Fprintf(&b, "\t{ID: %q, UpFile: %q, DownFile: %q},\n", migration.Name, migration.Name+".up.sql", migration.Name+".down.sql")
	}
	b.WriteString(`}

type Runner struct {
	DB *gorm.DB
	Driver string
}

func NewRunner(db *gorm.DB, driver string) *Runner {
	return &Runner{DB: db, Driver: driver}
}

func (r *Runner) ensureMigrationsTable() error {
	return r.DB.Exec(migrationsTableSQL(r.Driver)).Error
}

func migrationsTableSQL(driver string) string {
	switch driver {
	case "pgsql", "postgres":
		return ` + "`" + `CREATE TABLE IF NOT EXISTS migrations (
			id SERIAL PRIMARY KEY,
			migration VARCHAR(255) NOT NULL,
			batch INTEGER NOT NULL
		)` + "`" + `
	case "mysql":
		return ` + "`" + `CREATE TABLE IF NOT EXISTS migrations (
			id INTEGER PRIMARY KEY AUTO_INCREMENT,
			migration VARCHAR(255) NOT NULL,
			batch INTEGER NOT NULL
		)` + "`" + `
	default:
		return ` + "`" + `CREATE TABLE IF NOT EXISTS migrations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		migration VARCHAR(255) NOT NULL,
		batch INTEGER NOT NULL
	)` + "`" + `
	}
}

func (r *Runner) applied() (map[string]int, error) {
	if err := r.ensureMigrationsTable(); err != nil {
		return nil, err
	}
	rows, err := r.DB.Raw("SELECT migration, batch FROM migrations").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var batch int
		if err := rows.Scan(&id, &batch); err != nil {
			return nil, err
		}
		out[id] = batch
	}
	return out, rows.Err()
}

func nextBatch(applied map[string]int) int {
	maxBatch := 0
	for _, batch := range applied {
		if batch > maxBatch {
			maxBatch = batch
		}
	}
	return maxBatch + 1
}

func (r *Runner) Migrate(entries []MigrationEntry) error {
	applied, err := r.applied()
	if err != nil {
		return err
	}
	batch := nextBatch(applied)
	ran := 0
	for _, entry := range entries {
		if _, ok := applied[entry.ID]; ok {
			continue
		}
		fmt.Printf("  migrating: %s\n", entry.ID)
		if err := r.execMigrationFile(entry.UpFile); err != nil {
			return fmt.Errorf("migrating %s: %w", entry.ID, err)
		}
		if err := r.DB.Exec("INSERT INTO migrations (migration, batch) VALUES (?, ?)", entry.ID, batch).Error; err != nil {
			return fmt.Errorf("recording %s: %w", entry.ID, err)
		}
		fmt.Printf("  migrated:  %s\n", entry.ID)
		ran++
	}
	if ran == 0 {
		fmt.Println("  nothing to migrate")
	}
	return nil
}

func (r *Runner) Rollback(entries []MigrationEntry) error {
	applied, err := r.applied()
	if err != nil {
		return err
	}
	maxBatch := 0
	for _, batch := range applied {
		if batch > maxBatch {
			maxBatch = batch
		}
	}
	if maxBatch == 0 {
		fmt.Println("  nothing to roll back")
		return nil
	}
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		batch, ok := applied[entry.ID]
		if !ok || batch != maxBatch {
			continue
		}
		fmt.Printf("  rolling back: %s\n", entry.ID)
		if err := r.execMigrationFile(entry.DownFile); err != nil {
			return fmt.Errorf("rolling back %s: %w", entry.ID, err)
		}
		if err := r.DB.Exec("DELETE FROM migrations WHERE migration = ?", entry.ID).Error; err != nil {
			return fmt.Errorf("removing %s: %w", entry.ID, err)
		}
		fmt.Printf("  rolled back: %s\n", entry.ID)
	}
	return nil
}

func (r *Runner) Fresh(entries []MigrationEntry) error {
	for i := len(entries) - 1; i >= 0; i-- {
		_ = r.execMigrationFile(entries[i].DownFile)
	}
	if err := r.DB.Exec("DROP TABLE IF EXISTS migrations").Error; err != nil {
		return err
	}
	return r.Migrate(entries)
}

func (r *Runner) Status(entries []MigrationEntry) ([]MigrationStatus, error) {
	applied, err := r.applied()
	if err != nil {
		return nil, err
	}
	statuses := make([]MigrationStatus, 0, len(entries))
	for _, entry := range entries {
		status := MigrationStatus{ID: entry.ID}
		if batch, ok := applied[entry.ID]; ok {
			status.Applied = true
			status.Batch = batch
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func PrintStatus(statuses []MigrationStatus) {
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].ID < statuses[j].ID })
	for _, status := range statuses {
		state := "pending"
		if status.Applied {
			state = fmt.Sprintf("applied (batch %d)", status.Batch)
		}
		fmt.Printf("%-50s %s\n", status.ID, state)
	}
}

func (r *Runner) execMigrationFile(name string) error {
	data, err := migrationFiles.ReadFile(name)
	if err != nil {
		return err
	}
	sql := normalizeSQLForDriver(string(data), r.Driver)
	for _, statement := range splitSQLStatements(sql) {
		if err := r.DB.Exec(statement).Error; err != nil {
			return fmt.Errorf("executing %q: %w", statement, err)
		}
	}
	return nil
}

func normalizeSQLForDriver(sql, driver string) string {
	switch driver {
	case "sqlite", "sqlite3":
		return normalizeSQLForSQLite(sql)
	case "mysql":
		return normalizeSQLForMySQL(sql)
	default:
		return sql
	}
}

func normalizeSQLForSQLite(sql string) string {
	replacements := map[string]string{
		" UUID": " TEXT",
		" JSONB": " TEXT",
		" BYTEA": " BLOB",
		" TIMESTAMPTZ": " DATETIME",
		" DEFAULT NOW()": " DEFAULT CURRENT_TIMESTAMP",
		" DEFAULT gen_random_uuid()": "",
		" CASCADE": "",
	}
	for from, to := range replacements {
		sql = strings.ReplaceAll(sql, from, to)
	}
	return sql
}

func normalizeSQLForMySQL(sql string) string {
	sql = strings.ReplaceAll(sql, string(rune(34)), string(rune(96)))
	replacements := map[string]string{
		" UUID": " CHAR(36)",
		" JSONB": " JSON",
		" BYTEA": " BLOB",
		" TIMESTAMPTZ": " DATETIME",
		" DEFAULT NOW()": " DEFAULT CURRENT_TIMESTAMP",
		" DEFAULT gen_random_uuid()": "",
		" CASCADE": "",
	}
	for from, to := range replacements {
		sql = strings.ReplaceAll(sql, from, to)
	}
	return sql
}

func splitSQLStatements(sql string) []string {
	var statements []string
	for _, part := range strings.Split(sql, ";") {
		stmt := strings.TrimSpace(part)
		if stmt != "" {
			statements = append(statements, stmt)
		}
	}
	return statements
}
`)
	return format.Source([]byte(b.String()))
}

func (e *exporter) generateCommandsSupport() ([]byte, error) {
	hasGraphQL := e.hasGraphQLPackage()
	hasSchedule := e.hasSchedule()
	hasPolicies := e.hasPolicySupport()
	var b strings.Builder
	b.WriteString("package commands\n\n")
	b.WriteString("import (\n")
	if hasSchedule {
		b.WriteString("\t\"context\"\n")
	}
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"log\"\n")
	b.WriteString("\t\"net/http\"\n")
	if hasSchedule {
		b.WriteString("\t\"os\"\n")
		b.WriteString("\t\"os/signal\"\n")
	}
	b.WriteString("\t\"time\"\n\n")
	if hasGraphQL {
		b.WriteString(fmt.Sprintf("\t\"%s/app/graphql\"\n", e.modulePath))
	}
	b.WriteString(fmt.Sprintf("\t\"%s/app/http/auth\"\n", e.modulePath))
	b.WriteString(fmt.Sprintf("\t\"%s/app/models\"\n", e.modulePath))
	b.WriteString(fmt.Sprintf("\t\"%s/config\"\n", e.modulePath))
	b.WriteString(fmt.Sprintf("\t\"%s/database/migrations\"\n", e.modulePath))
	if hasPolicies {
		b.WriteString(fmt.Sprintf("\t\"%s/database/policies\"\n", e.modulePath))
	}
	b.WriteString(fmt.Sprintf("\t\"%s/routes\"\n", e.modulePath))
	if hasSchedule {
		b.WriteString(fmt.Sprintf("\t\"%s/schedule\"\n", e.modulePath))
	}
	b.WriteString(")\n\n")
	b.WriteString(`type Command interface {
	Name() string
	Description() string
	Run(args []string) error
}

type App struct {
	commands map[string]Command
	initFn func()
	serveFn func()
}

func BuildApp(initFn func(), serveFn func(), cmds ...Command) *App {
	app := &App{commands: map[string]Command{}, initFn: initFn, serveFn: serveFn}
	for _, cmd := range cmds {
		app.commands[cmd.Name()] = cmd
	}
	return app
}

func (a *App) Run(args []string) {
	a.initFn()
	if len(args) > 0 {
		cmd, ok := a.commands[args[0]]
		if !ok {
			a.PrintCommands()
			log.Fatalf("unknown command: %s", args[0])
		}
		if err := cmd.Run(args[1:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	a.serveFn()
}

func (a *App) PrintCommands() {
	fmt.Println("Available commands:")
	for name, cmd := range a.commands {
		fmt.Printf("  %-25s %s\n", name, cmd.Description())
	}
}

type migrateCommand struct{}

func (c migrateCommand) Name() string { return "migrate" }
func (c migrateCommand) Description() string { return "Run pending migrations" }
func (c migrateCommand) Run(args []string) error {
	runner := migrations.NewRunner(models.DB, config.Database.Connection().Driver)
	if err := runner.Migrate(migrations.Registry); err != nil {
		return err
	}
`)
	if hasPolicies {
		b.WriteString("\treturn policies.Migrate(models.DB, config.Database.Connection().Driver)\n")
	} else {
		b.WriteString("\treturn nil\n")
	}
	b.WriteString(`
}

type migrateRollbackCommand struct{}

func (c migrateRollbackCommand) Name() string { return "migrate:rollback" }
func (c migrateRollbackCommand) Description() string { return "Roll back the last migration batch" }
func (c migrateRollbackCommand) Run(args []string) error {
	runner := migrations.NewRunner(models.DB, config.Database.Connection().Driver)
`)
	if hasPolicies {
		b.WriteString("\tif err := policies.Rollback(models.DB, config.Database.Connection().Driver); err != nil {\n\t\treturn err\n\t}\n")
	}
	b.WriteString(`
	return runner.Rollback(migrations.Registry)
}

type migrateFreshCommand struct{}

func (c migrateFreshCommand) Name() string { return "migrate:fresh" }
func (c migrateFreshCommand) Description() string { return "Drop all tables and re-run all migrations" }
func (c migrateFreshCommand) Run(args []string) error {
	runner := migrations.NewRunner(models.DB, config.Database.Connection().Driver)
`)
	if hasPolicies {
		b.WriteString("\tif err := policies.Fresh(models.DB, config.Database.Connection().Driver); err != nil {\n\t\treturn err\n\t}\n")
	}
	b.WriteString(`	if err := runner.Fresh(migrations.Registry); err != nil {
		return err
	}
`)
	if hasPolicies {
		b.WriteString("\treturn policies.Migrate(models.DB, config.Database.Connection().Driver)\n")
	} else {
		b.WriteString("\treturn nil\n")
	}
	b.WriteString(`
}

type migrateStatusCommand struct{}

func (c migrateStatusCommand) Name() string { return "migrate:status" }
func (c migrateStatusCommand) Description() string { return "Show migration status" }
func (c migrateStatusCommand) Run(args []string) error {
	runner := migrations.NewRunner(models.DB, config.Database.Connection().Driver)
	statuses, err := runner.Status(migrations.Registry)
	if err != nil {
		return err
	}
	migrations.PrintStatus(statuses)
`)
	if hasPolicies {
		b.WriteString(`	policyStatuses, err := policies.Status(models.DB, config.Database.Connection().Driver)
	if err != nil {
		return err
	}
	policies.PrintStatus(policyStatuses)
`)
	}
	b.WriteString(`
	return nil
}

func BuiltinCommands() []Command {
	return []Command{
		migrateCommand{},
		migrateRollbackCommand{},
		migrateFreshCommand{},
		migrateStatusCommand{},
	}
}

func UserCommands() []Command {
	return []Command{}
}

func Commands() []Command {
	return append(BuiltinCommands(), UserCommands()...)
}

func NewApp() *App {
	return BuildApp(
		func() {
			config.Init()
			db := config.Database.OpenGORM()
			models.SetDB(db)
			sqlDB, err := db.DB()
			if err != nil {
				log.Fatalf("commands: failed to unwrap database handle: %v", err)
			}
			auth.Init(config.Env, sqlDB)
		},
		func() {
`)
	if hasSchedule {
		b.WriteString("\t\t\tctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)\n")
		b.WriteString("\t\t\tdefer stop()\n")
		b.WriteString("\t\t\tgo schedule.Schedule.Start(ctx)\n")
	}
	b.WriteString(`			mux := http.NewServeMux()
			routes.API.RegisterRoutes(mux)
`)
	if hasGraphQL {
		b.WriteString("\t\t\tmux.Handle(\"/graphql\", graphql.Handler())\n")
		b.WriteString("\t\t\tmux.Handle(\"/graphql/playground\", graphql.PlaygroundHandler(\"/graphql\"))\n")
	}
	b.WriteString(`			addr := ":" + config.App.Port
			log.Printf("listening on %s", addr)
			server := &http.Server{
				Addr: addr,
				Handler: mux,
				ReadHeaderTimeout: 10 * time.Second,
				ReadTimeout: 30 * time.Second,
				WriteTimeout: 60 * time.Second,
				IdleTimeout: 120 * time.Second,
			}
			if err := server.ListenAndServe(); err != nil {
				log.Fatal(err)
			}
		},
		Commands()...,
	)
}
`)
	return format.Source([]byte(b.String()))
}

func (e *exporter) generateSQLMigrations(tables []*schema.Table, views []*schema.View) ([]sqlMigration, error) {
	if len(e.migrations) > 0 {
		var out []sqlMigration
		for _, migration := range e.migrations {
			up, err := sqlForMigrationOps(migration.Up)
			if err != nil {
				return nil, fmt.Errorf("unsupported migration export for %s: %w", migration.Name, err)
			}
			down, err := sqlForMigrationOps(migration.Down)
			if err != nil {
				return nil, fmt.Errorf("unsupported migration export for %s: %w", migration.Name, err)
			}
			if migrationHasRawSQL(migration) {
				e.result.Findings = append(e.result.Findings, Finding{File: "database/migrations", Rule: "raw_sql_migration", Message: fmt.Sprintf("migration %s contains raw SQL; exported statements need driver-specific review", migration.Name)})
			}
			out = append(out, sqlMigration{Name: migrationExportNameFromStruct(migration.Name), Up: up, Down: down})
		}
		return out, nil
	}

	byName := map[string]*schema.Table{}
	for _, table := range tables {
		byName[table.Name] = table
	}
	viewsByName := map[string]*schema.View{}
	for _, view := range views {
		viewsByName[view.Name] = view
	}
	entries, err := os.ReadDir(e.project.Layout.MigrationsDir)
	if err != nil {
		return nil, err
	}
	var out []sqlMigration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_gen.go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), ".go")
		exportName := migrationExportName(base)
		operation := migrationOperationName(base)
		switch {
		case strings.HasPrefix(operation, "create_") && strings.HasSuffix(operation, "_table"):
			tableName := strings.TrimSuffix(strings.TrimPrefix(operation, "create_"), "_table")
			table, ok := byName[tableName]
			if !ok {
				return nil, fmt.Errorf("migration %s references unknown table %s", entry.Name(), tableName)
			}
			out = append(out, sqlMigration{Name: exportName, Up: createTableSQL(table) + tableIndexesSQL(table) + ";\n", Down: dropTableSQL(table.Name) + ";\n"})
		case strings.HasPrefix(operation, "create_") && strings.HasSuffix(operation, "_view"):
			viewName := strings.TrimSuffix(strings.TrimPrefix(operation, "create_"), "_view")
			view, ok := viewsByName[viewName]
			if !ok {
				return nil, fmt.Errorf("migration %s references unknown view %s", entry.Name(), viewName)
			}
			out = append(out, sqlMigration{Name: exportName, Up: createViewSQL(view) + ";\n", Down: dropViewSQL(view.Name) + ";\n"})
		default:
			return nil, unsupportedMigrationError(entry.Name(), operation)
		}
	}
	return out, nil
}

func migrationHasRawSQL(migration generator.MigrationOps) bool {
	for _, op := range append(migration.Up, migration.Down...) {
		if op.Type == "raw_sql" {
			return true
		}
	}
	return false
}

func sqlForMigrationOps(ops []generator.MigrationOperation) (string, error) {
	var statements []string
	for _, op := range ops {
		sql, err := sqlForMigrationOp(op)
		if err != nil {
			return "", err
		}
		if sql != "" {
			statements = append(statements, sql)
		}
	}
	if len(statements) == 0 {
		return "", nil
	}
	return strings.Join(statements, ";\n") + ";\n", nil
}

func sqlForMigrationOp(op generator.MigrationOperation) (string, error) {
	switch op.Type {
	case "create_table":
		if op.TableDef == nil {
			return "", fmt.Errorf("create_table missing table definition")
		}
		return createTableSQL(op.TableDef) + tableIndexesSQL(op.TableDef), nil
	case "drop_table_if_exists":
		return dropTableSQL(op.Table), nil
	case "rename_table":
		return "ALTER TABLE " + quoteIdent(op.OldName) + " RENAME TO " + quoteIdent(op.NewName), nil
	case "add_column":
		if len(op.Columns) == 0 {
			return "", fmt.Errorf("add_column on %s did not define a column", op.Table)
		}
		var statements []string
		for _, col := range op.Columns {
			statements = append(statements, "ALTER TABLE "+quoteIdent(op.Table)+" ADD COLUMN "+columnSQL(col, false))
		}
		return strings.Join(statements, ";\n"), nil
	case "drop_column":
		return "ALTER TABLE " + quoteIdent(op.Table) + " DROP COLUMN " + quoteIdent(op.ColumnName), nil
	case "rename_column":
		return "ALTER TABLE " + quoteIdent(op.Table) + " RENAME COLUMN " + quoteIdent(op.OldName) + " TO " + quoteIdent(op.NewName), nil
	case "add_index", "add_unique_index":
		if op.Index == nil {
			return "", fmt.Errorf("%s on %s missing index definition", op.Type, op.Table)
		}
		idx := *op.Index
		if idx.Table == "" {
			idx.Table = op.Table
		}
		return createIndexSQL(&idx), nil
	case "create_view":
		if op.ViewDef == nil {
			return "", fmt.Errorf("create_view missing view definition")
		}
		return createViewSQL(op.ViewDef), nil
	case "drop_view":
		return dropViewSQL(op.Table), nil
	case "raw_sql":
		if strings.TrimSpace(op.SQL) == "" {
			return "", fmt.Errorf("raw_sql operation missing SQL")
		}
		return strings.TrimRight(strings.TrimSpace(op.SQL), ";"), nil
	default:
		return "", fmt.Errorf("%s migrations are not lowered yet", op.Type)
	}
}

func unsupportedMigrationError(fileName, operation string) error {
	var kind string
	switch {
	case strings.HasPrefix(operation, "add_"):
		kind = "add-column/index"
	case strings.HasPrefix(operation, "drop_"):
		kind = "drop-column/table/view"
	case strings.HasPrefix(operation, "rename_"):
		kind = "rename-table/column"
	case strings.Contains(operation, "index"):
		kind = "index"
	case strings.Contains(operation, "raw") || strings.Contains(operation, "sql"):
		kind = "raw-sql"
	default:
		kind = "unknown"
	}
	return fmt.Errorf("unsupported migration export for %s: %s migrations are not lowered yet", fileName, kind)
}

func migrationExportName(base string) string {
	parts := strings.SplitN(base, "_", 5)
	if len(parts) < 5 {
		return base
	}
	return parts[0] + parts[1] + parts[2] + parts[3] + "_" + parts[4]
}

func migrationExportNameFromStruct(name string) string {
	const suffixLen = len("_2006_01_02_150405")
	if len(name) <= suffixLen || name[len(name)-suffixLen] != '_' {
		return pascalToSnake(name)
	}
	prefix := name[:len(name)-suffixLen]
	timestamp := strings.ReplaceAll(name[len(name)-suffixLen+1:], "_", "")
	return timestamp + "_" + pascalToSnake(prefix)
}

func migrationOperationName(base string) string {
	parts := strings.SplitN(base, "_", 5)
	if len(parts) < 5 {
		return base
	}
	return parts[4]
}

func createTableSQL(table *schema.Table) string {
	var cols []string
	var pk []string
	for _, col := range table.Columns {
		if col.IsPrimaryKey {
			pk = append(pk, quoteIdent(col.Name))
		}
	}
	compositePrimaryKey := len(pk) > 1
	for _, col := range table.Columns {
		cols = append(cols, "\t"+columnSQL(col, compositePrimaryKey))
	}
	if len(pk) > 1 {
		cols = append(cols, "\tPRIMARY KEY ("+strings.Join(pk, ", ")+")")
	}
	return "CREATE TABLE " + quoteIdent(table.Name) + " (\n" + strings.Join(cols, ",\n") + "\n)"
}

func dropTableSQL(name string) string {
	return "DROP TABLE IF EXISTS " + quoteIdent(name) + " CASCADE"
}

func tableIndexesSQL(table *schema.Table) string {
	if len(table.Indexes) == 0 {
		return ""
	}
	var parts []string
	for _, idx := range table.Indexes {
		parts = append(parts, createIndexSQL(idx))
	}
	return ";\n" + strings.Join(parts, ";\n")
}

func createIndexSQL(idx *schema.Index) string {
	var cols []string
	for _, col := range idx.Columns {
		cols = append(cols, quoteIdent(col))
	}
	unique := ""
	if idx.Unique {
		unique = "UNIQUE "
	}
	return fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)", unique, quoteIdent(indexName(idx)), quoteIdent(idx.Table), strings.Join(cols, ", "))
}

func indexName(idx *schema.Index) string {
	kind := "idx"
	if idx.Unique {
		kind = "uidx"
	}
	return kind + "_" + idx.Table + "_" + strings.Join(idx.Columns, "_")
}

func createViewSQL(view *schema.View) string {
	var selectCols []string
	for _, col := range view.Columns {
		if col.RawExpr != "" {
			selectCols = append(selectCols, "\t"+col.RawExpr+" AS "+quoteIdent(col.OutputName()))
			continue
		}
		expr := quoteIdent(col.SourceAlias) + "." + quoteIdent(col.SourceColumn)
		if col.OutputAlias != "" && col.OutputAlias != col.SourceColumn {
			expr += " AS " + quoteIdent(col.OutputAlias)
		}
		selectCols = append(selectCols, "\t"+expr)
	}

	var fromParts []string
	for i, src := range view.Sources {
		if i == 0 && src.JoinType == "" {
			fromParts = append(fromParts, quoteIdent(src.Table)+" AS "+quoteIdent(src.Alias))
			continue
		}
		joinType := src.JoinType
		if joinType == "" {
			joinType = "JOIN"
		}
		fromParts = append(fromParts, joinType+" "+quoteIdent(src.Table)+" AS "+quoteIdent(src.Alias)+" ON "+src.JoinCondition)
	}

	var b strings.Builder
	b.WriteString("CREATE VIEW ")
	b.WriteString(quoteIdent(view.Name))
	b.WriteString(" AS\nSELECT\n")
	b.WriteString(strings.Join(selectCols, ",\n"))
	b.WriteString("\nFROM ")
	b.WriteString(strings.Join(fromParts, "\n"))
	if len(view.GroupByCols) > 0 {
		b.WriteString("\nGROUP BY ")
		b.WriteString(strings.Join(view.GroupByCols, ", "))
	}
	return b.String()
}

func dropViewSQL(name string) string {
	return "DROP VIEW IF EXISTS " + quoteIdent(name)
}

func columnSQL(col *schema.Column, compositePrimaryKey bool) string {
	var b strings.Builder
	b.WriteString(quoteIdent(col.Name))
	b.WriteByte(' ')
	b.WriteString(sqlType(col))
	if col.IsPrimaryKey && !compositePrimaryKey {
		b.WriteString(" PRIMARY KEY")
	}
	if !col.IsNullable && !col.IsPrimaryKey {
		b.WriteString(" NOT NULL")
	}
	if col.IsUnique {
		b.WriteString(" UNIQUE")
	}
	if col.HasDefault {
		if s, ok := col.DefaultValue.(string); ok {
			if strings.Contains(s, "(") {
				b.WriteString(" DEFAULT " + s)
			} else {
				b.WriteString(" DEFAULT '" + strings.ReplaceAll(s, "'", "''") + "'")
			}
		} else {
			b.WriteString(fmt.Sprintf(" DEFAULT %v", col.DefaultValue))
		}
	}
	if col.ForeignKeyTable != "" && !col.FKMetadataOnly {
		b.WriteString(" REFERENCES " + quoteIdent(col.ForeignKeyTable) + "(" + quoteIdent(col.ForeignKeyColumn) + ")")
		if col.OnDeleteAction != "" {
			b.WriteString(" ON DELETE " + col.OnDeleteAction)
		}
	}
	return b.String()
}

func sqlType(col *schema.Column) string {
	switch col.Type {
	case schema.UUID:
		return "UUID"
	case schema.String:
		if col.Length > 0 {
			return fmt.Sprintf("VARCHAR(%d)", col.Length)
		}
		return "VARCHAR(255)"
	case schema.Text:
		return "TEXT"
	case schema.Integer:
		return "INTEGER"
	case schema.BigInteger:
		return "BIGINT"
	case schema.Decimal:
		if col.Precision > 0 {
			return fmt.Sprintf("NUMERIC(%d, %d)", col.Precision, col.Scale)
		}
		return "NUMERIC"
	case schema.Boolean:
		return "BOOLEAN"
	case schema.Timestamp:
		return "TIMESTAMPTZ"
	case schema.JSONB:
		return "JSONB"
	case schema.Date:
		return "DATE"
	case schema.Time:
		return "TIME"
	case schema.Binary:
		return "BYTEA"
	case schema.Float:
		return "REAL"
	case schema.Double:
		return "DOUBLE PRECISION"
	}
	return "TEXT"
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func (e *exporter) writeBindings() error {
	if len(e.project.Services) > 0 {
		for _, svc := range e.project.Services {
			requests, err := generator.ScanRequests(svc.RequestsDir)
			if err != nil {
				if os.IsNotExist(err) || strings.Contains(err.Error(), "no such file") {
					continue
				}
				return err
			}
			data, err := generateBindings(requests)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(e.project.Dir, svc.RequestsDir)
			if err != nil {
				return err
			}
			if err := e.writeFile(filepath.Join(rel, "bindings.go"), data); err != nil {
				return err
			}
		}
		return nil
	}
	requests, err := generator.ScanRequests(e.project.Layout.RequestsDir)
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "no such file") {
			return nil
		}
		return err
	}
	data, err := generateBindings(requests)
	if err != nil {
		return err
	}
	return e.writeFile(filepath.Join("app", "http", "requests", "bindings.go"), data)
}

func (e *exporter) writeJobsSupport() error {
	jobsDir := filepath.Join(e.project.Dir, "app", "jobs")
	if _, err := os.Stat(jobsDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return e.writeFile(filepath.Join("app", "jobs", "support.go"), []byte(jobsSupportSource))
}

func (e *exporter) writeConfigSupport() error {
	scan, err := generator.ScanConfigs(e.project.Layout.ConfigDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	data, err := generateConfigSupport(scan)
	if err != nil {
		return err
	}
	return e.writeFile(filepath.Join("config", "support.go"), data)
}

func (e *exporter) writeAuthSupport() error {
	if err := e.writeFile(filepath.Join("app", "http", "auth", "auth.go"), []byte(fmt.Sprintf(authSupportSource, e.modulePath, e.modulePath, e.modulePath, e.modulePath, e.modulePath))); err != nil {
		return err
	}
	if err := e.writeFile(filepath.Join("app", "http", "auth", "jwt", "jwt.go"), []byte(fmt.Sprintf(jwtSupportSource, e.modulePath))); err != nil {
		return err
	}
	if err := e.writeFile(filepath.Join("app", "http", "auth", "oauth", "oauth.go"), []byte(fmt.Sprintf(oauthSupportSource, e.modulePath))); err != nil {
		return err
	}
	return e.writeFile(filepath.Join("app", "http", "auth", "session", "session.go"), []byte(fmt.Sprintf(sessionSupportSource, e.modulePath)))
}

func (e *exporter) writeServerMain() error {
	scan, err := generator.ScanConfigs(e.project.Layout.ConfigDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if e.hasCommands() {
		return e.writeFile(filepath.Join("cmd", "server", "main.go"), []byte(fmt.Sprintf(commandsServerMainSource, e.modulePath)))
	}
	hasSchedule := e.hasSchedule()
	if len(e.project.Services) > 0 {
		data, err := e.generateMultiServiceServerMain(scan != nil && scan.HasDatabaseConfig, hasSchedule)
		if err != nil {
			return err
		}
		return e.writeFile(filepath.Join("cmd", "server", "main.go"), data)
	}
	if e.hasGraphQLPackage() {
		data, err := e.generateServerMain(scan != nil && scan.HasDatabaseConfig, true, hasSchedule)
		if err != nil {
			return err
		}
		return e.writeFile(filepath.Join("cmd", "server", "main.go"), data)
	}
	data, err := e.generateServerMain(scan != nil && scan.HasDatabaseConfig, false, hasSchedule)
	if err != nil {
		return err
	}
	return e.writeFile(filepath.Join("cmd", "server", "main.go"), data)
}

func (e *exporter) hasSchedule() bool {
	_, err := os.Stat(filepath.Join(e.project.Dir, "schedule", "jobs.go"))
	return err == nil
}

func (e *exporter) generateServerMain(hasDatabaseConfig, hasGraphQL, hasSchedule bool) ([]byte, error) {
	var b strings.Builder
	b.WriteString("package main\n\n")
	b.WriteString("import (\n")
	if hasSchedule {
		b.WriteString("\t\"context\"\n")
	}
	b.WriteString("\t\"log\"\n")
	b.WriteString("\t\"net/http\"\n")
	if hasSchedule {
		b.WriteString("\t\"os\"\n")
		b.WriteString("\t\"os/signal\"\n")
	}
	b.WriteString("\n")
	if hasGraphQL {
		b.WriteString(fmt.Sprintf("\t\"%s/app/graphql\"\n", e.modulePath))
	}
	if hasDatabaseConfig {
		b.WriteString(fmt.Sprintf("\t\"%s/app/models\"\n", e.modulePath))
	}
	b.WriteString(fmt.Sprintf("\t\"%s/config\"\n", e.modulePath))
	b.WriteString(fmt.Sprintf("\t\"%s/routes\"\n", e.modulePath))
	if hasSchedule {
		b.WriteString(fmt.Sprintf("\t\"%s/schedule\"\n", e.modulePath))
	}
	b.WriteString(")\n\n")
	b.WriteString("func main() {\n")
	b.WriteString("\tconfig.Init()\n")
	if hasDatabaseConfig {
		b.WriteString("\tmodels.SetDB(config.Database.OpenGORM())\n")
	}
	if hasSchedule {
		b.WriteString("\tctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)\n")
		b.WriteString("\tdefer stop()\n")
		b.WriteString("\tgo schedule.Schedule.Start(ctx)\n")
	}
	b.WriteString("\tmux := http.NewServeMux()\n")
	b.WriteString("\troutes.API.RegisterRoutes(mux)\n")
	if hasGraphQL {
		b.WriteString("\tmux.Handle(\"/graphql\", graphql.Handler())\n")
		b.WriteString("\tmux.Handle(\"/graphql/playground\", graphql.PlaygroundHandler(\"/graphql\"))\n")
	}
	b.WriteString("\taddr := \":\" + config.App.Port\n")
	b.WriteString("\tlog.Printf(\"listening on %s\", addr)\n")
	b.WriteString("\tif err := http.ListenAndServe(addr, mux); err != nil {\n")
	b.WriteString("\t\tlog.Fatal(err)\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n")
	return format.Source([]byte(b.String()))
}

func (e *exporter) generateMultiServiceServerMain(hasDatabaseConfig, hasSchedule bool) ([]byte, error) {
	var b strings.Builder
	b.WriteString("package main\n\n")
	b.WriteString("import (\n")
	if hasSchedule {
		b.WriteString("\t\"context\"\n")
	}
	b.WriteString("\t\"log\"\n")
	b.WriteString("\t\"net/http\"\n")
	if hasSchedule {
		b.WriteString("\t\"os\"\n")
		b.WriteString("\t\"os/signal\"\n")
	}
	b.WriteString("\n")
	if hasDatabaseConfig {
		b.WriteString(fmt.Sprintf("\t\"%s/app/models\"\n", e.modulePath))
	}
	b.WriteString(fmt.Sprintf("\t\"%s/config\"\n", e.modulePath))
	if hasSchedule {
		b.WriteString(fmt.Sprintf("\t\"%s/schedule\"\n", e.modulePath))
	}
	for _, svc := range e.project.Services {
		rel, err := filepath.Rel(e.project.Dir, filepath.Join(svc.Dir, "routes"))
		if err != nil {
			return nil, err
		}
		b.WriteString(fmt.Sprintf("\t%sRoutes \"%s/%s\"\n", safeImportAlias(svc.Name), e.modulePath, filepath.ToSlash(rel)))
	}
	b.WriteString(")\n\n")
	b.WriteString("func main() {\n")
	b.WriteString("\tconfig.Init()\n")
	if hasDatabaseConfig {
		b.WriteString("\tmodels.SetDB(config.Database.OpenGORM())\n")
	}
	if hasSchedule {
		b.WriteString("\tctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)\n")
		b.WriteString("\tdefer stop()\n")
		b.WriteString("\tgo schedule.Schedule.Start(ctx)\n")
	}
	b.WriteString("\tmux := http.NewServeMux()\n")
	for i, svc := range e.project.Services {
		prefix := "/" + strings.Trim(svc.Name, "/") + "/"
		if i == 0 && svc.Name == "api" {
			b.WriteString(fmt.Sprintf("\t%sRoutes.API.RegisterRoutes(mux)\n", safeImportAlias(svc.Name)))
			continue
		}
		stripPrefix := strings.TrimSuffix(prefix, "/")
		b.WriteString(fmt.Sprintf("\tmux.Handle(%q, http.StripPrefix(%q, %sRoutes.API))\n", prefix, stripPrefix, safeImportAlias(svc.Name)))
	}
	b.WriteString("\taddr := \":\" + config.App.Port\n")
	b.WriteString("\tlog.Printf(\"listening on %s\", addr)\n")
	b.WriteString("\tif err := http.ListenAndServe(addr, mux); err != nil {\n")
	b.WriteString("\t\tlog.Fatal(err)\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n")
	return format.Source([]byte(b.String()))
}

func safeImportAlias(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "service"
	}
	return b.String()
}

func (e *exporter) addSchemaFindings(tables []*schema.Table) {
}

func (e *exporter) writeReport(orm string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Export Report\n\n")
	fmt.Fprintf(&b, "Source project: `%s`\n\n", e.project.Dir)
	fmt.Fprintf(&b, "Exported module: `%s`\n\n", e.modulePath)
	fmt.Fprintf(&b, "Target ORM: `%s`\n\n", orm)
	fmt.Fprintf(&b, "Generated at: `%s`\n\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "Files written: `%d`\n\n", e.result.FilesWritten)
	b.WriteString("## Exported\n\n")
	b.WriteString("- Standalone Go module with rewritten imports and no Pickle runtime dependency\n")
	b.WriteString("- GORM models and database handle setup\n")
	b.WriteString("- SQL migrations for supported schema operations\n")
	b.WriteString("- HTTP routing, request binding, auth, config, and server support\n")
	if e.hasGraphQLPackage() {
		b.WriteString("- Generated GraphQL package with standalone GORM query support and /graphql server mount\n")
	}
	if e.hasEncryptedColumns {
		b.WriteString("- Encrypted and sealed columns with GORM encrypt/decrypt hooks\n")
	}
	if len(e.integrityModels) > 0 {
		b.WriteString("- Immutable and append-only integrity tables with hash-chain write and verification helpers\n")
	}
	if e.hasSchedule() {
		b.WriteString("- Cron job scheduler support with exported server startup wiring\n")
	}
	if e.hasCommands() {
		b.WriteString("- Standalone command dispatch with embedded SQL migration commands\n")
	}
	b.WriteString("- Standalone JWT, OAuth client-credentials, and session auth drivers\n")
	if e.hasPolicySupport() {
		b.WriteString("- Standalone RBAC and GraphQL policy state support with changelog tables\n")
	}
	b.WriteString("\n")
	e.writeUnsupportedSection(&b)
	if len(e.result.Findings) > 0 {
		e.writeFindingSection(&b, "Partial Support", "partial")
		e.writeFindingSection(&b, "Omitted", "omitted")
		e.writeFindingSection(&b, "Manual Review", "manual")
	}
	rel, err := filepath.Rel(e.outDir, e.result.ReportPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		if e.dryRun {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(e.result.ReportPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(e.result.ReportPath, []byte(b.String()), 0o644)
	}
	return e.writeFile(rel, []byte(b.String()))
}

func (e *exporter) writeUnsupportedSection(b *strings.Builder) {
	findings := e.findingsByCategory("unsupported")
	b.WriteString("## Unsupported\n\n")
	if len(findings) == 0 {
		b.WriteString("No unsupported export findings.\n")
		b.WriteString("\n")
		return
	}
	for _, f := range findings {
		loc := f.File
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		fmt.Fprintf(b, "- `%s` `%s` - %s\n", loc, f.Rule, f.Message)
	}
	b.WriteString("\n")
}

func (e *exporter) findingsByCategory(category string) []Finding {
	var findings []Finding
	for _, finding := range e.result.Findings {
		if findingCategory(finding.Rule) == category {
			findings = append(findings, finding)
		}
	}
	return findings
}

func (e *exporter) writeFindingSection(b *strings.Builder, title, category string) {
	findings := e.findingsByCategory(category)
	if len(findings) == 0 {
		return
	}
	fmt.Fprintf(b, "## %s\n\n", title)
	for _, f := range findings {
		loc := f.File
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		fmt.Fprintf(b, "- `%s` `%s` - %s\n", loc, f.Rule, f.Message)
	}
	b.WriteString("\n")
}

func findingCategory(rule string) string {
	switch rule {
	case "action_export_unsupported_signature":
		return "unsupported"
	case "rbac_policy_export":
		return "partial"
	case "generated_graphql", "generated_graphql_policies", "generated_policies", "generated_actions":
		return "omitted"
	case "encrypted_columns", "integrity_tables", "raw_sql_migration":
		return "manual"
	default:
		return "manual"
	}
}

func (e *exporter) writeFile(rel string, data []byte) error {
	if e.dryRun {
		e.result.FilesWritten++
		return nil
	}
	path := filepath.Join(e.outDir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	e.result.FilesWritten++
	return nil
}

func exportModulePath(source string) string {
	base := filepath.Base(source)
	base = strings.TrimSpace(base)
	if base == "." || base == "/" || base == "" {
		return "exported-app"
	}
	base = strings.ReplaceAll(base, "_", "-")
	return base
}

func generateModels(tables []*schema.Table) ([]byte, error) {
	var imports = map[string]bool{"gorm.io/gorm": true}
	var models []modelInfo
	for _, tbl := range tables {
		mi := modelInfo{Name: tableToStruct(tbl.Name), Table: tbl.Name}
		for _, col := range tbl.Columns {
			goType, imp := gormGoType(col)
			if imp != "" {
				imports[imp] = true
			}
			field := modelField{Name: snakeToPascal(col.Name), Type: goType, JSON: jsonTag(col), GORM: gormTag(col)}
			mi.Fields = append(mi.Fields, field)
		}
		mi.PublicFields = publicFields(mi.Fields)
		models = append(models, mi)
	}
	var sorted []string
	for imp := range imports {
		sorted = append(sorted, imp)
	}
	sort.Strings(sorted)
	data := modelTemplateData{Imports: sorted, Models: models}
	var buf bytes.Buffer
	if err := modelTemplate.Execute(&buf, data); err != nil {
		return nil, err
	}
	return format.Source(buf.Bytes())
}

func generateModelFile(table *schema.Table) ([]byte, error) {
	imports := map[string]bool{}
	mi := modelInfo{Name: tableToStruct(table.Name), Table: table.Name}
	for _, col := range table.Columns {
		goType, imp := gormGoType(col)
		if imp != "" {
			imports[imp] = true
		}
		if col.IsEncrypted || col.IsSealed {
			imports["fmt"] = true
			imports["gorm.io/gorm"] = true
			plainName := snakeToPascal(col.Name)
			encryptedName := snakeToPascal(col.Name + "_encrypted")
			v2Name := snakeToPascal(col.Name + "_encrypted_v2")
			mi.Fields = append(mi.Fields,
				modelField{Name: plainName, Type: goType, JSON: jsonTag(col), GORM: "-"},
				modelField{Name: encryptedName, Type: encryptedStorageType(col), JSON: "-", GORM: gormTag(encryptedStorageColumn(col, col.Name+"_encrypted", col.IsNullable))},
				modelField{Name: v2Name, Type: "*string", JSON: "-", GORM: gormTag(encryptedStorageColumn(col, col.Name+"_encrypted_v2", true))},
			)
			mi.EncryptedFields = append(mi.EncryptedFields, encryptedModelField{
				Field:         plainName,
				Encrypted:     encryptedName,
				EncryptedV2:   v2Name,
				Column:        col.Name + "_encrypted",
				ColumnV2:      col.Name + "_encrypted_v2",
				Deterministic: col.IsEncrypted,
				Nullable:      col.IsNullable,
			})
			continue
		}
		mi.Fields = append(mi.Fields, modelField{Name: snakeToPascal(col.Name), Type: goType, JSON: jsonTag(col), GORM: gormTag(col)})
	}
	mi.PublicFields = publicFields(mi.Fields)
	var sorted []string
	for imp := range imports {
		sorted = append(sorted, imp)
	}
	sort.Strings(sorted)
	var buf bytes.Buffer
	if err := singleModelTemplate.Execute(&buf, modelTemplateData{Imports: sorted, Models: []modelInfo{mi}}); err != nil {
		return nil, err
	}
	return format.Source(buf.Bytes())
}

func generateViewModelFile(view *schema.View) ([]byte, error) {
	imports := map[string]bool{}
	mi := modelInfo{Name: tableToStruct(view.Name), Table: view.Name}
	for _, col := range view.Columns {
		goType, imp := gormGoType(&col.Column)
		if imp != "" {
			imports[imp] = true
		}
		name := col.OutputName()
		mi.Fields = append(mi.Fields, modelField{Name: snakeToPascal(name), Type: goType, JSON: jsonTag(&col.Column), GORM: "column:" + name + ";->"})
	}
	mi.PublicFields = publicFields(mi.Fields)
	var sorted []string
	for imp := range imports {
		sorted = append(sorted, imp)
	}
	sort.Strings(sorted)
	var buf bytes.Buffer
	if err := singleModelTemplate.Execute(&buf, modelTemplateData{Imports: sorted, Models: []modelInfo{mi}}); err != nil {
		return nil, err
	}
	return format.Source(buf.Bytes())
}

func generateIntegritySupport(tables []*schema.Table) ([]byte, error) {
	var b strings.Builder
	b.WriteString(`package models

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var genesisHash = make([]byte, 32)
var uuidV7Mu sync.Mutex
var uuidV7LastMillis uint64
var uuidV7Sequence uint16

type StaleVersionError struct {
	Table string
	EntityID string
	ExpectedVersion string
	ActualVersion string
}

func (e *StaleVersionError) Error() string {
	return fmt.Sprintf("stale version on %s (id=%s): expected version %s but found %s", e.Table, e.EntityID, e.ExpectedVersion, e.ActualVersion)
}

type ChainError struct {
	Table string
	Position int
	Expected []byte
	Actual []byte
}

func (e *ChainError) Error() string {
	return fmt.Sprintf("chain integrity error on %s at position %d: expected %x but found %x", e.Table, e.Position, e.Expected, e.Actual)
}

`)
	for _, table := range tables {
		if !table.IsImmutable && !table.IsAppendOnly {
			continue
		}
		model := tableToStruct(table.Name)
		columns := integrityColumnList(table)
		verifyOrder := "id ASC"
		latestOrder := "id DESC"
		if table.IsImmutable {
			verifyOrder = "version_id ASC"
			latestOrder = "version_id DESC"
		}
		fmt.Fprintf(&b, "var %sIntegrityColumns = []string{%s}\n\n", model, quotedStringList(columns))
		fmt.Fprintf(&b, "func Create%s(record *%s) error {\n", model, model)
		fmt.Fprintf(&b, "\treturn createIntegrityRecord(DB, %q, record, %t, %q, %sIntegrityColumns)\n", table.Name, table.IsImmutable, latestOrder, model)
		b.WriteString("}\n\n")
		fmt.Fprintf(&b, "func Update%s(record *%s) error {\n", model, model)
		if table.IsImmutable {
			fmt.Fprintf(&b, "\treturn updateImmutableRecord(DB, %q, record, %q, %sIntegrityColumns)\n", table.Name, latestOrder, model)
		} else {
			b.WriteString("\treturn errors.New(\"append-only records cannot be updated\")\n")
		}
		b.WriteString("}\n\n")
		fmt.Fprintf(&b, "func Delete%s(record *%s) error {\n", model, model)
		if table.IsImmutable && tableHasColumn(table, "deleted_at") {
			fmt.Fprintf(&b, "\tnow := time.Now().UTC()\n\trecord.DeletedAt = &now\n\treturn Update%s(record)\n", model)
		} else {
			b.WriteString("\treturn errors.New(\"integrity records cannot be deleted\")\n")
		}
		b.WriteString("}\n\n")
		fmt.Fprintf(&b, "func Verify%sRow(record *%s) error {\n", model, model)
		fmt.Fprintf(&b, "\treturn verifyIntegrityRow(%q, record, %sIntegrityColumns)\n", table.Name, model)
		b.WriteString("}\n\n")
		fmt.Fprintf(&b, "func Verify%sChain() error {\n", model)
		fmt.Fprintf(&b, "\tvar records []%s\n", model)
		fmt.Fprintf(&b, "\tif err := DB.Order(%q).Find(&records).Error; err != nil { return err }\n", verifyOrder)
		fmt.Fprintf(&b, "\treturn verifyIntegrityRecords(%q, records, %sIntegrityColumns)\n", table.Name, model)
		b.WriteString("}\n\n")
	}
	b.WriteString(integritySupportGenericSource)
	return format.Source([]byte(b.String()))
}

func integrityColumnList(table *schema.Table) []string {
	var out []string
	for _, col := range table.Columns {
		if col.Name == "row_hash" || col.Name == "prev_hash" {
			continue
		}
		out = append(out, col.Name)
	}
	return out
}

func quotedStringList(values []string) string {
	var parts []string
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%q", value))
	}
	return strings.Join(parts, ", ")
}

type modelTemplateData struct {
	Imports []string
	Models  []modelInfo
}

type modelInfo struct {
	Name            string
	Table           string
	Fields          []modelField
	PublicFields    []modelField
	EncryptedFields []encryptedModelField
}

type modelField struct {
	Name string
	Type string
	JSON string
	GORM string
}

type encryptedModelField struct {
	Field         string
	Encrypted     string
	EncryptedV2   string
	Column        string
	ColumnV2      string
	Deterministic bool
	Nullable      bool
}

var modelTemplate = template.Must(template.New("models").Parse(`package models

import (
{{- range .Imports }}
	"{{ . }}"
{{- end }}
)

var DB *gorm.DB

func SetDB(db *gorm.DB) { DB = db }

{{ range .Models }}
type {{ .Name }} struct {
{{- range .Fields }}
	{{ .Name }} {{ .Type }} ` + "`" + `json:"{{ .JSON }}" gorm:"{{ .GORM }}"` + "`" + `
{{- end }}
}

func ({{ .Name }}) TableName() string { return "{{ .Table }}" }

{{ if .PublicFields }}type {{ .Name }}Public struct {
{{- range .PublicFields }}
	{{ .Name }} {{ .Type }} ` + "`" + `json:"{{ .JSON }}"` + "`" + `
{{- end }}
}

func (m *{{ .Name }}) Public() {{ .Name }}Public {
	if m == nil { return {{ .Name }}Public{} }
	return {{ .Name }}Public{
{{- range .PublicFields }}
		{{ .Name }}: m.{{ .Name }},
{{- end }}
	}
}

func Public{{ .Name }}s(records []{{ .Name }}) []{{ .Name }}Public {
	out := make([]{{ .Name }}Public, len(records))
	for i := range records { out[i] = records[i].Public() }
	return out
}
{{ end }}
{{ if .EncryptedFields }}
func (m *{{ .Name }}) BeforeSave(tx *gorm.DB) error { return encryptModelFields(m) }
func (m *{{ .Name }}) AfterFind(tx *gorm.DB) error { return decryptModelFields(m) }

func (m *{{ .Name }}) encryptedFields() []exportedEncryptedField {
	return []exportedEncryptedField{
{{- range .EncryptedFields }}
		{
			Column: {{ printf "%q" .Column }},
			ColumnV2: {{ printf "%q" .ColumnV2 }},
			Deterministic: {{ .Deterministic }},
			Marshal: func() ([]byte, error) { return []byte(fmt.Sprint(m.{{ .Field }})), nil },
			Unmarshal: func(b []byte) error { m.{{ .Field }} = string(b); return nil },
			Ciphertext: func() string { return {{ if .Nullable }}derefString(m.{{ .Encrypted }}){{ else }}m.{{ .Encrypted }}{{ end }} },
			CiphertextV2: func() *string { return m.{{ .EncryptedV2 }} },
			SetCiphertext: func(v string) { {{ if .Nullable }}m.{{ .Encrypted }} = &v{{ else }}m.{{ .Encrypted }} = v{{ end }} },
			SetCiphertextV2: func(v *string) { m.{{ .EncryptedV2 }} = v },
		},
{{- end }}
	}
}
{{ end }}
{{ end }}
`))

var singleModelTemplate = template.Must(template.New("model").Parse(`package models

{{ if .Imports }}import (
{{- range .Imports }}
	"{{ . }}"
{{- end }}
)
{{ end }}
{{ range .Models }}
type {{ .Name }} struct {
{{- range .Fields }}
	{{ .Name }} {{ .Type }} ` + "`" + `json:"{{ .JSON }}" gorm:"{{ .GORM }}"` + "`" + `
{{- end }}
}

func ({{ .Name }}) TableName() string { return "{{ .Table }}" }

{{ if .PublicFields }}type {{ .Name }}Public struct {
{{- range .PublicFields }}
	{{ .Name }} {{ .Type }} ` + "`" + `json:"{{ .JSON }}"` + "`" + `
{{- end }}
}

func (m *{{ .Name }}) Public() {{ .Name }}Public {
	if m == nil { return {{ .Name }}Public{} }
	return {{ .Name }}Public{
{{- range .PublicFields }}
		{{ .Name }}: m.{{ .Name }},
{{- end }}
	}
}

func Public{{ .Name }}s(records []{{ .Name }}) []{{ .Name }}Public {
	out := make([]{{ .Name }}Public, len(records))
	for i := range records { out[i] = records[i].Public() }
	return out
}
{{ end }}
{{ if .EncryptedFields }}
func (m *{{ .Name }}) BeforeSave(tx *gorm.DB) error { return encryptModelFields(m) }
func (m *{{ .Name }}) AfterFind(tx *gorm.DB) error { return decryptModelFields(m) }

func (m *{{ .Name }}) encryptedFields() []exportedEncryptedField {
	return []exportedEncryptedField{
{{- range .EncryptedFields }}
		{
			Column: {{ printf "%q" .Column }},
			ColumnV2: {{ printf "%q" .ColumnV2 }},
			Deterministic: {{ .Deterministic }},
			Marshal: func() ([]byte, error) { return []byte(fmt.Sprint(m.{{ .Field }})), nil },
			Unmarshal: func(b []byte) error { m.{{ .Field }} = string(b); return nil },
			Ciphertext: func() string { return {{ if .Nullable }}derefString(m.{{ .Encrypted }}){{ else }}m.{{ .Encrypted }}{{ end }} },
			CiphertextV2: func() *string { return m.{{ .EncryptedV2 }} },
			SetCiphertext: func(v string) { {{ if .Nullable }}m.{{ .Encrypted }} = &v{{ else }}m.{{ .Encrypted }} = v{{ end }} },
			SetCiphertextV2: func(v *string) { m.{{ .EncryptedV2 }} = v },
		},
{{- end }}
	}
}
{{ end }}
{{ end }}
`))

func gormGoType(col *schema.Column) (string, string) {
	base, imp := func() (string, string) {
		switch col.Type {
		case schema.UUID:
			return "uuid.UUID", "github.com/google/uuid"
		case schema.String, schema.Text, schema.Time:
			return "string", ""
		case schema.Decimal:
			return "decimal.Decimal", "github.com/shopspring/decimal"
		case schema.JSONB:
			return "json.RawMessage", "encoding/json"
		case schema.Binary:
			return "[]byte", ""
		case schema.Integer:
			return "int", ""
		case schema.BigInteger:
			return "int64", ""
		case schema.Boolean:
			return "bool", ""
		case schema.Timestamp, schema.Date:
			return "time.Time", "time"
		case schema.Float:
			return "float32", ""
		case schema.Double:
			return "float64", ""
		default:
			return "string", ""
		}
	}()
	if col.IsNullable && !strings.HasPrefix(base, "*") {
		base = "*" + base
	}
	return base, imp
}

func encryptedStorageType(col *schema.Column) string {
	if col.IsNullable {
		return "*string"
	}
	return "string"
}

func encryptedStorageColumn(src *schema.Column, name string, nullable bool) *schema.Column {
	col := *src
	col.Name = name
	col.Type = schema.Text
	col.IsEncrypted = false
	col.IsSealed = false
	col.IsNullable = nullable
	col.IsPrimaryKey = false
	col.IsUnique = false
	col.ForeignKeyTable = ""
	col.ForeignKeyColumn = ""
	col.HasDefault = false
	col.DefaultValue = nil
	return &col
}

func tableHasEncryptedColumns(table *schema.Table) bool {
	for _, col := range table.Columns {
		if col.IsEncrypted || col.IsSealed {
			return true
		}
	}
	return false
}

func tablesHaveEncryptedColumns(tables []*schema.Table) bool {
	for _, table := range tables {
		if tableHasEncryptedColumns(table) {
			return true
		}
	}
	return false
}

func integrityModelSet(tables []*schema.Table) map[string]integrityModelInfo {
	out := map[string]integrityModelInfo{}
	for _, table := range tables {
		if !table.IsImmutable && !table.IsAppendOnly {
			continue
		}
		out[tableToStruct(table.Name)] = integrityModelInfo{
			Table:       table,
			Immutable:   table.IsImmutable,
			AppendOnly:  table.IsAppendOnly,
			SoftDeletes: tableHasColumn(table, "deleted_at"),
		}
	}
	return out
}

func tableHasColumn(table *schema.Table, name string) bool {
	for _, col := range table.Columns {
		if col.Name == name {
			return true
		}
	}
	return false
}

func gormTag(col *schema.Column) string {
	var tags []string
	tags = append(tags, "column:"+col.Name)
	if col.IsPrimaryKey {
		tags = append(tags, "primaryKey")
	}
	if col.Type == schema.UUID {
		tags = append(tags, "type:uuid")
	}
	if col.Type == schema.String && col.Length > 0 {
		tags = append(tags, fmt.Sprintf("size:%d", col.Length))
	}
	if !col.IsNullable && !col.IsPrimaryKey {
		tags = append(tags, "not null")
	}
	if col.IsUnique {
		tags = append(tags, "uniqueIndex")
	}
	return strings.Join(tags, ";")
}

func jsonTag(col *schema.Column) string {
	if col.Name == "password" || col.Name == "password_hash" || col.Name == "row_hash" || col.Name == "prev_hash" {
		return "-"
	}
	if col.IsNullable {
		return col.Name + ",omitempty"
	}
	return col.Name
}

func publicFields(fields []modelField) []modelField {
	var out []modelField
	for _, f := range fields {
		if f.JSON != "-" {
			out = append(out, f)
		}
	}
	return out
}

func generateBindings(requests []generator.RequestDef) ([]byte, error) {
	if len(requests) == 0 {
		return []byte("package requests\n"), nil
	}
	data := struct{ Requests []generator.RequestDef }{Requests: requests}
	var buf bytes.Buffer
	if err := bindingsTemplate.Execute(&buf, data); err != nil {
		return nil, err
	}
	return format.Source(buf.Bytes())
}

func generateConfigSupport(scan *generator.ConfigScanResult) ([]byte, error) {
	var buf bytes.Buffer
	if err := configSupportTemplate.Execute(&buf, scan); err != nil {
		return nil, err
	}
	return format.Source(buf.Bytes())
}

var configSupportTemplate = template.Must(template.New("config-support").Parse(`package config

import (
	"bufio"
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var envOnce sync.Once
var envMap map[string]string

func Env(key, fallback string) string {
	envOnce.Do(loadEnv)
	if v := os.Getenv(key); v != "" { return v }
	if v, ok := envMap[key]; ok { return v }
	return fallback
}

func loadEnv() {
	envMap = map[string]string{}
	f, err := os.Open(".env")
	if err != nil { return }
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") { continue }
		idx := strings.IndexByte(line, '=')
		if idx < 0 { continue }
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) { value = value[1:len(value)-1] }
		if os.Getenv(key) == "" { envMap[key] = value }
	}
}

type ConnectionConfig struct {
	Driver string
	Host string
	Port string
	Name string
	User string
	Password string
	Region string
	Options map[string]string
}

func (c ConnectionConfig) DSN() string {
	dsn, err := c.dsn()
	if err != nil { return "" }
	return dsn
}

func (c ConnectionConfig) Validate() error {
	switch c.Driver {
	case "pgsql", "postgres", "mysql", "sqlite":
		return nil
	default:
		return fmt.Errorf("unsupported database driver: %s", c.Driver)
	}
}

func (c ConnectionConfig) dsn() (string, error) {
	switch c.Driver {
	case "pgsql", "postgres":
		params := url.Values{}
		params.Set("sslmode", "disable")
		for k, v := range c.Options { params.Set(k, v) }
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?%s", url.PathEscape(c.User), url.PathEscape(c.Password), c.Host, c.Port, c.Name, params.Encode()), nil
	case "mysql":
		params := url.Values{}
		params.Set("parseTime", "true")
		for k, v := range c.Options { params.Set(k, v) }
		return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?%s", url.PathEscape(c.User), url.PathEscape(c.Password), c.Host, c.Port, c.Name, params.Encode()), nil
	case "sqlite":
		return c.Name, nil
	default:
		return "", fmt.Errorf("unsupported database driver: %s", c.Driver)
	}
}

func (c ConnectionConfig) driverName() (string, error) {
	switch c.Driver {
	case "pgsql", "postgres": return "pgx", nil
	case "mysql": return "mysql", nil
	case "sqlite": return "sqlite3", nil
	default: return "", fmt.Errorf("unsupported database driver: %s", c.Driver)
	}
}

func TryOpenDB(conn ConnectionConfig) (*sql.DB, error) {
	driverName, err := conn.driverName()
	if err != nil { return nil, err }
	dsn, err := conn.dsn()
	if err != nil { return nil, err }
	db, err := sql.Open(driverName, dsn)
	if err != nil { return nil, fmt.Errorf("open database: %w", err) }
	if err := db.Ping(); err != nil { db.Close(); return nil, fmt.Errorf("ping database: %w", err) }
	return db, nil
}

func OpenDB(conn ConnectionConfig) *sql.DB {
	db, err := TryOpenDB(conn)
	if err != nil { log.Fatalf("config: failed to open database: %v", err) }
	return db
}

func TryOpenGORM(conn ConnectionConfig) (*gorm.DB, error) {
	sqlDB, err := TryOpenDB(conn)
	if err != nil { return nil, err }
	var dialector gorm.Dialector
	switch conn.Driver {
	case "pgsql", "postgres":
		dialector = postgres.New(postgres.Config{Conn: sqlDB})
	case "mysql":
		dialector = mysql.New(mysql.Config{Conn: sqlDB})
	case "sqlite":
		dialector = sqlite.Dialector{Conn: sqlDB}
	default:
		sqlDB.Close()
		return nil, fmt.Errorf("unsupported database driver: %s", conn.Driver)
	}
	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil { sqlDB.Close(); return nil, fmt.Errorf("initialize gorm: %w", err) }
	return db, nil
}

func OpenGORM(conn ConnectionConfig) *gorm.DB {
	db, err := TryOpenGORM(conn)
	if err != nil { log.Fatalf("config: failed to initialize database: %v", err) }
	return db
}

{{ range .Configs }}var {{ .VarName }} {{ .ReturnType }}
{{ end }}

func Init() {
{{- range .Configs }}
	{{ .VarName }} = {{ .FuncName }}()
{{- end }}
}

{{ if .HasDatabaseConfig }}func (d DatabaseConfig) Connection(name ...string) ConnectionConfig {
	conn, err := d.TryConnection(name...)
	if err != nil { log.Fatal(err) }
	return conn
}

func (d DatabaseConfig) TryConnection(name ...string) (ConnectionConfig, error) {
	key := d.Default
	if len(name) > 0 && name[0] != "" { key = name[0] }
	conn, ok := d.Connections[key]
	if !ok { return ConnectionConfig{}, fmt.Errorf("config: unknown database connection: %s", key) }
	return conn, nil
}

func (d DatabaseConfig) Open(name ...string) *sql.DB { return OpenDB(d.Connection(name...)) }

func (d DatabaseConfig) TryOpen(name ...string) (*sql.DB, error) {
	conn, err := d.TryConnection(name...)
	if err != nil { return nil, err }
	return TryOpenDB(conn)
}

func (d DatabaseConfig) OpenGORM(name ...string) *gorm.DB { return OpenGORM(d.Connection(name...)) }

func (d DatabaseConfig) TryOpenGORM(name ...string) (*gorm.DB, error) {
	conn, err := d.TryConnection(name...)
	if err != nil { return nil, err }
	return TryOpenGORM(conn)
}
{{ end }}
`))

var bindingsTemplate = template.Must(template.New("bindings").Parse(`package requests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

type ValidationError struct { Field string ` + "`" + `json:"field"` + "`" + `; Message string ` + "`" + `json:"message"` + "`" + ` }
type BindingError struct { Status int ` + "`" + `json:"-"` + "`" + `; Errors []ValidationError ` + "`" + `json:"errors"` + "`" + ` }
func (e *BindingError) Error() string { parts := make([]string, len(e.Errors)); for i, ve := range e.Errors { parts[i] = ve.Field + ": " + ve.Message }; return strings.Join(parts, "; ") }
func formatValidationErrors(err error) *BindingError { ve, ok := err.(validator.ValidationErrors); if !ok { return &BindingError{Status: 422, Errors: []ValidationError{{"{{"}}Field: "_body", Message: err.Error(){{"}}"}}} }; out := make([]ValidationError, len(ve)); for i, fe := range ve { out[i] = ValidationError{Field: fe.Field(), Message: fmt.Sprintf("failed %s validation", fe.Tag())} }; return &BindingError{Status: 422, Errors: out} }
{{ range .Requests }}
func Bind{{ .Name }}(r *http.Request) ({{ .Name }}, *BindingError) { var req {{ .Name }}; if err := json.NewDecoder(r.Body).Decode(&req); err != nil { return req, &BindingError{Status: 400, Errors: []ValidationError{{"{{"}}Field: "_body", Message: "invalid request body"{{"}}"}}} }; if err := validate.Struct(req); err != nil { return req, formatValidationErrors(err) }; return req, nil }
{{ end }}
`))

func snakeToPascal(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		if strings.EqualFold(p, "id") {
			parts[i] = "ID"
			continue
		}
		if strings.EqualFold(p, "url") {
			parts[i] = "URL"
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

func tableToStruct(table string) string {
	if strings.HasSuffix(table, "ies") {
		return snakeToPascal(strings.TrimSuffix(table, "ies") + "y")
	}
	if strings.HasSuffix(table, "s") {
		return snakeToPascal(strings.TrimSuffix(table, "s"))
	}
	return snakeToPascal(table)
}

func modelSet(tables []*schema.Table) map[string]bool {
	out := map[string]bool{}
	for _, tbl := range tables {
		out[tableToStruct(tbl.Name)] = true
	}
	return out
}

func modelFileName(table string) string {
	name := table
	if strings.HasSuffix(name, "ies") {
		name = strings.TrimSuffix(name, "ies") + "y"
	} else if strings.HasSuffix(name, "s") {
		name = strings.TrimSuffix(name, "s")
	}
	return name
}

func pascalToSnake(s string) string {
	if s == "ID" {
		return "id"
	}
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if r >= 'A' && r <= 'Z' {
			if i > 0 && shouldInsertSnakeBoundary(runes, i) {
				b.WriteByte('_')
			}
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func shouldInsertSnakeBoundary(runes []rune, i int) bool {
	prev := runes[i-1]
	if prev >= 'a' && prev <= 'z' {
		return true
	}
	if prev >= '0' && prev <= '9' {
		return true
	}
	if prev >= 'A' && prev <= 'Z' && i+1 < len(runes) {
		next := runes[i+1]
		return next >= 'a' && next <= 'z'
	}
	return false
}

const httpxSource = `package httpx

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Controller struct{}
type Response struct { Status int; StatusCode int; Body any; Headers map[string]string; Cookies []*http.Cookie }
type AuthInfo struct { UserID string; Role string }
type RoleInfo struct { Slug string; Manages bool }
type Context struct { request *http.Request; response http.ResponseWriter; auth *AuthInfo; params map[string]string; roles []string; isAdmin bool }

func NewContext(r *http.Request) *Context { return &Context{request: r, params: map[string]string{}} }
func (c *Context) Request() *http.Request { return c.request }
func (c *Context) ResponseWriter() http.ResponseWriter { return c.response }
func (c *Context) Param(name string) string { value, ok := c.params[name]; if !ok { panic("pickle: ctx.Param(\"" + name + "\") - no such route parameter") }; return value }
func (c *Context) SetParam(name, value string) { c.params[name] = value }
func (c *Context) ParamUUID(name string) (uuid.UUID, error) { return uuid.Parse(c.Param(name)) }
func (c *Context) Cookie(name string) (string, error) { cookie, err := c.request.Cookie(name); if err != nil { return "", err }; return cookie.Value, nil }
func (c *Context) Query(name string) string { return c.request.URL.Query().Get(name) }
func (c *Context) BearerToken() string { h := c.request.Header.Get("Authorization"); parts := strings.Fields(h); if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") { return parts[1] }; return "" }
func (c *Context) Auth() *AuthInfo { if c.auth == nil { return &AuthInfo{} }; return c.auth }
func (c *Context) SetAuth(claims any) { switch v := claims.(type) { case *AuthInfo: c.auth = v; default: panic(fmt.Sprintf("pickle: SetAuth() requires *AuthInfo, got %T", claims)) } }
func (c *Context) IsAuthenticated() bool { return c.auth != nil && c.auth.UserID != "" }
func (c *Context) SetRoles(roles []RoleInfo) { c.roles = make([]string, len(roles)); c.isAdmin = false; for i, role := range roles { c.roles[i] = role.Slug; if role.Manages { c.isAdmin = true } } }
func (c *Context) Role() string { if len(c.roles) > 0 { return c.roles[0] }; if c.auth != nil { return c.auth.Role }; return "" }
func (c *Context) Roles() []string { roles := append([]string{}, c.roles...); if len(roles) == 0 && c.auth != nil && c.auth.Role != "" { roles = append(roles, c.auth.Role) }; return roles }
func (c *Context) HasRole(slug string) bool { for _, role := range c.roles { if role == slug { return true } }; return c.auth != nil && c.auth.Role == slug }
func (c *Context) HasAnyRole(roles ...string) bool { for _, role := range roles { if c.HasRole(role) { return true } }; return false }
func (c *Context) IsAdmin() bool { return c.isAdmin || (c.auth != nil && c.auth.Role == "admin") }
func (c *Context) JSON(status int, body any) Response { return Response{Status: status, StatusCode: status, Body: body} }
func (c *Context) Error(err error) Response { return c.JSON(500, map[string]string{"error": err.Error()}) }
func (c *Context) BadRequest(msg string) Response { return c.JSON(400, map[string]string{"error": msg}) }
func (c *Context) Unauthorized(msg string) Response { return c.JSON(401, map[string]string{"error": msg}) }
func (c *Context) Forbidden(msg string) Response { return c.JSON(403, map[string]string{"error": msg}) }
func (c *Context) NotFound(msg string) Response { return c.JSON(404, map[string]string{"error": msg}) }
func (c *Context) NoContent() Response { return Response{Status: 204, StatusCode: 204} }

type ResourceQuery interface { FetchResource(ownerID string) (any, error) }
type ResourceListQuery interface { FetchResources(ownerID string) (any, error) }
func (c *Context) Resource(q ResourceQuery) Response { ownerID := ""; if c.auth != nil { ownerID = c.auth.UserID }; result, err := q.FetchResource(ownerID); if err != nil { if err.Error() == "sql: no rows in result set" { return c.NotFound("not found") }; return c.Error(err) }; return c.JSON(http.StatusOK, result) }
func (c *Context) Resources(q ResourceListQuery) Response { ownerID := ""; if c.auth != nil { ownerID = c.auth.UserID }; result, err := q.FetchResources(ownerID); if err != nil { return c.Error(err) }; return c.JSON(http.StatusOK, result) }

func (r Response) WithCookie(cookie *http.Cookie) Response { r.Cookies = append(r.Cookies, cookie); return r }

func (r Response) Write(w http.ResponseWriter) { for k, v := range r.Headers { w.Header().Set(k, v) }; for _, cookie := range r.Cookies { http.SetCookie(w, cookie) }; status := r.Status; if status == 0 { status = r.StatusCode }; if status == 0 { status = 200 }; w.WriteHeader(status); if r.Body != nil { _ = json.NewEncoder(w).Encode(r.Body) } }

type HandlerFunc func(*Context) Response
type MiddlewareFunc func(*Context, func() Response) Response
type MiddlewareProvider interface { Middleware() MiddlewareFunc }
type ResourceController interface { Index(*Context) Response; Show(*Context) Response; Store(*Context) Response; Update(*Context) Response; Destroy(*Context) Response }
type ErrorReporter func(*Context, error)
type Route struct { Method string; Path string; Handler HandlerFunc; Middleware []MiddlewareFunc }
type Router struct{ prefix string; middleware []MiddlewareFunc; routes []Route; onError ErrorReporter }
func Routes(fn func(*Router)) *Router { r := &Router{}; fn(r); return r }
func (r *Router) OnError(fn ErrorReporter) { r.onError = fn }
func (r *Router) OnRateLimit(fn func(*Context, RateLimitEvent)) { rateLimitCallback = fn }
func (r *Router) Group(path string, args ...any) {
	child := &Router{prefix: joinPath(r.prefix, path), middleware: append([]MiddlewareFunc{}, r.middleware...)}
	var bodies []func(*Router)
	for _, arg := range args {
		switch v := arg.(type) {
		case MiddlewareFunc:
			child.middleware = append(child.middleware, v)
		case func(*Context, func() Response) Response:
			child.middleware = append(child.middleware, MiddlewareFunc(v))
		case MiddlewareProvider:
			child.middleware = append(child.middleware, v.Middleware())
		case func(*Router):
			bodies = append(bodies, v)
		}
	}
	for _, body := range bodies { body(child) }
	r.routes = append(r.routes, child.routes...)
}
func (r *Router) Get(path string, handler HandlerFunc, middleware ...any) { r.add("GET", path, handler, middleware...) }
func (r *Router) Post(path string, handler HandlerFunc, middleware ...any) { r.add("POST", path, handler, middleware...) }
func (r *Router) Put(path string, handler HandlerFunc, middleware ...any) { r.add("PUT", path, handler, middleware...) }
func (r *Router) Patch(path string, handler HandlerFunc, middleware ...any) { r.add("PATCH", path, handler, middleware...) }
func (r *Router) Delete(path string, handler HandlerFunc, middleware ...any) { r.add("DELETE", path, handler, middleware...) }
func (r *Router) add(method, path string, handler HandlerFunc, middleware ...any) { r.routes = append(r.routes, Route{Method: method, Path: joinPath(r.prefix, path), Handler: handler, Middleware: append(append([]MiddlewareFunc{}, r.middleware...), resolveMiddleware(middleware)...)} ) }
func (r *Router) Resource(prefix string, c ResourceController, middleware ...any) { r.Get(prefix, c.Index, middleware...); r.Get(prefix + "/:id", c.Show, middleware...); r.Post(prefix, c.Store, middleware...); r.Put(prefix + "/:id", c.Update, middleware...); r.Delete(prefix + "/:id", c.Destroy, middleware...) }
func resolveMiddleware(middleware []any) []MiddlewareFunc { resolved := make([]MiddlewareFunc, 0, len(middleware)); for _, mw := range middleware { switch v := mw.(type) { case MiddlewareFunc: resolved = append(resolved, v); case func(*Context, func() Response) Response: resolved = append(resolved, MiddlewareFunc(v)); case MiddlewareProvider: resolved = append(resolved, v.Middleware()); default: panic("pickle export: invalid middleware type") } }; return resolved }
func (r *Router) AllRoutes() []Route { routes := make([]Route, len(r.routes)); copy(routes, r.routes); return routes }
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var ctx *Context
	defer func() { if recovered := recover(); recovered != nil { err, ok := recovered.(error); if !ok { err = fmt.Errorf("%v", recovered) }; log.Printf("panic: %v\n%s", err, debug.Stack()); if r.onError != nil { r.onError(ctx, err) }; http.Error(w, "internal server error", http.StatusInternalServerError) } }()
	rateLimitResp, rateLimitHeaders := checkRateLimit(req)
	if rateLimitResp != nil {
		rateLimitResp.Write(w)
		return
	}
	for _, rt := range r.routes {
		params, ok := matchPath(rt.Path, req.URL.Path)
		if rt.Method != req.Method || !ok { continue }
		ctx = NewContext(req); ctx.response = w; ctx.params = params
		next := func() Response { return rt.Handler(ctx) }
		for i := len(rt.Middleware) - 1; i >= 0; i-- {
			mw := rt.Middleware[i]
			inner := next
			next = func() Response { return mw(ctx, inner) }
		}
		resp := next()
		for k, v := range rateLimitHeaders { if resp.Headers == nil { resp.Headers = map[string]string{} }; resp.Headers[k] = v }
		resp.Write(w)
		return
	}
	http.NotFound(w, req)
}
var paramPattern = regexp.MustCompile(` + "`" + `:(\w+)` + "`" + `)
func (r *Router) RegisterRoutes(mux *http.ServeMux) {
	registered := map[string]bool{}
	for _, route := range r.AllRoutes() {
		goPath := paramPattern.ReplaceAllString(route.Path, "{$1}")
		pattern := route.Method + " " + goPath
		if registered[pattern] { panic("pickle: duplicate route registered: " + pattern) }
		registered[pattern] = true
		mux.HandleFunc(pattern, r.ServeHTTP)
		if !strings.HasSuffix(goPath, "}") {
			alt := ""
			if strings.HasSuffix(goPath, "/") {
				if trimmed := strings.TrimRight(goPath, "/"); trimmed != "" { alt = route.Method + " " + trimmed }
			} else {
				alt = route.Method + " " + goPath + "/"
			}
			if alt != "" && !registered[alt] { registered[alt] = true; mux.HandleFunc(alt, r.ServeHTTP) }
		}
	}
}
func (r *Router) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	r.RegisterRoutes(mux)
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second}
	return server.ListenAndServe()
}
func joinPath(prefix, path string) string { if path == "/" { path = "" }; out := "/" + strings.Trim(strings.TrimRight(prefix, "/") + "/" + strings.Trim(path, "/"), "/"); if out == "/" { return "/" }; return out }
func matchPath(pattern, actual string) (map[string]string, bool) {
	pp := strings.Split(strings.Trim(pattern, "/"), "/")
	aa := strings.Split(strings.Trim(actual, "/"), "/")
	if len(pp) != len(aa) { return nil, false }
	params := map[string]string{}
	for i := range pp { if strings.HasPrefix(pp[i], ":") { params[strings.TrimPrefix(pp[i], ":")] = aa[i]; continue }; if pp[i] != aa[i] { return nil, false } }
	return params, true
}

type rateBucket struct { mu sync.Mutex; tokens float64; lastFill time.Time; lastSeen time.Time }
func (b *rateBucket) allow(rps float64, burst int) bool { b.mu.Lock(); defer b.mu.Unlock(); now := time.Now(); elapsed := now.Sub(b.lastFill).Seconds(); b.tokens = math.Min(float64(burst), b.tokens+elapsed*rps); b.lastFill = now; b.lastSeen = now; if b.tokens < 1 { return false }; b.tokens--; return true }
func (b *rateBucket) retryAfter(rps float64) int { b.mu.Lock(); defer b.mu.Unlock(); if rps <= 0 { return 1 }; missing := 1 - b.tokens; if missing <= 0 { return 1 }; retry := int(math.Ceil(missing / rps)); if retry < 1 { return 1 }; return retry }

type rateLimiterStore struct { mu sync.Mutex; buckets map[string]*rateBucket; rps float64; burst int; enabled bool }
func newRateLimiterStore(rps float64, burst int) *rateLimiterStore { if burst < 1 { burst = 1 }; return &rateLimiterStore{buckets: map[string]*rateBucket{}, rps: rps, burst: burst, enabled: rps > 0} }
func (s *rateLimiterStore) allow(key string) (*rateBucket, bool) { return s.allowWithParams(key, s.rps, s.burst) }
func (s *rateLimiterStore) allowWithParams(key string, rps float64, burst int) (*rateBucket, bool) { if burst < 1 { burst = 1 }; s.mu.Lock(); bucket := s.buckets[key]; if bucket == nil { bucket = &rateBucket{tokens: float64(burst), lastFill: time.Now(), lastSeen: time.Now()}; s.buckets[key] = bucket }; s.mu.Unlock(); return bucket, bucket.allow(rps, burst) }
func (s *rateLimiterStore) cleanup() { cutoff := time.Now().Add(-10 * time.Minute); s.mu.Lock(); defer s.mu.Unlock(); for key, bucket := range s.buckets { bucket.mu.Lock(); stale := bucket.lastSeen.Before(cutoff); bucket.mu.Unlock(); if stale { delete(s.buckets, key) } } }

var globalLimiterOnce sync.Once
var globalLimiter *rateLimiterStore
type RateLimitEvent struct { Key string; Layer string; Path string; RPS float64; Burst int; Remaining float64; Allowed bool }
var rateLimitCallback func(*Context, RateLimitEvent)
var trustedProxies []net.IPNet
var trustedProxiesAll bool
var trustedProxiesOnce sync.Once

func checkRateLimit(r *http.Request) (*Response, map[string]string) {
	globalLimiterOnce.Do(func() {
		enabled := strings.ToLower(os.Getenv("RATE_LIMIT")) != "false"
		rps, _ := strconv.ParseFloat(env("RATE_LIMIT_RPS", "10"), 64)
		burst, _ := strconv.Atoi(env("RATE_LIMIT_BURST", "20"))
		globalLimiter = newRateLimiterStore(rps, burst)
		globalLimiter.enabled = enabled && globalLimiter.enabled
		go cleanupRateLimiter(globalLimiter)
	})
	if globalLimiter == nil || !globalLimiter.enabled { return nil, nil }
	key := clientIP(r)
	bucket, ok := globalLimiter.allow(key)
	remaining := bucketRemaining(bucket)
	if rateLimitCallback != nil { rateLimitCallback(NewContext(r), RateLimitEvent{Key: key, Layer: "ip", Path: r.URL.Path, RPS: globalLimiter.rps, Burst: globalLimiter.burst, Remaining: remaining, Allowed: ok}) }
	if ok { return nil, rateLimitHeaders(globalLimiter.rps, globalLimiter.burst, remaining) }
	return rateLimitExceeded(bucket, globalLimiter.rps, globalLimiter.burst), nil
}

func RateLimit(rps, burst int) MiddlewareFunc {
	store := newRateLimiterStore(float64(rps), burst)
	go cleanupRateLimiter(store)
	return func(ctx *Context, next func() Response) Response {
		if !store.enabled { return next() }
		key := clientIP(ctx.Request())
		bucket, ok := store.allow(key)
		remaining := bucketRemaining(bucket)
		if rateLimitCallback != nil { rateLimitCallback(ctx, RateLimitEvent{Key: key, Layer: "ip", Path: ctx.Request().URL.Path, RPS: store.rps, Burst: store.burst, Remaining: remaining, Allowed: ok}) }
		if ok {
			resp := next()
			for k, v := range rateLimitHeaders(store.rps, store.burst, remaining) { if resp.Headers == nil { resp.Headers = map[string]string{} }; resp.Headers[k] = v }
			return resp
		}
		return *rateLimitExceeded(bucket, store.rps, store.burst)
	}
}

type RateTier struct { RPS float64; Burst int }
type AuthRateLimitConfig struct { rps float64; burst int; keyFunc func(*Context) string; tiers map[string]RateTier; store *rateLimiterStore }
func AuthRateLimit() *AuthRateLimitConfig { rps, _ := strconv.ParseFloat(env("AUTH_RATE_LIMIT_RPS", "30"), 64); burst, _ := strconv.Atoi(env("AUTH_RATE_LIMIT_BURST", "60")); c := &AuthRateLimitConfig{rps: rps, burst: burst, store: newRateLimiterStore(rps, burst)}; go cleanupRateLimiter(c.store); return c }
func (c *AuthRateLimitConfig) RPS(rps float64) *AuthRateLimitConfig { c.rps = rps; c.store.rps = rps; c.store.enabled = rps > 0; return c }
func (c *AuthRateLimitConfig) Burst(burst int) *AuthRateLimitConfig { if burst < 1 { burst = 1 }; c.burst = burst; c.store.burst = burst; return c }
func (c *AuthRateLimitConfig) KeyFunc(fn func(*Context) string) *AuthRateLimitConfig { c.keyFunc = fn; return c }
func (c *AuthRateLimitConfig) Tiers(tiers map[string]RateTier) *AuthRateLimitConfig { c.tiers = tiers; return c }
func (c *AuthRateLimitConfig) Middleware() MiddlewareFunc {
	return func(ctx *Context, next func() Response) Response {
		key := ""
		if ctx.auth != nil { key = ctx.auth.UserID }
		if key == "" && c.keyFunc != nil { key = c.keyFunc(ctx) }
		if key == "" { key = clientIP(ctx.Request()) }
		rps, burst := c.rps, c.burst
		if role := ctx.Role(); role != "" && c.tiers != nil { if tier, ok := c.tiers[role]; ok { rps = tier.RPS; burst = tier.Burst; key = role + ":" + key } }
		bucket, ok := c.store.allowWithParams(key, rps, burst)
		remaining := bucketRemaining(bucket)
		if rateLimitCallback != nil { rateLimitCallback(ctx, RateLimitEvent{Key: key, Layer: "auth", Path: ctx.Request().URL.Path, RPS: rps, Burst: burst, Remaining: remaining, Allowed: ok}) }
		if ok {
			resp := next()
			for k, v := range rateLimitHeaders(rps, burst, remaining) { if resp.Headers == nil { resp.Headers = map[string]string{} }; resp.Headers[k] = v }
			return resp
		}
		return *rateLimitExceeded(bucket, rps, burst)
	}
}

func cleanupRateLimiter(store *rateLimiterStore) { ticker := time.NewTicker(5 * time.Minute); defer ticker.Stop(); for range ticker.C { store.cleanup() } }
func bucketRemaining(bucket *rateBucket) float64 { if bucket == nil { return 0 }; bucket.mu.Lock(); defer bucket.mu.Unlock(); return bucket.tokens }
func rateLimitExceeded(bucket *rateBucket, rps float64, burst int) *Response { resp := Response{StatusCode: http.StatusTooManyRequests, Body: map[string]string{"error": "rate limit exceeded"}, Headers: map[string]string{"Content-Type": "application/json", "Retry-After": strconv.Itoa(bucket.retryAfter(rps))}}; for k, v := range rateLimitHeaders(rps, burst, 0) { resp.Headers[k] = v }; return &resp }
func rateLimitHeaders(rps float64, burst int, remaining float64) map[string]string { if remaining < 0 { remaining = 0 }; reset := time.Now(); if rps > 0 { deficit := float64(burst) - remaining; if deficit > 0 { reset = reset.Add(time.Duration((deficit / rps) * float64(time.Second))) } }; return map[string]string{"X-RateLimit-Limit": strconv.Itoa(int(rps)), "X-RateLimit-Remaining": strconv.Itoa(int(remaining)), "X-RateLimit-Reset": strconv.FormatInt(reset.Unix(), 10)} }
func env(key, fallback string) string { if value := os.Getenv(key); value != "" { return value }; return fallback }
func initTrustedProxies() { trustedProxiesOnce.Do(func() { raw := strings.TrimSpace(env("TRUSTED_PROXIES", "")); if raw == "" { return }; if raw == "all" { trustedProxiesAll = true; return }; for _, entry := range strings.Split(raw, ",") { entry = strings.TrimSpace(entry); if entry == "" { continue }; if !strings.Contains(entry, "/") { ip := net.ParseIP(entry); if ip == nil { continue }; if ip.To4() != nil { entry += "/32" } else { entry += "/128" } }; _, cidr, err := net.ParseCIDR(entry); if err == nil { trustedProxies = append(trustedProxies, *cidr) } } }) }
func proxyHeadersTrusted(remote string) bool { initTrustedProxies(); if trustedProxiesAll { return true }; ip := net.ParseIP(remote); if ip == nil { return false }; for _, cidr := range trustedProxies { if cidr.Contains(ip) { return true } }; return false }
func firstUntrustedIP(xff string) string { parts := strings.Split(xff, ","); for i := len(parts) - 1; i >= 0; i-- { ip := strings.TrimSpace(parts[i]); if ip != "" && !proxyHeadersTrusted(ip) { return ip } }; return strings.TrimSpace(parts[0]) }
func stripPort(addr string) string { if host, _, err := net.SplitHostPort(addr); err == nil { return host }; return addr }
func clientIP(r *http.Request) string { remote := stripPort(r.RemoteAddr); if !proxyHeadersTrusted(remote) { return remote }; if xff := r.Header.Get("X-Forwarded-For"); xff != "" { return firstUntrustedIP(xff) }; if xri := r.Header.Get("X-Real-IP"); xri != "" { return strings.TrimSpace(xri) }; return remote }
`

const rbacMiddlewareSupportSource = `package middleware

import (
	"errors"

	"%s/app/models"
	"%s/internal/httpx"
)

var errNoRoleDatabase = errors.New("pickle export: models.DB is not configured for RBAC role loading")

type roleRow struct {
	Slug    string
	Manages bool
}

func LoadRoles(ctx *httpx.Context, next func() httpx.Response) httpx.Response {
	if !ctx.IsAuthenticated() {
		return ctx.Unauthorized("LoadRoles requires authentication - add Auth middleware before LoadRoles")
	}
	if models.DB == nil {
		return ctx.Error(errNoRoleDatabase)
	}
	var rows []roleRow
	if err := models.DB.Raw("SELECT r.slug, r.manages FROM roles r JOIN role_user ru ON ru.role_id = r.id WHERE ru.user_id = ?", ctx.Auth().UserID).Scan(&rows).Error; err != nil {
		return ctx.Error(err)
	}
	roles := make([]httpx.RoleInfo, len(rows))
	for i, row := range rows {
		roles[i] = httpx.RoleInfo{Slug: row.Slug, Manages: row.Manages}
	}
	ctx.SetRoles(roles)
	return next()
}

func RequireRole(roles ...string) httpx.MiddlewareFunc {
	return func(ctx *httpx.Context, next func() httpx.Response) httpx.Response {
		if !ctx.HasAnyRole(roles...) {
			return ctx.Forbidden("insufficient role")
		}
		return next()
	}
}

func RequireAdmin(ctx *httpx.Context, next func() httpx.Response) httpx.Response {
	if !ctx.IsAdmin() {
		return ctx.Forbidden("admin access required")
	}
	return next()
}
`

const authSupportSource = `package auth

import (
	"database/sql"
	"fmt"
	"net/http"

	"%s/app/http/auth/jwt"
	"%s/app/http/auth/oauth"
	"%s/app/http/auth/session"
	"%s/config"
	"%s/internal/httpx"
)

type AuthDriver interface {
	Authenticate(r *http.Request) (*httpx.AuthInfo, error)
}

var (
	registry = map[string]AuthDriver{}
	envFunc = config.Env
)

func Init(env func(string, string) string, db *sql.DB) {
	if env != nil {
		envFunc = env
	}
	driver := "sqlite"
	if config.Database.Connections != nil {
		driver = config.Database.Connection().Driver
	} else if envFunc != nil {
		driver = envFunc("DB_CONNECTION", "sqlite")
	}
	registry["jwt"] = jwt.NewDriver(envFunc, db, driver)
	registry["oauth"] = oauth.NewDriver(envFunc, db, driver)
	registry["session"] = session.NewDriver(envFunc, db, driver)
}

func Driver(name string) AuthDriver {
	d, err := TryDriver(name)
	if err != nil {
		panic(err.Error())
	}
	return d
}

func TryDriver(name string) (AuthDriver, error) {
	d, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("auth: unknown driver %%q", name)
	}
	return d, nil
}

func ActiveDriver() AuthDriver {
	return Driver(activeDriverName())
}

func TryActiveDriver() (AuthDriver, error) {
	return TryDriver(activeDriverName())
}

func activeDriverName() string {
	if envFunc != nil {
		if name := envFunc("AUTH_DRIVER", "jwt"); name != "" {
			return name
		}
	}
	return "jwt"
}

func ActiveDriverName() string {
	return activeDriverName()
}

func Authenticate(r *http.Request) (*httpx.AuthInfo, error) {
	driver, err := TryActiveDriver()
	if err != nil {
		return nil, err
	}
	return driver.Authenticate(r)
}

func DefaultAuthMiddleware(ctx *httpx.Context, next func() httpx.Response) httpx.Response {
	info, err := Authenticate(ctx.Request())
	if err != nil {
		return ctx.Unauthorized(err.Error())
	}
	ctx.SetAuth(info)
	return next()
}

func Env() string {
	return activeDriverName()
}

`

const jwtSupportSource = `package jwt

import (
	"database/sql"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"net/http"
	"strings"
	"time"

	"%s/internal/httpx"

	"github.com/google/uuid"
)

var ErrInvalidToken = errors.New("jwt: invalid token")

type Claims struct {
	JTI       string         ` + "`" + `json:"jti,omitempty"` + "`" + `
	Subject   string         ` + "`" + `json:"sub,omitempty"` + "`" + `
	Issuer    string         ` + "`" + `json:"iss,omitempty"` + "`" + `
	ExpiresAt int64          ` + "`" + `json:"exp,omitempty"` + "`" + `
	IssuedAt  int64          ` + "`" + `json:"iat,omitempty"` + "`" + `
	Role      string         ` + "`" + `json:"role,omitempty"` + "`" + `
	Extra     map[string]any ` + "`" + `json:"-"` + "`" + `
}

type Driver struct {
	db        *sql.DB
	driver    string
	secret    string
	issuer    string
	expiry    int
	algorithm string
}

func NewDriver(env func(string, string) string, db *sql.DB, driver string) *Driver {
	expiry := 3600
	if v := env("JWT_EXPIRY", ""); v != "" {
		n := 0
		for _, c := range v { if c >= '0' && c <= '9' { n = n*10 + int(c-'0') } }
		if n > 0 { expiry = n }
	}
	return &Driver{
		db:        db,
		driver:    driver,
		secret:    env("JWT_SECRET", ""),
		issuer:    env("JWT_ISSUER", ""),
		expiry:    expiry,
		algorithm: env("JWT_ALGORITHM", "HS256"),
	}
}

func (d *Driver) SignToken(claims Claims) (string, error) {
	if d.secret == "" { return "", errors.New("jwt: secret not configured") }
	if d.db == nil { return "", errors.New("jwt: database not configured") }
	if claims.Subject == "" { return "", errors.New("jwt: missing subject") }
	now := time.Now().Unix()
	if claims.IssuedAt == 0 { claims.IssuedAt = now }
	if claims.ExpiresAt == 0 && d.expiry > 0 { claims.ExpiresAt = now + int64(d.expiry) }
	if claims.Issuer == "" && d.issuer != "" { claims.Issuer = d.issuer }
	if claims.JTI == "" { claims.JTI = uuid.New().String() }
	alg := d.algorithm
	if alg == "" { alg = "HS256" }
	header := base64URLEncode([]byte(` + "`" + `{"alg":"` + "`" + ` + alg + ` + "`" + `","typ":"JWT"}` + "`" + `))
	payload, err := json.Marshal(claims)
	if err != nil { return "", err }
	signingInput := header + "." + base64URLEncode(payload)
	sig, err := hmacSign([]byte(signingInput), []byte(d.secret), alg)
	if err != nil { return "", err }
	if err := d.registerToken(claims); err != nil { return "", err }
	return signingInput + "." + base64URLEncode(sig), nil
}

func (d *Driver) ValidateToken(token string) (Claims, error) {
	if d.secret == "" { return Claims{}, ErrInvalidToken }
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 { return Claims{}, ErrInvalidToken }
	headerJSON, err := base64URLDecode(parts[0])
	if err != nil { return Claims{}, ErrInvalidToken }
	var header struct{ Alg string ` + "`" + `json:"alg"` + "`" + ` }
	if err := json.Unmarshal(headerJSON, &header); err != nil { return Claims{}, ErrInvalidToken }
	alg := d.algorithm
	if alg == "" { alg = "HS256" }
	if header.Alg != alg { return Claims{}, ErrInvalidToken }
	signingInput := parts[0] + "." + parts[1]
	sig, err := base64URLDecode(parts[2])
	if err != nil { return Claims{}, ErrInvalidToken }
	if !hmacVerify([]byte(signingInput), sig, []byte(d.secret), alg) { return Claims{}, ErrInvalidToken }
	claimsJSON, err := base64URLDecode(parts[1])
	if err != nil { return Claims{}, ErrInvalidToken }
	var claims Claims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil { return Claims{}, ErrInvalidToken }
	if claims.ExpiresAt > 0 && time.Now().Unix() > claims.ExpiresAt { return Claims{}, ErrInvalidToken }
	if d.issuer != "" && claims.Issuer != d.issuer { return Claims{}, ErrInvalidToken }
	if claims.Subject == "" { return Claims{}, ErrInvalidToken }
	if err := d.checkAllowlist(claims); err != nil { return Claims{}, ErrInvalidToken }
	return claims, nil
}

func (d *Driver) Authenticate(r *http.Request) (*httpx.AuthInfo, error) {
	header := r.Header.Get("Authorization")
	if header == "" { return nil, errors.New("missing authorization header") }
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") { return nil, errors.New("invalid authorization header") }
	claims, err := d.ValidateToken(parts[1])
	if err != nil { return nil, err }
	return &httpx.AuthInfo{UserID: claims.Subject, Role: claims.Role}, nil
}

func (d *Driver) registerToken(claims Claims) error {
	expiresAt := time.Unix(claims.ExpiresAt, 0)
	query := "INSERT INTO jwt_tokens (jti, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)"
	_, err := d.db.Exec(bindPlaceholders(d.driver, query), claims.JTI, claims.Subject, expiresAt, time.Now().UTC())
	return err
}

func (d *Driver) checkAllowlist(claims Claims) error {
	if d.db == nil { return errors.New("jwt: database not configured") }
	var expiresAt time.Time
	err := d.db.QueryRow(bindPlaceholders(d.driver, "SELECT expires_at FROM jwt_tokens WHERE jti = ? AND user_id = ?"), claims.JTI, claims.Subject).Scan(&expiresAt)
	if err != nil { return err }
	if time.Now().After(expiresAt) { return errors.New("jwt: token expired") }
	return nil
}

func bindPlaceholders(driver, query string) string {
	if driver != "pgsql" && driver != "postgres" { return query }
	var b strings.Builder
	arg := 1
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			fmt.Fprintf(&b, "$%%d", arg)
			arg++
			continue
		}
		b.WriteByte(query[i])
	}
	return b.String()
}

func hmacSign(data, secret []byte, alg string) ([]byte, error) {
	var h func() hash.Hash
	switch alg {
	case "HS256": h = sha256.New
	case "HS384": h = sha512.New384
	case "HS512": h = sha512.New
	default: return nil, fmt.Errorf("jwt: unsupported algorithm %%s", alg)
	}
	mac := hmac.New(h, secret)
	mac.Write(data)
	return mac.Sum(nil), nil
}

func hmacVerify(data, sig, secret []byte, alg string) bool {
	expected, err := hmacSign(data, secret, alg)
	if err != nil { return false }
	return hmac.Equal(sig, expected)
}

func base64URLEncode(data []byte) string { return base64.RawURLEncoding.EncodeToString(data) }

func base64URLDecode(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }
`

const oauthSupportSource = `package oauth

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"%s/internal/httpx"
)

type Driver struct {
	db           *sql.DB
	driver       string
	clientID     string
	clientSecret string
	expiry       int
}

func NewDriver(env func(string, string) string, db *sql.DB, driver string) *Driver {
	expiry := 3600
	if v := env("OAUTH_TOKEN_EXPIRY", ""); v != "" {
		n := 0
		for _, c := range v { if c >= '0' && c <= '9' { n = n*10 + int(c-'0') } }
		if n > 0 { expiry = n }
	}
	return &Driver{db: db, driver: driver, clientID: env("OAUTH_CLIENT_ID", ""), clientSecret: env("OAUTH_CLIENT_SECRET", ""), expiry: expiry}
}

func (d *Driver) Authenticate(r *http.Request) (*httpx.AuthInfo, error) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") { return nil, errors.New("missing bearer token") }
	return d.ValidateToken(h[7:])
}

func (d *Driver) ValidateToken(token string) (*httpx.AuthInfo, error) {
	if d.db == nil { return nil, errors.New("oauth: database not configured") }
	var clientID string
	var expiresAt time.Time
	err := d.db.QueryRow(bindPlaceholders(d.driver, "SELECT client_id, expires_at FROM oauth_tokens WHERE token = ?"), token).Scan(&clientID, &expiresAt)
	if err == sql.ErrNoRows { return nil, errors.New("oauth: invalid token") }
	if err != nil { return nil, errors.New("oauth: database error") }
	if time.Now().After(expiresAt) { return nil, errors.New("oauth: token expired") }
	return &httpx.AuthInfo{UserID: clientID, Role: "client"}, nil
}

func (d *Driver) TokenEndpoint(ctx *httpx.Context) httpx.Response {
	r := ctx.Request()
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		return ctx.JSON(400, map[string]string{"error": "invalid_request", "error_description": "Content-Type must be application/x-www-form-urlencoded"})
	}
	if err := r.ParseForm(); err != nil {
		return ctx.JSON(400, map[string]string{"error": "invalid_request", "error_description": "malformed form body"})
	}
	if r.FormValue("grant_type") != "client_credentials" {
		return ctx.JSON(400, map[string]string{"error": "unsupported_grant_type", "error_description": "only client_credentials grant type is supported"})
	}
	clientID, clientSecret, ok := parseBasicAuth(r.Header.Get("Authorization"))
	if !ok {
		return ctx.JSON(401, map[string]string{"error": "invalid_client", "error_description": "missing or malformed Authorization header"})
	}
	idMatch := subtle.ConstantTimeCompare([]byte(clientID), []byte(d.clientID))
	secretMatch := subtle.ConstantTimeCompare([]byte(clientSecret), []byte(d.clientSecret))
	if idMatch&secretMatch != 1 {
		return ctx.JSON(401, map[string]string{"error": "invalid_client", "error_description": "invalid client credentials"})
	}
	token, err := generateToken()
	if err != nil { return ctx.Error(err) }
	expiresAt := time.Now().Add(time.Duration(d.expiry) * time.Second)
	if _, err := d.db.Exec(bindPlaceholders(d.driver, "INSERT INTO oauth_tokens (token, client_id, expires_at, created_at) VALUES (?, ?, ?, ?)"), token, clientID, expiresAt, time.Now().UTC()); err != nil {
		return ctx.Error(fmt.Errorf("oauth: failed to store token: %%w", err))
	}
	return ctx.JSON(200, map[string]any{"access_token": token, "token_type": "bearer", "expires_in": d.expiry})
}

func bindPlaceholders(driver, query string) string {
	if driver != "pgsql" && driver != "postgres" { return query }
	var b strings.Builder
	arg := 1
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			fmt.Fprintf(&b, "$%%d", arg)
			arg++
			continue
		}
		b.WriteByte(query[i])
	}
	return b.String()
}

func parseBasicAuth(header string) (string, string, bool) {
	if !strings.HasPrefix(header, "Basic ") { return "", "", false }
	decoded, err := base64.StdEncoding.DecodeString(header[6:])
	if err != nil { return "", "", false }
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 { return "", "", false }
	return parts[0], parts[1], true
}

func generateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil { return "", err }
	return hex.EncodeToString(buf), nil
}
`

const sessionSupportSource = `package session

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"%s/internal/httpx"

	"github.com/google/uuid"
)

var csrfConfig struct {
	secret []byte
	cookieName string
}
var sessionCookieName = "session_id"
var activeDriver *Driver

type Driver struct {
	db *sql.DB
	driver string
	cookieName string
	ttl int
}

func NewDriver(env func(string, string) string, db *sql.DB, driver string) *Driver {
	ttl := 86400
	if v := env("SESSION_TTL", ""); v != "" {
		n := 0
		for _, c := range v { if c >= '0' && c <= '9' { n = n*10 + int(c-'0') } }
		if n > 0 { ttl = n }
	}
	cookieName := env("SESSION_COOKIE", "session_id")
	sessionCookieName = cookieName
	csrfConfig.cookieName = env("CSRF_COOKIE", "csrf_token")
	if secret := env("SESSION_SECRET", ""); secret != "" { csrfConfig.secret = []byte(secret) } else { csrfConfig.secret = nil }
	d := &Driver{db: db, driver: driver, cookieName: cookieName, ttl: ttl}
	activeDriver = d
	return d
}

func (d *Driver) Authenticate(r *http.Request) (*httpx.AuthInfo, error) {
	cookie, err := r.Cookie(d.cookieName)
	if err != nil { return nil, errors.New("session: missing session cookie") }
	if d.db == nil { return nil, errors.New("session: database not configured") }
	var userID, role string
	var expiresAt time.Time
	err = d.db.QueryRow(bindPlaceholders(d.driver, "SELECT user_id, role, expires_at FROM sessions WHERE id = ?"), cookie.Value).Scan(&userID, &role, &expiresAt)
	if err == sql.ErrNoRows { return nil, errors.New("session: invalid or expired session") }
	if err != nil { return nil, errors.New("session: database error") }
	if time.Now().After(expiresAt) { return nil, errors.New("session: invalid or expired session") }
	return &httpx.AuthInfo{UserID: userID, Role: role}, nil
}

func (d *Driver) TTL() int { return d.ttl }
func (d *Driver) CookieName() string { return d.cookieName }

func driver() *Driver {
	if activeDriver == nil { panic("session: driver not initialized") }
	return activeDriver
}

func Create(ctx *httpx.Context, userID, role string) (httpx.Response, error) {
	d := driver()
	sessionID := uuid.New().String()
	expiresAt := time.Now().UTC().Add(time.Duration(d.ttl) * time.Second)
	if _, err := d.db.Exec(bindPlaceholders(d.driver, "INSERT INTO sessions (id, user_id, role, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)"), sessionID, userID, role, expiresAt, time.Now().UTC(), time.Now().UTC()); err != nil {
		return httpx.Response{}, err
	}
	resp := ctx.JSON(200, map[string]string{"status": "ok"})
	resp = resp.WithCookie(&http.Cookie{Name: d.cookieName, Value: sessionID, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, Expires: expiresAt})
	if len(csrfConfig.secret) > 0 {
		resp = resp.WithCookie(newCSRFCookie(sessionID))
	}
	return resp, nil
}

func CSRF(ctx *httpx.Context, next func() httpx.Response) httpx.Response {
	if len(csrfConfig.secret) == 0 { return ctx.Forbidden("CSRF secret not configured") }
	if strings.HasPrefix(ctx.Request().Header.Get("Authorization"), "Bearer ") { return next() }
	sessionID := sessionIDFromRequest(ctx.Request())
	method := ctx.Request().Method
	if method == "GET" || method == "HEAD" || method == "OPTIONS" {
		resp := next()
		if _, err := ctx.Cookie(csrfConfig.cookieName); err != nil { resp = resp.WithCookie(newCSRFCookie(sessionID)) }
		return resp
	}
	token := ctx.Request().Header.Get("X-CSRF-TOKEN")
	if token == "" { return ctx.Forbidden("CSRF token missing") }
	if !validateCSRFToken(token, sessionID, csrfConfig.secret) { return ctx.Forbidden("CSRF token invalid") }
	return next()
}

func sessionIDFromRequest(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil { return "" }
	return c.Value
}

func bindPlaceholders(driver, query string) string {
	if driver != "pgsql" && driver != "postgres" { return query }
	var b strings.Builder
	arg := 1
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			fmt.Fprintf(&b, "$%%d", arg)
			arg++
			continue
		}
		b.WriteByte(query[i])
	}
	return b.String()
}

func newCSRFCookie(sessionID string) *http.Cookie {
	return &http.Cookie{Name: csrfConfig.cookieName, Value: generateCSRFToken(sessionID, csrfConfig.secret), Path: "/", HttpOnly: false, Secure: true, SameSite: http.SameSiteStrictMode}
}

func generateCSRFToken(sessionID string, secret []byte) string {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil { panic("csrf: failed to generate random nonce: " + err.Error()) }
	return hex.EncodeToString(nonce) + "." + hex.EncodeToString(computeHMAC(nonce, []byte(sessionID), secret))
}

func validateCSRFToken(token, sessionID string, secret []byte) bool {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 { return false }
	nonce, err := hex.DecodeString(parts[0])
	if err != nil { return false }
	sig, err := hex.DecodeString(parts[1])
	if err != nil { return false }
	return hmac.Equal(sig, computeHMAC(nonce, []byte(sessionID), secret))
}

func computeHMAC(nonce, sessionID, secret []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write(nonce)
	mac.Write(sessionID)
	return mac.Sum(nil)
}
`

const jobsSupportSource = `package jobs

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type Job interface { Handle() error }

type JobEntry struct {
	Schedule string
	Job Job
	maxRetries int
	retryDelay time.Duration
	timeout time.Duration
	allowOverlap bool
}

type Scheduler struct { entries []*JobEntry }

func Cron(fn func(s *Scheduler)) *Scheduler { s := &Scheduler{}; fn(s); return s }

func (s *Scheduler) Job(schedule string, job Job) *JobEntry { e := &JobEntry{Schedule: schedule, Job: job}; s.entries = append(s.entries, e); return e }

func (s *Scheduler) Entries() []*JobEntry { return s.entries }

func (e *JobEntry) MaxRetries(n int) *JobEntry { e.maxRetries = n; return e }
func (e *JobEntry) RetryDelay(d time.Duration) *JobEntry { e.retryDelay = d; return e }
func (e *JobEntry) Timeout(d time.Duration) *JobEntry { e.timeout = d; return e }
func (e *JobEntry) SkipIfRunning() *JobEntry { e.allowOverlap = false; return e }
func (e *JobEntry) AllowOverlap() *JobEntry { e.allowOverlap = true; return e }

func (s *Scheduler) Start(ctx context.Context) {
	c := cron.New()
	for _, entry := range s.entries {
		entry := entry
		var mu sync.Mutex
		running := false
		_, err := c.AddFunc(entry.Schedule, func() {
			if !entry.allowOverlap {
				mu.Lock()
				if running { mu.Unlock(); log.Printf("job %T skipped: previous run still in progress", entry.Job); return }
				running = true
				mu.Unlock()
				defer func() { mu.Lock(); running = false; mu.Unlock() }()
			}
			runJob(entry)
		})
		if err != nil { log.Printf("job %T schedule %q rejected: %v", entry.Job, entry.Schedule, err) }
	}
	c.Start()
	<-ctx.Done()
	c.Stop()
}

func runJob(entry *JobEntry) {
	attempts := entry.maxRetries + 1
	for i := 0; i < attempts; i++ {
		var err error
		if entry.timeout > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), entry.timeout)
			done := make(chan error, 1)
			go func() { done <- entry.Job.Handle() }()
			select { case err = <-done: case <-ctx.Done(): err = fmt.Errorf("job timed out after %s", entry.timeout) }
			cancel()
		} else { err = entry.Job.Handle() }
		if err == nil { return }
		log.Printf("job %T failed (attempt %d/%d): %v", entry.Job, i+1, attempts, err)
		if i < attempts-1 && entry.retryDelay > 0 { time.Sleep(entry.retryDelay) }
	}
}
`

const serverMainSource = `package main

import (
	"log"
	"net/http"

	"%s/config"
	"%s/routes"
)

func main() {
	config.Init()
	addr := ":" + config.App.Port
	log.Printf("listening on %%s", addr)
	mux := http.NewServeMux()
	routes.API.RegisterRoutes(mux)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
`

const commandsServerMainSource = `package main

import (
	"os"

	"%s/app/commands"
)

func main() {
	commands.NewApp().Run(os.Args[1:])
}
`

const serverMainWithDatabaseSource = `package main

import (
	"log"
	"net/http"

	"%s/app/models"
	"%s/config"
	"%s/routes"
)

func main() {
	config.Init()
	models.SetDB(config.Database.OpenGORM())
	addr := ":" + config.App.Port
	log.Printf("listening on %%s", addr)
	mux := http.NewServeMux()
	routes.API.RegisterRoutes(mux)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
`

const exportedEncryptionSupportSource = `package models

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

type exportedEncryptedModel interface {
	encryptedFields() []exportedEncryptedField
}

type exportedEncryptedField struct {
	Column        string
	ColumnV2      string
	Deterministic bool
	Marshal       func() ([]byte, error)
	Unmarshal     func([]byte) error
	Ciphertext     func() string
	CiphertextV2   func() *string
	SetCiphertext  func(string)
	SetCiphertextV2 func(*string)
}

func encryptModelFields(model exportedEncryptedModel) error {
	key, err := exportedEncryptionKey()
	if err != nil {
		return err
	}
	for _, field := range model.encryptedFields() {
		plain, err := field.Marshal()
		if err != nil {
			return err
		}
		var ciphertext string
		if field.Deterministic {
			ciphertext, err = encryptDeterministic(key, plain)
		} else {
			ciphertext, err = encryptRandom(key, plain)
		}
		if err != nil {
			return fmt.Errorf("encrypting %s: %w", field.Column, err)
		}
		field.SetCiphertext(ciphertext)
		field.SetCiphertextV2(nil)
	}
	return nil
}

func decryptModelFields(model exportedEncryptedModel) error {
	key, err := exportedEncryptionKey()
	if err != nil {
		return err
	}
	for _, field := range model.encryptedFields() {
		ciphertext := field.Ciphertext()
		if v2 := field.CiphertextV2(); v2 != nil && *v2 != "" {
			ciphertext = *v2
		}
		if ciphertext == "" {
			continue
		}
		var plain []byte
		if field.Deterministic {
			plain, err = decryptDeterministic(key, ciphertext)
		} else {
			plain, err = decryptRandom(key, ciphertext)
		}
		if err != nil {
			return fmt.Errorf("decrypting %s: %w", field.Column, err)
		}
		if err := field.Unmarshal(plain); err != nil {
			return err
		}
	}
	return nil
}

func exportedEncryptionKey() ([]byte, error) {
	raw := os.Getenv("APP_ENCRYPTION_KEY")
	if raw == "" {
		raw = os.Getenv("ENCRYPTION_KEY")
	}
	if raw == "" {
		return nil, errors.New("APP_ENCRYPTION_KEY is required for encrypted columns")
	}
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	key := []byte(raw)
	if len(key) != 32 {
		return nil, fmt.Errorf("APP_ENCRYPTION_KEY must be 32 bytes or base64-encoded 32 bytes, got %d bytes", len(key))
	}
	return key, nil
}

func encryptDeterministic(key, plaintext []byte) (string, error) {
	if len(key) != 32 {
		return "", errors.New("deterministic encryption key must be 32 bytes")
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(plaintext)
	iv := mac.Sum(nil)[:aes.BlockSize]
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	stream := cipher.NewCTR(block, iv)
	ciphertext := make([]byte, len(plaintext))
	stream.XORKeyStream(ciphertext, plaintext)
	out := make([]byte, aes.BlockSize+len(ciphertext))
	copy(out[:aes.BlockSize], iv)
	copy(out[aes.BlockSize:], ciphertext)
	return base64.StdEncoding.EncodeToString(out), nil
}

func decryptDeterministic(key []byte, encoded string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	if len(data) < aes.BlockSize {
		return nil, errors.New("ciphertext too short")
	}
	iv := data[:aes.BlockSize]
	ciphertext := data[aes.BlockSize:]
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	stream := cipher.NewCTR(block, iv)
	plaintext := make([]byte, len(ciphertext))
	stream.XORKeyStream(plaintext, ciphertext)
	return plaintext, nil
}

func encryptRandom(key, plaintext []byte) (string, error) {
	if len(key) != 32 {
		return "", errors.New("random encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, plaintext, nil)), nil
}

func decryptRandom(key []byte, encoded string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce := data[:gcm.NonceSize()]
	ciphertext := data[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
`

const integritySupportGenericSource = `
func createIntegrityRecord(db *gorm.DB, table string, record any, immutable bool, order string, columns []string) error {
	if db == nil {
		return errors.New("models.DB is not configured")
	}
	if err := ensureUUIDField(record, "ID"); err != nil {
		return err
	}
	if immutable {
		if err := setUUIDField(record, "VersionID", newUUIDV7()); err != nil {
			return err
		}
	}
	prev, err := latestIntegrityHash(db, table, order)
	if err != nil {
		return err
	}
	if err := setBytesField(record, "PrevHash", prev); err != nil {
		return err
	}
	rowHash := computeIntegrityHash(prev, record, columns)
	if err := setBytesField(record, "RowHash", rowHash); err != nil {
		return err
	}
	return db.Create(record).Error
}

func updateImmutableRecord(db *gorm.DB, table string, record any, order string, columns []string) error {
	if db == nil {
		return errors.New("models.DB is not configured")
	}
	id, err := getUUIDField(record, "ID")
	if err != nil {
		return err
	}
	versionID, err := getUUIDField(record, "VersionID")
	if err != nil {
		return err
	}
	var latest struct {
		VersionID uuid.UUID ` + "`" + `gorm:"column:version_id"` + "`" + `
		RowHash []byte ` + "`" + `gorm:"column:row_hash"` + "`" + `
	}
	if err := db.Table(table).Select("version_id, row_hash").Where("id = ?", id).Order("version_id DESC").Limit(1).Scan(&latest).Error; err != nil {
		return err
	}
	if latest.VersionID == uuid.Nil {
		return fmt.Errorf("no current version found for %s id=%s", table, id)
	}
	if latest.VersionID != versionID {
		return &StaleVersionError{Table: table, EntityID: id.String(), ExpectedVersion: latest.VersionID.String(), ActualVersion: versionID.String()}
	}
	if err := setBytesField(record, "PrevHash", latest.RowHash); err != nil {
		return err
	}
	if err := setUUIDField(record, "VersionID", newUUIDV7()); err != nil {
		return err
	}
	rowHash := computeIntegrityHash(latest.RowHash, record, columns)
	if err := setBytesField(record, "RowHash", rowHash); err != nil {
		return err
	}
	return db.Create(record).Error
}

func latestIntegrityHash(db *gorm.DB, table, order string) ([]byte, error) {
	var row struct { RowHash []byte ` + "`" + `gorm:"column:row_hash"` + "`" + ` }
	if err := db.Table(table).Select("row_hash").Order(order).Limit(1).Scan(&row).Error; err != nil {
		return nil, err
	}
	if len(row.RowHash) == 0 {
		return append([]byte(nil), genesisHash...), nil
	}
	return append([]byte(nil), row.RowHash...), nil
}

func verifyIntegrityRow(table string, record any, columns []string) error {
	prev, err := getBytesField(record, "PrevHash")
	if err != nil {
		return err
	}
	rowHash, err := getBytesField(record, "RowHash")
	if err != nil {
		return err
	}
	expected := computeIntegrityHash(prev, record, columns)
	if !bytes.Equal(expected, rowHash) {
		return &ChainError{Table: table, Position: -1, Expected: expected, Actual: rowHash}
	}
	return nil
}

func verifyIntegrityRecords[T any](table string, records []T, columns []string) error {
	prev := append([]byte(nil), genesisHash...)
	for i := range records {
		record := &records[i]
		prevHash, err := getBytesField(record, "PrevHash")
		if err != nil {
			return err
		}
		if !bytes.Equal(prevHash, prev) {
			return &ChainError{Table: table, Position: i, Expected: prev, Actual: prevHash}
		}
		rowHash, err := getBytesField(record, "RowHash")
		if err != nil {
			return err
		}
		expected := computeIntegrityHash(prev, record, columns)
		if !bytes.Equal(expected, rowHash) {
			return &ChainError{Table: table, Position: i, Expected: expected, Actual: rowHash}
		}
		prev = append([]byte(nil), rowHash...)
	}
	return nil
}

func computeIntegrityHash(prevHash []byte, record any, columns []string) []byte {
	h := sha256.New()
	h.Write(prevHash)
	h.Write(canonicalIntegrityBytes(record, columns))
	return h.Sum(nil)
}

func canonicalIntegrityBytes(record any, columns []string) []byte {
	rv := reflect.ValueOf(record)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	rt := rv.Type()
	fieldByColumn := map[string]reflect.Value{}
	for i := 0; i < rt.NumField(); i++ {
		column := gormColumnName(rt.Field(i))
		if column == "" || column == "row_hash" || column == "prev_hash" {
			continue
		}
		fieldByColumn[column] = rv.Field(i)
	}
	var out []byte
	for _, column := range columns {
		out = append(out, []byte(column)...)
		out = append(out, 0)
		field, ok := fieldByColumn[column]
		if !ok || (field.Kind() == reflect.Ptr && field.IsNil()) {
			out = append(out, []byte("null")...)
			out = append(out, 0)
			continue
		}
		if field.Kind() == reflect.Ptr {
			field = field.Elem()
		}
		data, err := json.Marshal(field.Interface())
		if err != nil {
			data = []byte(fmt.Sprint(field.Interface()))
		}
		out = append(out, data...)
		out = append(out, 0)
	}
	return out
}

func gormColumnName(field reflect.StructField) string {
	tag := field.Tag.Get("gorm")
	if tag == "-" {
		return ""
	}
	for _, part := range strings.Split(tag, ";") {
		if strings.HasPrefix(part, "column:") {
			return strings.TrimPrefix(part, "column:")
		}
	}
	return ""
}

func ensureUUIDField(record any, name string) error {
	current, err := getUUIDField(record, name)
	if err != nil {
		return err
	}
	if current != uuid.Nil {
		return nil
	}
	return setUUIDField(record, name, newUUIDV7())
}

func getUUIDField(record any, name string) (uuid.UUID, error) {
	field, err := structField(record, name)
	if err != nil {
		return uuid.Nil, err
	}
	if value, ok := field.Interface().(uuid.UUID); ok {
		return value, nil
	}
	return uuid.Nil, fmt.Errorf("%s is not a uuid.UUID", name)
}

func setUUIDField(record any, name string, value uuid.UUID) error {
	field, err := structField(record, name)
	if err != nil {
		return err
	}
	if !field.CanSet() {
		return fmt.Errorf("%s cannot be set", name)
	}
	field.Set(reflect.ValueOf(value))
	return nil
}

func getBytesField(record any, name string) ([]byte, error) {
	field, err := structField(record, name)
	if err != nil {
		return nil, err
	}
	if value, ok := field.Interface().([]byte); ok {
		return value, nil
	}
	return nil, fmt.Errorf("%s is not []byte", name)
}

func setBytesField(record any, name string, value []byte) error {
	field, err := structField(record, name)
	if err != nil {
		return err
	}
	if !field.CanSet() {
		return fmt.Errorf("%s cannot be set", name)
	}
	field.Set(reflect.ValueOf(append([]byte(nil), value...)))
	return nil
}

func structField(record any, name string) (reflect.Value, error) {
	rv := reflect.ValueOf(record)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return reflect.Value{}, errors.New("record must be a non-nil pointer")
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return reflect.Value{}, errors.New("record must point to a struct")
	}
	field := rv.FieldByName(name)
	if !field.IsValid() {
		return reflect.Value{}, fmt.Errorf("field %s not found", name)
	}
	return field, nil
}

func newUUIDV7() uuid.UUID {
	var id uuid.UUID
	uuidV7Mu.Lock()
	defer uuidV7Mu.Unlock()
	ms := uint64(time.Now().UnixMilli())
	if ms == uuidV7LastMillis {
		uuidV7Sequence++
	} else {
		uuidV7LastMillis = ms
		uuidV7Sequence = 0
	}
	id[0] = byte(ms >> 40)
	id[1] = byte(ms >> 32)
	id[2] = byte(ms >> 24)
	id[3] = byte(ms >> 16)
	id[4] = byte(ms >> 8)
	id[5] = byte(ms)
	if _, err := io.ReadFull(rand.Reader, id[6:]); err != nil {
		return uuid.New()
	}
	id[6] = 0x70 | byte(uuidV7Sequence>>8)&0x0f
	id[7] = byte(uuidV7Sequence)
	id[8] = (id[8] & 0x3f) | 0x80
	return id
}
`
