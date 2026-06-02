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

// Finding records a lossy or unsupported export step.
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
	ex.addGeneratedSubsystemFindings()
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
	content := fmt.Sprintf("module %s\n\ngo 1.24.0\n\nrequire (\n\tgithub.com/go-playground/validator/v10 v10.30.1\n\tgithub.com/google/uuid v1.6.0\n\tgorm.io/driver/mysql v1.6.0\n\tgorm.io/driver/postgres v1.6.0\n\tgorm.io/driver/sqlite v1.6.0\n\tgorm.io/gorm v1.31.1\n)\n", e.modulePath)
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
	replaced := strings.ReplaceAll(string(data), e.sourceModule+"/", e.modulePath+"/")
	formatted, err := format.Source([]byte(replaced))
	if err != nil {
		return nil, fmt.Errorf("formatting exported GraphQL source %s: %w", path, err)
	}
	return formatted, nil
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
	b.WriteString(fmt.Sprintf("type %s struct { db *gorm.DB }\n\n", queryName))
	b.WriteString(fmt.Sprintf("func Query%s() *%s { return &%s{db: DB.Model(&%s{})} }\n\n", structName, queryName, queryName, structName))
	b.WriteString(fmt.Sprintf("func (q *%s) SelectPublic() *%s { return q }\n", queryName, queryName))
	b.WriteString(fmt.Sprintf("func (q *%s) SelectOwner() *%s { return q }\n", queryName, queryName))
	b.WriteString(fmt.Sprintf("func (q *%s) SelectAll() *%s { return q }\n", queryName, queryName))
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
		e.result.Findings = append(e.result.Findings, Finding{File: filepath.Join("database", "actions", set.Model), Rule: "actions_audit", Message: "actions are exported as plain methods; audit behavior needs manual review"})
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

	"%s/internal/httpx"
)

type Context = httpx.Context

var ErrUnauthorized = errors.New("unauthorized")
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
		if action.HasResult {
			b.WriteString("\t\treturn nil, ErrUnauthorized\n")
		} else {
			b.WriteString("\t\treturn ErrUnauthorized\n")
		}
		b.WriteString("\t}\n")
		b.WriteString("\t_ = roleID\n")
		if action.HasResult {
			b.WriteString(fmt.Sprintf("\treturn action.%s(ctx, m)\n", action.Name))
		} else {
			b.WriteString(fmt.Sprintf("\treturn action.%s(ctx, m)\n", action.Name))
		}
		b.WriteString("}\n\n")
	}
	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return []byte(b.String()), fmt.Errorf("formatting exported action wiring for %s: %w", set.Model, err)
	}
	return formatted, nil
}

func (e *exporter) writeSQLMigrations(tables []*schema.Table, views []*schema.View) error {
	migrations, err := e.generateSQLMigrations(tables, views)
	if err != nil {
		return err
	}
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

type sqlMigration struct {
	Name string
	Up   string
	Down string
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
			statements = append(statements, "ALTER TABLE "+quoteIdent(op.Table)+" ADD COLUMN "+columnSQL(col))
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
		cols = append(cols, "\t"+columnSQL(col))
		if col.IsPrimaryKey {
			pk = append(pk, quoteIdent(col.Name))
		}
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

func columnSQL(col *schema.Column) string {
	var b strings.Builder
	b.WriteString(quoteIdent(col.Name))
	b.WriteByte(' ')
	b.WriteString(sqlType(col))
	if col.IsPrimaryKey {
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
	if err := e.writeFile(filepath.Join("app", "http", "auth", "auth.go"), []byte(fmt.Sprintf(authSupportSource, e.modulePath, e.modulePath, e.modulePath))); err != nil {
		return err
	}
	return e.writeFile(filepath.Join("app", "http", "auth", "jwt", "jwt.go"), []byte(fmt.Sprintf(jwtSupportSource, e.modulePath)))
}

func (e *exporter) writeServerMain() error {
	scan, err := generator.ScanConfigs(e.project.Layout.ConfigDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(e.project.Services) > 0 {
		data, err := e.generateMultiServiceServerMain(scan != nil && scan.HasDatabaseConfig)
		if err != nil {
			return err
		}
		return e.writeFile(filepath.Join("cmd", "server", "main.go"), data)
	}
	if e.hasGraphQLPackage() {
		data, err := e.generateServerMain(scan != nil && scan.HasDatabaseConfig, true)
		if err != nil {
			return err
		}
		return e.writeFile(filepath.Join("cmd", "server", "main.go"), data)
	}
	source := serverMainSource
	if scan != nil && scan.HasDatabaseConfig {
		source = serverMainWithDatabaseSource
		return e.writeFile(filepath.Join("cmd", "server", "main.go"), []byte(fmt.Sprintf(source, e.modulePath, e.modulePath, e.modulePath)))
	}
	return e.writeFile(filepath.Join("cmd", "server", "main.go"), []byte(fmt.Sprintf(source, e.modulePath, e.modulePath)))
}

func (e *exporter) generateServerMain(hasDatabaseConfig, hasGraphQL bool) ([]byte, error) {
	var b strings.Builder
	b.WriteString("package main\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t\"log\"\n")
	b.WriteString("\t\"net/http\"\n\n")
	if hasGraphQL {
		b.WriteString(fmt.Sprintf("\t\"%s/app/graphql\"\n", e.modulePath))
	}
	if hasDatabaseConfig {
		b.WriteString(fmt.Sprintf("\t\"%s/app/models\"\n", e.modulePath))
	}
	b.WriteString(fmt.Sprintf("\t\"%s/config\"\n", e.modulePath))
	b.WriteString(fmt.Sprintf("\t\"%s/routes\"\n", e.modulePath))
	b.WriteString(")\n\n")
	b.WriteString("func main() {\n")
	b.WriteString("\tconfig.Init()\n")
	if hasDatabaseConfig {
		b.WriteString("\tmodels.SetDB(config.Database.OpenGORM())\n")
	}
	b.WriteString("\tmux := http.NewServeMux()\n")
	b.WriteString("\tmux.Handle(\"/\", routes.API)\n")
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

func (e *exporter) generateMultiServiceServerMain(hasDatabaseConfig bool) ([]byte, error) {
	var b strings.Builder
	b.WriteString("package main\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t\"log\"\n")
	b.WriteString("\t\"net/http\"\n\n")
	if hasDatabaseConfig {
		b.WriteString(fmt.Sprintf("\t\"%s/app/models\"\n", e.modulePath))
	}
	b.WriteString(fmt.Sprintf("\t\"%s/config\"\n", e.modulePath))
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
	b.WriteString("\tmux := http.NewServeMux()\n")
	for i, svc := range e.project.Services {
		prefix := "/" + strings.Trim(svc.Name, "/") + "/"
		if i == 0 && svc.Name == "api" {
			prefix = "/api/"
		}
		b.WriteString(fmt.Sprintf("\tmux.Handle(%q, %sRoutes.API)\n", prefix, safeImportAlias(svc.Name)))
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

func (e *exporter) addGeneratedSubsystemFindings() {
	checks := []struct {
		path string
		rule string
		msg  string
	}{
		{"app/http/auth", "generated_auth", "auth runtime is exported with standalone JWT support; sessions, OAuth, and JWT allowlist/revocation need manual review"},
		{"app/jobs", "generated_jobs", "scheduler runtime is exported with minimal standalone support in v1"},
		{"app/commands", "generated_commands", "Pickle command runtime is not lowered in v1"},
		{"database/policies", "rbac_policy_export", "RBAC role grants are reflected in exported gates where statically derivable; policy runners and changelog state are not exported"},
		{"database/policies/graphql", "generated_graphql_policies", "generated GraphQL schema preserves derived exposure state; policy runner/changelog migration commands are not exported in v1"},
	}
	for _, c := range checks {
		if _, err := os.Stat(filepath.Join(e.project.Dir, c.path)); err == nil {
			e.result.Findings = append(e.result.Findings, Finding{File: c.path, Rule: c.rule, Message: c.msg})
		}
	}
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
	b.WriteString("\n")
	if len(e.result.Findings) == 0 {
		b.WriteString("## Manual Review\n\n")
		b.WriteString("No unsupported export findings.\n")
	} else {
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

func (e *exporter) writeFindingSection(b *strings.Builder, title, category string) {
	var findings []Finding
	for _, finding := range e.result.Findings {
		if findingCategory(finding.Rule) == category {
			findings = append(findings, finding)
		}
	}
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
	case "generated_auth", "generated_jobs", "rbac_policy_export":
		return "partial"
	case "generated_graphql", "generated_graphql_policies", "generated_commands", "generated_policies", "generated_actions":
		return "omitted"
	case "encrypted_columns", "integrity_tables":
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
	switch c.Driver {
	case "pgsql", "postgres":
		params := url.Values{}
		params.Set("sslmode", "disable")
		for k, v := range c.Options { params.Set(k, v) }
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?%s", url.PathEscape(c.User), url.PathEscape(c.Password), c.Host, c.Port, c.Name, params.Encode())
	case "mysql":
		params := url.Values{}
		params.Set("parseTime", "true")
		for k, v := range c.Options { params.Set(k, v) }
		return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?%s", url.PathEscape(c.User), url.PathEscape(c.Password), c.Host, c.Port, c.Name, params.Encode())
	case "sqlite":
		return c.Name
	default:
		panic("unsupported database driver: " + c.Driver)
	}
}

func (c ConnectionConfig) driverName() string {
	switch c.Driver {
	case "pgsql", "postgres": return "pgx"
	case "mysql": return "mysql"
	case "sqlite": return "sqlite3"
	default: panic("unsupported database driver: " + c.Driver)
	}
}

func OpenDB(conn ConnectionConfig) *sql.DB {
	db, err := sql.Open(conn.driverName(), conn.DSN())
	if err != nil { log.Fatalf("config: failed to open database: %v", err) }
	if err := db.Ping(); err != nil { log.Fatalf("config: failed to ping database: %v", err) }
	return db
}

func OpenGORM(conn ConnectionConfig) *gorm.DB {
	sqlDB := OpenDB(conn)
	var dialector gorm.Dialector
	switch conn.Driver {
	case "pgsql", "postgres":
		dialector = postgres.New(postgres.Config{Conn: sqlDB})
	case "mysql":
		dialector = mysql.New(mysql.Config{Conn: sqlDB})
	case "sqlite":
		dialector = sqlite.Dialector{Conn: sqlDB}
	default:
		panic("unsupported database driver: " + conn.Driver)
	}
	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil { log.Fatalf("config: failed to initialize gorm: %v", err) }
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
	key := d.Default
	if len(name) > 0 && name[0] != "" { key = name[0] }
	conn, ok := d.Connections[key]
	if !ok { log.Fatalf("config: unknown database connection: %s", key) }
	return conn
}

func (d DatabaseConfig) Open(name ...string) *sql.DB { return OpenDB(d.Connection(name...)) }

func (d DatabaseConfig) OpenGORM(name ...string) *gorm.DB { return OpenGORM(d.Connection(name...)) }
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
	"net/http"
	"strings"
)

type Controller struct{}
type Response struct { Status int; StatusCode int; Body any; Headers map[string]string }
type AuthInfo struct { UserID string; Role string }
type Context struct { request *http.Request; auth *AuthInfo; params map[string]string }

func NewContext(r *http.Request) *Context { return &Context{request: r, params: map[string]string{}} }
func (c *Context) Request() *http.Request { return c.request }
func (c *Context) Param(name string) string { return c.params[name] }
func (c *Context) BearerToken() string { h := c.request.Header.Get("Authorization"); parts := strings.Fields(h); if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") { return parts[1] }; return "" }
func (c *Context) Auth() *AuthInfo { if c.auth == nil { return &AuthInfo{} }; return c.auth }
func (c *Context) SetAuth(info *AuthInfo) { c.auth = info }
func (c *Context) IsAuthenticated() bool { return c.auth != nil && c.auth.UserID != "" }
func (c *Context) IsAdmin() bool { return c.Auth().Role == "admin" }
func (c *Context) HasAnyRole(roles ...string) bool { current := c.Auth().Role; for _, role := range roles { if role == current { return true } }; return false }
func (c *Context) JSON(status int, body any) Response { return Response{Status: status, StatusCode: status, Body: body} }
func (c *Context) Error(err error) Response { return c.JSON(500, map[string]string{"error": err.Error()}) }
func (c *Context) BadRequest(msg string) Response { return c.JSON(400, map[string]string{"error": msg}) }
func (c *Context) Unauthorized(msg string) Response { return c.JSON(401, map[string]string{"error": msg}) }
func (c *Context) NotFound(msg string) Response { return c.JSON(404, map[string]string{"error": msg}) }
func (c *Context) NoContent() Response { return Response{Status: 204, StatusCode: 204} }

func (r Response) Write(w http.ResponseWriter) { for k, v := range r.Headers { w.Header().Set(k, v) }; status := r.Status; if status == 0 { status = r.StatusCode }; if status == 0 { status = 200 }; w.WriteHeader(status); if r.Body != nil { _ = json.NewEncoder(w).Encode(r.Body) } }

type HandlerFunc func(*Context) Response
type MiddlewareFunc func(*Context, func() Response) Response
type route struct { method string; path string; handler HandlerFunc; middleware []MiddlewareFunc }
type Router struct{ prefix string; middleware []MiddlewareFunc; routes []route }
func Routes(fn func(*Router)) *Router { r := &Router{}; fn(r); return r }
func (r *Router) Group(path string, args ...any) {
	child := &Router{prefix: joinPath(r.prefix, path), middleware: append([]MiddlewareFunc{}, r.middleware...)}
	for _, arg := range args {
		switch v := arg.(type) {
		case MiddlewareFunc:
			child.middleware = append(child.middleware, v)
		case func(*Router):
			v(child)
		}
	}
	r.routes = append(r.routes, child.routes...)
}
func (r *Router) Get(path string, handler HandlerFunc, middleware ...MiddlewareFunc) { r.add("GET", path, handler, middleware...) }
func (r *Router) Post(path string, handler HandlerFunc, middleware ...MiddlewareFunc) { r.add("POST", path, handler, middleware...) }
func (r *Router) Put(path string, handler HandlerFunc, middleware ...MiddlewareFunc) { r.add("PUT", path, handler, middleware...) }
func (r *Router) Patch(path string, handler HandlerFunc, middleware ...MiddlewareFunc) { r.add("PATCH", path, handler, middleware...) }
func (r *Router) Delete(path string, handler HandlerFunc, middleware ...MiddlewareFunc) { r.add("DELETE", path, handler, middleware...) }
func (r *Router) add(method, path string, handler HandlerFunc, middleware ...MiddlewareFunc) { r.routes = append(r.routes, route{method: method, path: joinPath(r.prefix, path), handler: handler, middleware: append(append([]MiddlewareFunc{}, r.middleware...), middleware...)}) }
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	defer func() { if recovered := recover(); recovered != nil { http.Error(w, "internal server error", http.StatusInternalServerError) } }()
	for _, rt := range r.routes {
		params, ok := matchPath(rt.path, req.URL.Path)
		if rt.method != req.Method || !ok { continue }
		ctx := NewContext(req); ctx.params = params
		i := len(rt.middleware) - 1
		var next func() Response
		next = func() Response {
			if i < 0 { return rt.handler(ctx) }
			mw := rt.middleware[i]; i--; return mw(ctx, next)
		}
		next().Write(w)
		return
	}
	http.NotFound(w, req)
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
func RateLimit(rps, burst int) MiddlewareFunc { return func(ctx *Context, next func() Response) Response { return next() } }
`

const authSupportSource = `package auth

import (
	"database/sql"
	"fmt"
	"net/http"

	"%s/app/http/auth/jwt"
	"%s/config"
	"%s/internal/httpx"
)

type AuthDriver interface {
	Authenticate(r *http.Request) (*httpx.AuthInfo, error)
}

type unsupportedDriver struct{ name string }

func (d unsupportedDriver) Authenticate(r *http.Request) (*httpx.AuthInfo, error) {
	return nil, fmt.Errorf("auth: %%s driver requires manual implementation after export", d.name)
}

var (
	jwtDriver = jwt.NewDriver(config.Env)
	registry = map[string]AuthDriver{
		"jwt": jwtDriver,
		"oauth": unsupportedDriver{name: "oauth"},
		"session": unsupportedDriver{name: "session"},
	}
	envFunc = config.Env
)

func Init(env func(string, string) string, db *sql.DB) {
	if env != nil {
		envFunc = env
		jwtDriver = jwt.NewDriver(env)
		registry["jwt"] = jwtDriver
	}
	_ = db
}

func Driver(name string) AuthDriver {
	d, ok := registry[name]
	if !ok {
		return unsupportedDriver{name: name}
	}
	return d
	}

func ActiveDriver() AuthDriver {
	return Driver(activeDriverName())
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
	return ActiveDriver().Authenticate(r)
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
	secret    string
	issuer    string
	expiry    int
	algorithm string
}

func NewDriver(env func(string, string) string) *Driver {
	expiry := 3600
	if v := env("JWT_EXPIRY", ""); v != "" {
		n := 0
		for _, c := range v { if c >= '0' && c <= '9' { n = n*10 + int(c-'0') } }
		if n > 0 { expiry = n }
	}
	return &Driver{
		secret:    env("JWT_SECRET", ""),
		issuer:    env("JWT_ISSUER", ""),
		expiry:    expiry,
		algorithm: env("JWT_ALGORITHM", "HS256"),
	}
}

func (d *Driver) SignToken(claims Claims) (string, error) {
	if d.secret == "" { return "", errors.New("jwt: secret not configured") }
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
	if err := http.ListenAndServe(addr, routes.API); err != nil {
		log.Fatal(err)
	}
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
	if err := http.ListenAndServe(addr, routes.API); err != nil {
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
