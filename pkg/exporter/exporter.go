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
	ex.models = modelSet(tables)
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
	if err := ex.writeSQLMigrations(tables, views); err != nil {
		return nil, err
	}
	if err := ex.writeBindings(); err != nil {
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
	project      *generator.Project
	outDir       string
	modulePath   string
	sourceModule string
	result       *Result
	dryRun       bool
	models       map[string]bool
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

	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		switch {
		case p == e.sourceModule+"/app/http":
			imp.Path.Value = strconv.Quote(e.modulePath + "/internal/httpx")
			imp.Name = ast.NewIdent("httpx")
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
		return true
	})

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
		return parseExpr(fmt.Sprintf("func() (*models.%s, error) { var record models.%s; err := %s.First(&record).Error; return &record, err }()", q.Model, q.Model, q.gormChain()))
	case q.Terminal == "All":
		return parseExpr(fmt.Sprintf("func() ([]models.%s, error) { var records []models.%s; err := %s.Find(&records).Error; return records, err }()", q.Model, q.Model, q.gormChain()))
	case q.Terminal == "Count":
		return parseExpr(fmt.Sprintf("func() (int64, error) { var count int64; err := %s.Count(&count).Error; return count, err }()", q.gormChain()))
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
		return parseExpr(fmt.Sprintf("func() (*float64, error) { var value *float64; err := %s.Select(%q).Scan(&value).Error; return value, err }()", q.gormChain(), fn+"("+column+")"))
	case q.Terminal == "Create":
		arg, err := exprString(q.Arg)
		if err != nil {
			return nil, err
		}
		return parseExpr(fmt.Sprintf("models.DB.Create(%s).Error", arg))
	case q.Terminal == "Update":
		arg, err := exprString(q.Arg)
		if err != nil {
			return nil, err
		}
		return parseExpr(fmt.Sprintf("models.DB.Save(%s).Error", arg))
	case q.Terminal == "Delete":
		arg, err := exprString(q.Arg)
		if err != nil {
			return nil, err
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
	return parseExpr(q.gormChain())
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

func (q queryChain) gormChain() string {
	chain := fmt.Sprintf("models.DB.Model(&models.%s{})", q.Model)
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
	for _, table := range tables {
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
	return nil
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
			out = append(out, sqlMigration{Name: exportName, Up: createTableSQL(table) + ";\n", Down: dropTableSQL(table.Name) + ";\n"})
		case strings.HasPrefix(operation, "create_") && strings.HasSuffix(operation, "_view"):
			viewName := strings.TrimSuffix(strings.TrimPrefix(operation, "create_"), "_view")
			view, ok := viewsByName[viewName]
			if !ok {
				return nil, fmt.Errorf("migration %s references unknown view %s", entry.Name(), viewName)
			}
			out = append(out, sqlMigration{Name: exportName, Up: createViewSQL(view) + ";\n", Down: dropViewSQL(view.Name) + ";\n"})
		default:
			return nil, fmt.Errorf("unsupported migration export for %s", entry.Name())
		}
	}
	return out, nil
}

func migrationExportName(base string) string {
	parts := strings.SplitN(base, "_", 5)
	if len(parts) < 5 {
		return base
	}
	return parts[0] + parts[1] + parts[2] + parts[3] + "_" + parts[4]
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
	return e.writeFile(filepath.Join("app", "http", "auth", "jwt", "jwt.go"), []byte(jwtSupportSource))
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
	source := serverMainSource
	if scan != nil && scan.HasDatabaseConfig {
		source = serverMainWithDatabaseSource
		return e.writeFile(filepath.Join("cmd", "server", "main.go"), []byte(fmt.Sprintf(source, e.modulePath, e.modulePath, e.modulePath)))
	}
	return e.writeFile(filepath.Join("cmd", "server", "main.go"), []byte(fmt.Sprintf(source, e.modulePath, e.modulePath)))
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
		{"app/graphql", "generated_graphql", "generated GraphQL runtime is not lowered in v1"},
	}
	for _, c := range checks {
		if _, err := os.Stat(filepath.Join(e.project.Dir, c.path)); err == nil {
			e.result.Findings = append(e.result.Findings, Finding{File: c.path, Rule: c.rule, Message: c.msg})
		}
	}
}

func (e *exporter) addSchemaFindings(tables []*schema.Table) {
	for _, table := range tables {
		if table.IsImmutable || table.IsAppendOnly {
			e.result.Findings = append(e.result.Findings, Finding{File: "database/migrations", Rule: "integrity_tables", Message: "immutable/append-only hash-chain runtime behavior is not lowered in v1"})
			break
		}
	}
	for _, table := range tables {
		for _, col := range table.Columns {
			if col.IsEncrypted || col.IsSealed {
				e.result.Findings = append(e.result.Findings, Finding{File: "database/migrations", Rule: "encrypted_columns", Message: "encrypted/sealed column runtime behavior is not lowered in v1"})
				return
			}
		}
	}
}

func (e *exporter) writeReport(orm string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# Export Report\n\n")
	fmt.Fprintf(&b, "Source project: `%s`\n\n", e.project.Dir)
	fmt.Fprintf(&b, "Exported module: `%s`\n\n", e.modulePath)
	fmt.Fprintf(&b, "Target ORM: `%s`\n\n", orm)
	fmt.Fprintf(&b, "Generated at: `%s`\n\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "Files written: `%d`\n\n", e.result.FilesWritten)
	if len(e.result.Findings) == 0 {
		b.WriteString("No unsupported export findings.\n")
	} else {
		b.WriteString("## Findings\n\n")
		for _, f := range e.result.Findings {
			loc := f.File
			if f.Line > 0 {
				loc = fmt.Sprintf("%s:%d", f.File, f.Line)
			}
			fmt.Fprintf(&b, "- `%s` `%s` - %s\n", loc, f.Rule, f.Message)
		}
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

type modelTemplateData struct {
	Imports []string
	Models  []modelInfo
}

type modelInfo struct {
	Name         string
	Table        string
	Fields       []modelField
	PublicFields []modelField
}

type modelField struct {
	Name string
	Type string
	JSON string
	GORM string
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
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
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
	"errors"
	"net/http"
	"strings"

	"%s/app/http/auth/jwt"
	"%s/config"
	"%s/internal/httpx"
)

var jwtDriver = jwt.NewDriver(config.Env)

func Driver(name string) any {
	switch name {
	case "jwt":
		return jwtDriver
	default:
		return nil
	}
}

func Authenticate(r *http.Request) (*httpx.AuthInfo, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return nil, errors.New("missing authorization header")
	}
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return nil, errors.New("invalid authorization header")
	}
	claims, err := jwtDriver.ValidateToken(parts[1])
	if err != nil {
		return nil, err
	}
	return &httpx.AuthInfo{UserID: claims.Subject, Role: claims.Role}, nil
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
	"strings"
	"time"

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

func hmacSign(data, secret []byte, alg string) ([]byte, error) {
	var h func() hash.Hash
	switch alg {
	case "HS256": h = sha256.New
	case "HS384": h = sha512.New384
	case "HS512": h = sha512.New
	default: return nil, fmt.Errorf("jwt: unsupported algorithm %s", alg)
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
