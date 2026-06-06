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
		return nil, exportError{Rule: "orm_export_unsupported", Message: fmt.Sprintf("unsupported orm %q", opts.ORM)}
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
	ex.schemaTables = schemaTableSet(tables)
	ex.hasEncryptedColumns = tablesHaveEncryptedColumns(tables)
	ex.integrityModels = integrityModelSet(tables)
	ex.scopes, err = generator.ScanScopes(filepath.Join(project.Dir, "database", "scopes"))
	if err != nil {
		return nil, fmt.Errorf("scanning scopes: %w", err)
	}
	ex.managesRoles, err = exportedManagesRoles(project.Dir)
	if err != nil {
		return nil, fmt.Errorf("deriving manages roles: %w", err)
	}
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
	if err := ex.generateGraphQLExportTarget(); err != nil {
		return nil, err
	}
	if err := ex.tidyModule(); err != nil {
		return nil, err
	}
	ex.addGraphQLActionFindings()
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
	schemaTables        map[string]*schema.Table
	migrations          []generator.MigrationOps
	sqlMigrations       []sqlMigration
	hasEncryptedColumns bool
	integrityModels     map[string]integrityModelInfo
	scopes              map[string][]generator.ScopeDef
	managesRoles        map[string]bool
}

type integrityModelInfo struct {
	Table       *schema.Table
	Immutable   bool
	AppendOnly  bool
	SoftDeletes bool
}

func exportedManagesRoles(projectDir string) (map[string]bool, error) {
	policies, err := generator.ParsePolicyOps(filepath.Join(projectDir, "database", "policies"))
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, role := range generator.StaticDeriveRoles(policies) {
		if role.IsManages {
			out[role.Slug] = true
		}
	}
	return out, nil
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
	Rule    string
	Message string
}

func (e exportError) Error() string {
	if e.Rule != "" {
		if e.File == "" {
			return fmt.Sprintf("[%s] %s", e.Rule, e.Message)
		}
		if e.Line == 0 {
			return fmt.Sprintf("%s: [%s] %s", e.File, e.Rule, e.Message)
		}
		return fmt.Sprintf("%s:%d: [%s] %s", e.File, e.Line, e.Rule, e.Message)
	}
	return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Message)
}

func queryExportError(path string, line int, message string) exportError {
	return exportError{
		File:    path,
		Line:    line,
		Rule:    "query_export_unsupported",
		Message: message,
	}
}

func actionQueryExportError(file string, line int, message string) exportError {
	return exportError{
		File:    file,
		Line:    line,
		Rule:    "action_export_unsupported_query",
		Message: message,
	}
}

func migrationExportError(location, name string, err error) exportError {
	return exportError{
		File:    location,
		Rule:    "migration_export_unsupported",
		Message: fmt.Sprintf("unsupported migration export for %s: %s", name, err),
	}
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

func (e *exporter) generateGraphQLExportTarget() error {
	if e.dryRun || !e.hasGraphQLPackage() {
		return nil
	}
	download := exec.Command("go", "mod", "download", "github.com/99designs/gqlgen")
	download.Dir = e.outDir
	if out, err := download.CombinedOutput(); err != nil {
		return fmt.Errorf("downloading gqlgen in exported app: %w\n%s", err, out)
	}
	cmd := exec.Command("go", "run", "-mod=mod", "github.com/99designs/gqlgen", "generate", "--config", "gqlgen.yml")
	cmd.Dir = e.outDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("generating gqlgen target in exported app: %w\n%s", err, out)
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
		if p == e.sourceModule+"/app/models" && imp.Name != nil && imp.Name.Name != "." && imp.Name.Name != "_" {
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
		case p == e.sourceModule+"/app/models":
			imp.Path.Value = strconv.Quote(e.modulePath + "/app/models")
			if imp.Name != nil && imp.Name.Name == modelAlias {
				imp.Name = nil
			}
		case strings.HasPrefix(p, e.sourceModule+"/database/actions/"):
			imp.Path.Value = strconv.Quote(e.modulePath + "/app/models")
			if imp.Name != nil {
				actionImportAliases = append(actionImportAliases, imp.Name.Name)
			} else {
				actionImportAliases = append(actionImportAliases, filepath.Base(p))
			}
			imp.Name = nil
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
		if modelAlias != "models" {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == modelAlias {
				id.Name = "models"
			}
		}
		for _, alias := range actionImportAliases {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == alias {
				id.Name = "models"
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
	if fileUsesIdent(f, "clause") {
		addImport(f, "gorm.io/gorm/clause", "")
		dedupeImports(f)
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

func fileUsesIdent(f *ast.File, name string) bool {
	used := false
	ast.Inspect(f, func(n ast.Node) bool {
		if used {
			return false
		}
		id, ok := n.(*ast.Ident)
		if ok && id.Name == name {
			used = true
			return false
		}
		return true
	})
	return used
}

func addImport(f *ast.File, path, name string) {
	quoted := strconv.Quote(path)
	for _, imp := range f.Imports {
		if imp.Path.Value == quoted {
			if name != "" && imp.Name == nil {
				imp.Name = ast.NewIdent(name)
			}
			return
		}
	}
	spec := &ast.ImportSpec{Path: &ast.BasicLit{Kind: token.STRING, Value: quoted}}
	if name != "" {
		spec.Name = ast.NewIdent(name)
	}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			continue
		}
		gen.Specs = append(gen.Specs, spec)
		f.Imports = append(f.Imports, spec)
		return
	}
	gen := &ast.GenDecl{Tok: token.IMPORT, Specs: []ast.Spec{spec}}
	f.Decls = append([]ast.Decl{gen}, f.Decls...)
	f.Imports = append(f.Imports, spec)
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
		queryVars := map[string]queryVarState{}
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

func (e *exporter) rewriteStmt(path string, fset *token.FileSet, stmt ast.Stmt, queryVars map[string]queryVarState) (ast.Stmt, error) {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		if len(s.Rhs) == 1 {
			call, ok := s.Rhs[0].(*ast.CallExpr)
			if ok {
				if assign, ok, err := e.rewriteQueryVarMutation(call, queryVars); err != nil {
					return nil, queryExportError(path, fset.Position(call.Pos()).Line, err.Error())
				} else if ok {
					if len(s.Lhs) != 1 {
						return nil, queryExportError(path, fset.Position(call.Pos()).Line, "query builder assignment must target one variable")
					}
					lhs, lhsOK := s.Lhs[0].(*ast.Ident)
					if !lhsOK {
						return nil, queryExportError(path, fset.Position(call.Pos()).Line, "query builder assignment must target a variable")
					}
					if rhs, rhsOK := assign.(*ast.AssignStmt); rhsOK && len(rhs.Lhs) == 1 {
						rhsID, rhsIDOK := rhs.Lhs[0].(*ast.Ident)
						if !rhsIDOK || rhsID.Name != lhs.Name {
							return nil, queryExportError(path, fset.Position(call.Pos()).Line, "query builder assignment must target the same variable")
						}
						return assign, nil
					}
					if _, empty := assign.(*ast.EmptyStmt); empty {
						return assign, nil
					}
				}

				if terminal, ok, err := parseQueryVarTerminal(call, queryVars); err != nil {
					return nil, queryExportError(path, fset.Position(call.Pos()).Line, err.Error())
				} else if ok {
					expr, err := e.gormVarTerminalExpr(terminal)
					if err != nil {
						return nil, queryExportError(path, fset.Position(call.Pos()).Line, err.Error())
					}
					s.Rhs[0] = expr
					return stmt, nil
				}

				if len(s.Lhs) == 1 {
					if e.preserveQueryCall(call) {
						return stmt, nil
					}
					chain, chainOK, err := parseQueryBuilderChain(call)
					if err != nil {
						return nil, queryExportError(path, fset.Position(call.Pos()).Line, err.Error())
					}
					if chainOK && chain.Terminal == "" {
						ident, identOK := s.Lhs[0].(*ast.Ident)
						if identOK {
							chain.DeferLatest = true
							expr, err := e.gormBuilderExpr(chain)
							if err != nil {
								return nil, queryExportError(path, fset.Position(call.Pos()).Line, err.Error())
							}
							s.Rhs[0] = expr
							queryVars[ident.Name] = queryVarState{
								Model:         chain.Model,
								DBRoot:        chain.dbRoot(),
								AllVersions:   chain.AllVersions,
								VersionFilter: chain.hasVersionFilter(),
								LockStrength:  chain.LockStrength,
								LockOptions:   chain.LockOptions,
								LockTimeout:   chain.LockTimeout,
							}
							return stmt, nil
						}
					}
				}

				if e.preserveQueryCall(call) {
					return stmt, nil
				}
				chain, ok, err := parseQueryChain(call)
				if err != nil {
					return nil, queryExportError(path, fset.Position(call.Pos()).Line, err.Error())
				}
				if ok {
					expr, err := e.gormExpr(chain)
					if err != nil {
						return nil, queryExportError(path, fset.Position(call.Pos()).Line, err.Error())
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
		assign, ok, err := e.rewriteQueryVarMutation(call, queryVars)
		if err != nil {
			return nil, queryExportError(path, fset.Position(call.Pos()).Line, err.Error())
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
			if e.preserveQueryCall(call) {
				continue
			}
			chain, chainOK, err := parseQueryChain(call)
			if err != nil {
				return nil, queryExportError(path, fset.Position(call.Pos()).Line, err.Error())
			}
			if chainOK {
				expr, err := e.gormExpr(chain)
				if err != nil {
					return nil, queryExportError(path, fset.Position(call.Pos()).Line, err.Error())
				}
				s.Results[i] = expr
				continue
			}
			terminal, ok, err := parseQueryVarTerminal(call, queryVars)
			if err != nil {
				return nil, queryExportError(path, fset.Position(call.Pos()).Line, err.Error())
			}
			if !ok {
				continue
			}
			expr, err := e.gormVarTerminalExpr(terminal)
			if err != nil {
				return nil, queryExportError(path, fset.Position(call.Pos()).Line, err.Error())
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
			if e.preserveQueryRootCall(call) {
				return true
			}
			pos := fset.Position(call.Pos())
			firstErr = queryExportError(path, pos.Line, "unsupported Pickle query chain; exporter requires clean GORM lowering")
			return false
		}
		return true
	})
	return firstErr
}

func (e *exporter) preserveQueryCall(call *ast.CallExpr) bool {
	model, ok := queryCallModel(call)
	return ok && e.modelHasCustomScopes(model)
}

func (e *exporter) preserveQueryRootCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	model := strings.TrimPrefix(sel.Sel.Name, "Query")
	return e.modelHasCustomScopes(model)
}

func (e *exporter) modelHasCustomScopes(model string) bool {
	if len(e.scopes) == 0 {
		return false
	}
	modelDir := strings.TrimSuffix(strings.ToLower(pascalToSnake(model)), "s")
	return len(e.scopes[modelDir]) > 0
}

func queryCallModel(call *ast.CallExpr) (string, bool) {
	cur := ast.Expr(call)
	for {
		c, ok := cur.(*ast.CallExpr)
		if !ok {
			return "", false
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			return "", false
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == "models" && strings.HasPrefix(sel.Sel.Name, "Query") {
			return strings.TrimPrefix(sel.Sel.Name, "Query"), true
		}
		if id, ok := sel.X.(*ast.Ident); ok && strings.HasPrefix(sel.Sel.Name, "Query") {
			if _, ok := queryRootDB(id.Name); ok {
				return strings.TrimPrefix(sel.Sel.Name, "Query"), true
			}
		}
		if rootCall, ok := sel.X.(*ast.CallExpr); ok {
			cur = rootCall
			continue
		}
		return "", false
	}
}

type queryChain struct {
	Model        string
	DBRoot       string
	Terminal     string
	Filters      []queryFilter
	Orders       []queryOrder
	Preloads     []string
	Selects      []string
	RoleSelect   *queryRoleSelect
	RoleScope    string
	Limit        ast.Expr
	Offset       ast.Expr
	Arg          ast.Expr
	AllVersions  bool
	DeferLatest  bool
	LockStrength string
	LockOptions  string
	LockTimeout  ast.Expr
}

type queryFilter struct {
	Column string
	Op     string
	Arg    ast.Expr
	Arg2   ast.Expr
}

type queryOrder struct {
	Column    string
	Direction string
}

type queryRoleSelect struct {
	Arg          ast.Expr
	SingleRole   bool
	IncludeOwner bool
}

type queryVarState struct {
	Model         string
	DBRoot        string
	AllVersions   bool
	VersionFilter bool
	LockStrength  string
	LockOptions   string
	LockTimeout   ast.Expr
}

func (q queryChain) dbRoot() string {
	if q.DBRoot == "" {
		return "models.DB"
	}
	return q.DBRoot
}

func (q queryChain) inTransaction() bool {
	return q.dbRoot() != "models.DB"
}

func (q queryChain) hasLock() bool {
	return q.LockStrength != "" || q.LockOptions != ""
}

func (q queryChain) lockRequiresTransaction() bool {
	return q.hasLock() && (q.Terminal == "First" || q.Terminal == "All")
}

func (q queryVarState) dbRoot() string {
	if q.DBRoot == "" {
		return "models.DB"
	}
	return q.DBRoot
}

func (q queryVarTerminal) inTransaction() bool {
	return q.DBRoot != "" && q.DBRoot != "models.DB"
}

func (q queryVarTerminal) hasLock() bool {
	return q.LockStrength != "" || q.LockOptions != ""
}

func (q queryVarTerminal) lockRequiresTransaction() bool {
	return q.hasLock() && (q.Terminal == "First" || q.Terminal == "All")
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
		if id, ok := sel.X.(*ast.Ident); ok && strings.HasPrefix(sel.Sel.Name, "Query") {
			if dbRoot, ok := queryRootDB(id.Name); ok {
				return buildQueryChain(strings.TrimPrefix(sel.Sel.Name, "Query"), dbRoot, methods, requireTerminal)
			}
		}
		methods = append(methods, struct {
			name string
			args []ast.Expr
		}{name: sel.Sel.Name, args: c.Args})
		if root, ok := sel.X.(*ast.SelectorExpr); ok {
			if id, ok := root.X.(*ast.Ident); ok && strings.HasPrefix(root.Sel.Name, "Query") {
				if dbRoot, ok := queryRootDB(id.Name); ok {
					return buildQueryChain(strings.TrimPrefix(root.Sel.Name, "Query"), dbRoot, methods, requireTerminal)
				}
			}
		}
		if rootCall, ok := sel.X.(*ast.CallExpr); ok {
			if root, ok := rootCall.Fun.(*ast.SelectorExpr); ok {
				if id, ok := root.X.(*ast.Ident); ok && strings.HasPrefix(root.Sel.Name, "Query") {
					if dbRoot, ok := queryRootDB(id.Name); ok {
						return buildQueryChain(strings.TrimPrefix(root.Sel.Name, "Query"), dbRoot, methods, requireTerminal)
					}
				}
			}
		}
		cur = sel.X
	}
}

func queryRootDB(root string) (string, bool) {
	if root == "models" {
		return "models.DB", true
	}
	lower := strings.ToLower(root)
	if lower == "tx" || lower == "txn" || lower == "transaction" || strings.HasSuffix(root, "Tx") || strings.HasSuffix(root, "TX") {
		return root + ".DB", true
	}
	return "", false
}

func buildQueryChain(model, dbRoot string, methods []struct {
	name string
	args []ast.Expr
}, requireTerminal bool) (queryChain, bool, error) {
	qc := queryChain{Model: model, DBRoot: dbRoot}
	for i := len(methods) - 1; i >= 0; i-- {
		m := methods[i]
		switch {
		case m.name == "AnyOwner":
		case m.name == "AllVersions":
			if len(m.args) != 0 {
				return qc, true, fmt.Errorf("AllVersions does not accept arguments")
			}
			qc.AllVersions = true
		case m.name == "Lock" || m.name == "LockForUpdate":
			if len(m.args) != 0 {
				return qc, true, fmt.Errorf("%s does not accept arguments", m.name)
			}
			qc.LockStrength = "UPDATE"
		case m.name == "LockForShare":
			if len(m.args) != 0 {
				return qc, true, fmt.Errorf("LockForShare does not accept arguments")
			}
			qc.LockStrength = "SHARE"
		case m.name == "SkipLocked":
			if len(m.args) != 0 {
				return qc, true, fmt.Errorf("SkipLocked does not accept arguments")
			}
			qc.LockOptions = "SKIP LOCKED"
		case m.name == "NoWait":
			if len(m.args) != 0 {
				return qc, true, fmt.Errorf("NoWait does not accept arguments")
			}
			qc.LockOptions = "NOWAIT"
		case m.name == "Timeout":
			if len(m.args) != 1 {
				return qc, true, fmt.Errorf("Timeout requires one argument")
			}
			qc.LockTimeout = m.args[0]
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
			qc.Selects = []string{"*"}
		case m.name == "SelectPublic":
			if len(m.args) != 0 {
				return qc, true, fmt.Errorf("SelectPublic does not accept arguments")
			}
			qc.Selects = []string{"__visibility_public__"}
		case m.name == "SelectOwner":
			if len(m.args) != 0 {
				return qc, true, fmt.Errorf("SelectOwner does not accept arguments")
			}
			qc.Selects = []string{"__visibility_owner__"}
		case m.name == "SelectFor":
			if len(m.args) != 1 {
				return qc, true, fmt.Errorf("SelectFor requires one argument")
			}
			qc.RoleSelect = &queryRoleSelect{Arg: m.args[0], SingleRole: true}
		case m.name == "SelectForRoles":
			if len(m.args) != 1 {
				return qc, true, fmt.Errorf("SelectForRoles requires one argument")
			}
			qc.RoleSelect = &queryRoleSelect{Arg: m.args[0]}
		case m.name == "SelectForOwner":
			if len(m.args) != 1 {
				return qc, true, fmt.Errorf("SelectForOwner requires one argument")
			}
			qc.RoleSelect = &queryRoleSelect{Arg: m.args[0], IncludeOwner: true}
		case strings.HasPrefix(m.name, "OrderBy") && m.name != "OrderBy":
			if len(m.args) != 1 {
				return qc, true, fmt.Errorf("%s requires one argument", m.name)
			}
			direction, err := exprString(m.args[0])
			if err != nil {
				return qc, true, err
			}
			qc.Orders = append(qc.Orders, queryOrder{
				Column:    fmt.Sprintf("%q", pascalToSnake(strings.TrimPrefix(m.name, "OrderBy"))),
				Direction: direction,
			})
		case m.name == "OrderBy":
			if len(m.args) != 2 {
				return qc, true, fmt.Errorf("OrderBy requires two arguments")
			}
			column, err := exprString(m.args[0])
			if err != nil {
				return qc, true, err
			}
			direction, err := exprString(m.args[1])
			if err != nil {
				return qc, true, err
			}
			qc.Orders = append(qc.Orders, queryOrder{Column: column, Direction: direction})
		case strings.HasPrefix(m.name, "Where"):
			col, op, ok := whereMethodColumn(m.name, model)
			if !ok {
				return qc, true, fmt.Errorf("unsupported query method %s", m.name)
			}
			if op == "BETWEEN" {
				if len(m.args) != 2 {
					return qc, true, fmt.Errorf("%s requires two arguments", m.name)
				}
				qc.Filters = append(qc.Filters, queryFilter{Column: col, Op: op, Arg: m.args[0], Arg2: m.args[1]})
			} else {
				if len(m.args) != 1 {
					return qc, true, fmt.Errorf("%s requires one argument", m.name)
				}
				qc.Filters = append(qc.Filters, queryFilter{Column: col, Op: op, Arg: m.args[0]})
			}
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
		case m.name == "VerifyChain":
			if len(m.args) != 0 {
				return qc, true, fmt.Errorf("VerifyChain does not accept arguments")
			}
			qc.Terminal = m.name
		case m.name == "VerifyRow":
			if len(m.args) != 1 {
				return qc, true, fmt.Errorf("VerifyRow requires one argument")
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
	Var           string
	Model         string
	DBRoot        string
	Terminal      string
	AllVersions   bool
	VersionFilter bool
	LockStrength  string
	LockOptions   string
	LockTimeout   ast.Expr
	Arg           ast.Expr
}

func parseQueryVarTerminal(call *ast.CallExpr, queryVars map[string]queryVarState) (queryVarTerminal, bool, error) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return queryVarTerminal{}, false, nil
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return queryVarTerminal{}, false, nil
	}
	state, ok := queryVars[id.Name]
	if !ok {
		return queryVarTerminal{}, false, nil
	}
	if sel.Sel.Name != "First" && sel.Sel.Name != "All" && sel.Sel.Name != "Count" && sel.Sel.Name != "VerifyChain" && sel.Sel.Name != "VerifyRow" && !strings.HasPrefix(sel.Sel.Name, "Sum") && !strings.HasPrefix(sel.Sel.Name, "Avg") {
		return queryVarTerminal{}, false, nil
	}
	var arg ast.Expr
	if sel.Sel.Name == "VerifyRow" {
		if len(call.Args) != 1 {
			return queryVarTerminal{}, true, fmt.Errorf("VerifyRow requires one argument")
		}
		arg = call.Args[0]
	} else if len(call.Args) != 0 {
		return queryVarTerminal{}, true, fmt.Errorf("%s does not accept arguments", sel.Sel.Name)
	}
	return queryVarTerminal{
		Var:           id.Name,
		Model:         state.Model,
		DBRoot:        state.dbRoot(),
		Terminal:      sel.Sel.Name,
		AllVersions:   state.AllVersions,
		VersionFilter: state.VersionFilter,
		LockStrength:  state.LockStrength,
		LockOptions:   state.LockOptions,
		LockTimeout:   state.LockTimeout,
		Arg:           arg,
	}, true, nil
}

func (e *exporter) rewriteQueryVarMutation(call *ast.CallExpr, queryVars map[string]queryVarState) (ast.Stmt, bool, error) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false, nil
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return nil, false, nil
	}
	state, ok := queryVars[id.Name]
	if !ok {
		return nil, false, nil
	}
	model := state.Model

	var expr ast.Expr
	var err error
	switch {
	case strings.HasPrefix(sel.Sel.Name, "Where"):
		col, op, ok := whereMethodColumn(sel.Sel.Name, model)
		if !ok {
			return nil, true, fmt.Errorf("unsupported query method %s", sel.Sel.Name)
		}
		if op == "BETWEEN" {
			if len(call.Args) != 2 {
				return nil, true, fmt.Errorf("%s requires two arguments", sel.Sel.Name)
			}
			expr, err = e.gormVarWhereExpr(state.Model, id.Name, col, op, call.Args[0], call.Args[1])
		} else {
			if len(call.Args) != 1 {
				return nil, true, fmt.Errorf("%s requires one argument", sel.Sel.Name)
			}
			expr, err = e.gormVarWhereExpr(state.Model, id.Name, col, op, call.Args[0])
		}
		if col == "version_id" {
			state.VersionFilter = true
			queryVars[id.Name] = state
		}
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
		if resolved := e.resolveQueryColumn(state.Model, column, "ORDER"); resolved.Column != "" {
			column = resolved.Column
		}
		expr, err = parseExpr(fmt.Sprintf("%s.Order(models.OrderClause(%q, %s))", id.Name, column, arg))
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
		expr, err = parseExpr(fmt.Sprintf("%s.Order(models.OrderClause(%s, %s))", id.Name, col, dir))
	case sel.Sel.Name == "AllVersions":
		if len(call.Args) != 0 {
			return nil, true, fmt.Errorf("AllVersions does not accept arguments")
		}
		state.AllVersions = true
		queryVars[id.Name] = state
		return &ast.EmptyStmt{}, true, nil
	case sel.Sel.Name == "Lock" || sel.Sel.Name == "LockForUpdate":
		if len(call.Args) != 0 {
			return nil, true, fmt.Errorf("%s does not accept arguments", sel.Sel.Name)
		}
		state.LockStrength = "UPDATE"
		queryVars[id.Name] = state
		expr, err = gormVarLockExpr(id.Name, state.LockStrength, state.LockOptions)
	case sel.Sel.Name == "LockForShare":
		if len(call.Args) != 0 {
			return nil, true, fmt.Errorf("LockForShare does not accept arguments")
		}
		state.LockStrength = "SHARE"
		queryVars[id.Name] = state
		expr, err = gormVarLockExpr(id.Name, state.LockStrength, state.LockOptions)
	case sel.Sel.Name == "SkipLocked":
		if len(call.Args) != 0 {
			return nil, true, fmt.Errorf("SkipLocked does not accept arguments")
		}
		state.LockOptions = "SKIP LOCKED"
		queryVars[id.Name] = state
		expr, err = gormVarLockExpr(id.Name, state.LockStrength, state.LockOptions)
	case sel.Sel.Name == "NoWait":
		if len(call.Args) != 0 {
			return nil, true, fmt.Errorf("NoWait does not accept arguments")
		}
		state.LockOptions = "NOWAIT"
		queryVars[id.Name] = state
		expr, err = gormVarLockExpr(id.Name, state.LockStrength, state.LockOptions)
	case sel.Sel.Name == "Timeout":
		if len(call.Args) != 1 {
			return nil, true, fmt.Errorf("Timeout requires one argument")
		}
		state.LockTimeout = call.Args[0]
		queryVars[id.Name] = state
		return &ast.EmptyStmt{}, true, nil
	case sel.Sel.Name == "SelectAll" || sel.Sel.Name == "AnyOwner":
		if len(call.Args) != 0 {
			return nil, true, fmt.Errorf("%s does not accept arguments", sel.Sel.Name)
		}
		if sel.Sel.Name == "SelectAll" {
			expr, err = parseExpr(fmt.Sprintf("%s.Select(%q)", id.Name, "*"))
			break
		}
		return &ast.EmptyStmt{}, true, nil
	case sel.Sel.Name == "SelectPublic" || sel.Sel.Name == "SelectOwner":
		if len(call.Args) != 0 {
			return nil, true, fmt.Errorf("%s does not accept arguments", sel.Sel.Name)
		}
		mode := strings.TrimPrefix(sel.Sel.Name, "Select")
		cols, colErr := e.queryVisibilitySelectColumns(state.Model, mode)
		if colErr != nil {
			return nil, true, colErr
		}
		expr, err = parseExpr(fmt.Sprintf("%s.Select([]string{%s})", id.Name, quotedStringList(cols)))
	case sel.Sel.Name == "SelectFor" || sel.Sel.Name == "SelectForRoles" || sel.Sel.Name == "SelectForOwner":
		if len(call.Args) != 1 {
			return nil, true, fmt.Errorf("%s requires one argument", sel.Sel.Name)
		}
		roleSelect := queryRoleSelect{
			Arg:          call.Args[0],
			SingleRole:   sel.Sel.Name == "SelectFor",
			IncludeOwner: sel.Sel.Name == "SelectForOwner",
		}
		scope, scopeErr := e.roleVisibilityScopeExpr(state.Model, roleSelect)
		if scopeErr != nil {
			return nil, true, scopeErr
		}
		expr, err = parseExpr(fmt.Sprintf("%s.Scopes(%s)", id.Name, scope))
	case sel.Sel.Name == "First" || sel.Sel.Name == "All" || sel.Sel.Name == "Count" || sel.Sel.Name == "VerifyChain" || sel.Sel.Name == "VerifyRow" || strings.HasPrefix(sel.Sel.Name, "Sum") || strings.HasPrefix(sel.Sel.Name, "Avg"):
		return nil, false, nil
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
	if err := e.resolveQueryChainSelects(&q); err != nil {
		return nil, err
	}
	if q.RoleSelect != nil {
		scope, err := e.roleVisibilityScopeExpr(q.Model, *q.RoleSelect)
		if err != nil {
			return nil, err
		}
		q.RoleScope = scope
	}
	if q.lockRequiresTransaction() && !q.inTransaction() {
		return e.lockOutsideTransactionExpr(q.Model, q.Terminal)
	}
	switch {
	case q.Terminal == "First":
		return parseExpr(fmt.Sprintf("func() (*models.%s, error) { %s var record models.%s; err := %s.First(&record).Error; return &record, err }()", q.Model, e.lockTimeoutStmt(q), q.Model, e.gormChain(q)))
	case q.Terminal == "All":
		return parseExpr(fmt.Sprintf("func() ([]models.%s, error) { %s var records []models.%s; err := %s.Find(&records).Error; return records, err }()", q.Model, e.lockTimeoutStmt(q), q.Model, e.gormChain(q)))
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
		return parseExpr(fmt.Sprintf("%s.Create(%s).Error", q.dbRoot(), arg))
	case q.Terminal == "Update":
		arg, err := exprString(q.Arg)
		if err != nil {
			return nil, err
		}
		if _, ok := e.integrityModels[q.Model]; ok {
			return parseExpr(fmt.Sprintf("models.Update%s(%s)", q.Model, arg))
		}
		return parseExpr(fmt.Sprintf("%s.Save(%s).Error", q.dbRoot(), arg))
	case q.Terminal == "Delete":
		arg, err := exprString(q.Arg)
		if err != nil {
			return nil, err
		}
		if _, ok := e.integrityModels[q.Model]; ok {
			return parseExpr(fmt.Sprintf("models.Delete%s(%s)", q.Model, arg))
		}
		return parseExpr(fmt.Sprintf("%s.Delete(%s).Error", q.dbRoot(), arg))
	case q.Terminal == "VerifyChain":
		if _, ok := e.integrityModels[q.Model]; !ok {
			return nil, fmt.Errorf("VerifyChain is only supported for integrity models")
		}
		return parseExpr(fmt.Sprintf("models.Verify%sChain()", q.Model))
	case q.Terminal == "VerifyRow":
		if _, ok := e.integrityModels[q.Model]; !ok {
			return nil, fmt.Errorf("VerifyRow is only supported for integrity models")
		}
		arg, err := exprString(q.Arg)
		if err != nil {
			return nil, err
		}
		return parseExpr(fmt.Sprintf("models.Verify%sRow(%s)", q.Model, arg))
	default:
		return nil, fmt.Errorf("unsupported terminal query method %s", q.Terminal)
	}
}

func (e *exporter) gormBuilderExpr(q queryChain) (ast.Expr, error) {
	if !e.models[q.Model] {
		return nil, fmt.Errorf("unknown exported model %s", q.Model)
	}
	if err := e.resolveQueryChainSelects(&q); err != nil {
		return nil, err
	}
	if q.RoleSelect != nil {
		scope, err := e.roleVisibilityScopeExpr(q.Model, *q.RoleSelect)
		if err != nil {
			return nil, err
		}
		q.RoleScope = scope
	}
	return parseExpr(e.gormChain(q))
}

func (e *exporter) gormVarTerminalExpr(q queryVarTerminal) (ast.Expr, error) {
	if !e.models[q.Model] {
		return nil, fmt.Errorf("unknown exported model %s", q.Model)
	}
	if q.lockRequiresTransaction() && !q.inTransaction() {
		return e.lockOutsideTransactionExpr(q.Model, q.Terminal)
	}
	query := e.gormVarTerminalRoot(q)
	switch {
	case q.Terminal == "First":
		return parseExpr(fmt.Sprintf("func() (*models.%s, error) { %s var record models.%s; err := %s.First(&record).Error; return &record, err }()", q.Model, e.lockTimeoutStmt(q), q.Model, query))
	case q.Terminal == "All":
		return parseExpr(fmt.Sprintf("func() ([]models.%s, error) { %s var records []models.%s; err := %s.Find(&records).Error; return records, err }()", q.Model, e.lockTimeoutStmt(q), q.Model, query))
	case q.Terminal == "Count":
		return parseExpr(fmt.Sprintf("func() (int64, error) { var count int64; err := %s.Count(&count).Error; return count, err }()", query))
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
		return parseExpr(fmt.Sprintf("func() (*float64, error) { var value *float64; err := %s.Select(%q).Scan(&value).Error; return value, err }()", query, fn+"("+column+")"))
	case q.Terminal == "VerifyChain":
		if _, ok := e.integrityModels[q.Model]; !ok {
			return nil, fmt.Errorf("VerifyChain is only supported for integrity models")
		}
		return parseExpr(fmt.Sprintf("func() error { _ = %s; return models.Verify%sChain() }()", q.Var, q.Model))
	case q.Terminal == "VerifyRow":
		if _, ok := e.integrityModels[q.Model]; !ok {
			return nil, fmt.Errorf("VerifyRow is only supported for integrity models")
		}
		arg, err := exprString(q.Arg)
		if err != nil {
			return nil, err
		}
		return parseExpr(fmt.Sprintf("func() error { _ = %s; return models.Verify%sRow(%s) }()", q.Var, q.Model, arg))
	default:
		return nil, fmt.Errorf("unsupported terminal query method %s", q.Terminal)
	}
}

func (e *exporter) gormVarTerminalRoot(q queryVarTerminal) string {
	if info, ok := e.integrityModels[q.Model]; ok && info.Immutable && !q.AllVersions && !q.VersionFilter {
		return fmt.Sprintf("%s.Where(%q)", q.Var, latestVersionPredicate(info.Table.Name))
	}
	return q.Var
}

func (e *exporter) lockOutsideTransactionExpr(model, terminal string) (ast.Expr, error) {
	errExpr := fmt.Sprintf("models.NewLockOutsideTransactionError(%q)", model)
	switch {
	case terminal == "First":
		return parseExpr(fmt.Sprintf("func() (*models.%s, error) { return nil, %s }()", model, errExpr))
	case terminal == "All":
		return parseExpr(fmt.Sprintf("func() ([]models.%s, error) { return nil, %s }()", model, errExpr))
	case terminal == "Count":
		return parseExpr(fmt.Sprintf("func() (int64, error) { return 0, %s }()", errExpr))
	case strings.HasPrefix(terminal, "Sum") || strings.HasPrefix(terminal, "Avg"):
		return parseExpr(fmt.Sprintf("func() (*float64, error) { return nil, %s }()", errExpr))
	case terminal == "Create" || terminal == "Update" || terminal == "Delete":
		return parseExpr(errExpr)
	default:
		return nil, fmt.Errorf("unsupported terminal query method %s", terminal)
	}
}

func (e *exporter) lockTimeoutStmt(q any) string {
	var dbRoot string
	var timeout ast.Expr
	var terminal string
	switch v := q.(type) {
	case queryChain:
		dbRoot = v.dbRoot()
		timeout = v.LockTimeout
		terminal = v.Terminal
	case queryVarTerminal:
		dbRoot = v.DBRoot
		timeout = v.LockTimeout
		terminal = v.Terminal
	}
	if timeout == nil || dbRoot == "" || dbRoot == "models.DB" || (terminal != "First" && terminal != "All") {
		return ""
	}
	arg, err := exprString(timeout)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("if err := models.ApplyLockTimeout(%s, %s); err != nil { return nil, err };", dbRoot, arg)
}

type exportedQueryColumn struct {
	Column      string
	Encrypted   bool
	Unsupported string
}

func (e *exporter) resolveQueryColumn(model, col, op string) exportedQueryColumn {
	if col == "__owner__" {
		return exportedQueryColumn{Column: "user_id"}
	}
	table := e.schemaTables[model]
	if table == nil {
		return exportedQueryColumn{Column: col}
	}
	for _, column := range table.Columns {
		if column.Name != col {
			continue
		}
		if column.IsSealed {
			return exportedQueryColumn{Column: col + "_encrypted", Unsupported: fmt.Sprintf("sealed column %s cannot be filtered", col)}
		}
		if column.IsEncrypted {
			if op != "=" && op != "IN" && op != "<>" && op != "NOT IN" {
				return exportedQueryColumn{Column: col + "_encrypted", Unsupported: fmt.Sprintf("encrypted column %s does not support %s filters", col, op)}
			}
			return exportedQueryColumn{Column: col + "_encrypted", Encrypted: true}
		}
		return exportedQueryColumn{Column: col}
	}
	return exportedQueryColumn{Column: col}
}

func (e *exporter) resolveQueryOrderColumn(model, expr string) string {
	unquoted, err := strconv.Unquote(expr)
	if err != nil {
		return expr
	}
	resolved := e.resolveQueryColumn(model, unquoted, "ORDER")
	if resolved.Column == "" {
		return expr
	}
	return fmt.Sprintf("%q", resolved.Column)
}

func (e *exporter) gormVarWhereExpr(model, varName, col, op string, args ...ast.Expr) (ast.Expr, error) {
	if col == "__owner__" {
		col = "user_id"
	}
	resolved := e.resolveQueryColumn(model, col, op)
	if resolved.Unsupported != "" {
		return parseExpr(fmt.Sprintf("%s.Scopes(models.UnsupportedQueryFilterScope(%q))", varName, resolved.Unsupported))
	}
	if resolved.Encrypted {
		if len(args) != 1 {
			return nil, fmt.Errorf("%s requires one argument", op)
		}
		argStr, err := exprString(args[0])
		if err != nil {
			return nil, err
		}
		return parseExpr(fmt.Sprintf("%s.Scopes(models.EncryptedWhereScope(%q, %q, %s))", varName, resolved.Column, op, argStr))
	}
	col = resolved.Column
	if op == "BETWEEN" {
		if len(args) != 2 {
			return nil, fmt.Errorf("BETWEEN requires two arguments")
		}
		start, err := exprString(args[0])
		if err != nil {
			return nil, err
		}
		end, err := exprString(args[1])
		if err != nil {
			return nil, err
		}
		return parseExpr(fmt.Sprintf("%s.Where(%q, %s, %s)", varName, col+" BETWEEN ? AND ?", start, end))
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("%s requires one argument", op)
	}
	argStr, err := exprString(args[0])
	if err != nil {
		return nil, err
	}
	return parseExpr(fmt.Sprintf("%s.Where(%q, %s)", varName, col+" "+op+" ?", argStr))
}

func (e *exporter) queryVisibilitySelectColumns(model, mode string) ([]string, error) {
	if mode == "All" {
		return []string{"*"}, nil
	}
	table := e.schemaTables[model]
	if table == nil {
		return nil, fmt.Errorf("%s visibility selection requires schema metadata", model)
	}
	var cols []string
	for _, col := range table.Columns {
		switch mode {
		case "Public":
			if !col.IsPublic {
				continue
			}
		case "Owner":
			if !col.IsPublic && !col.IsOwnerSees {
				continue
			}
		default:
			return nil, fmt.Errorf("unsupported visibility selector %s", mode)
		}
		cols = append(cols, graphQLSelectColumnName(col))
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("%s has no %s visibility columns", model, strings.ToLower(mode))
	}
	return cols, nil
}

func (e *exporter) resolveQueryChainSelects(q *queryChain) error {
	if q == nil || len(q.Selects) != 1 || !strings.HasPrefix(q.Selects[0], "__visibility_") {
		return nil
	}
	mode := "Public"
	if q.Selects[0] == "__visibility_owner__" {
		mode = "Owner"
	}
	cols, err := e.queryVisibilitySelectColumns(q.Model, mode)
	if err != nil {
		return err
	}
	q.Selects = cols
	return nil
}

func (e *exporter) roleVisibilityScopeExpr(model string, roleSelect queryRoleSelect) (string, error) {
	table := e.schemaTables[model]
	if table == nil {
		return "", fmt.Errorf("%s role visibility selection requires schema metadata", model)
	}
	publicCols, ownerCols, roleCols := e.queryRoleVisibilityColumns(table)
	rolesExpr, err := exprString(roleSelect.Arg)
	if err != nil {
		return "", err
	}
	if roleSelect.SingleRole {
		rolesExpr = "[]string{" + rolesExpr + "}"
	}
	return fmt.Sprintf("models.RoleVisibilitySelectScope([]string{%s}, map[string][]string{%s}, []string{%s}, []string{%s}, %s, %t)",
		quotedStringList(publicCols),
		quotedRoleColumnMap(roleCols),
		quotedStringList(ownerCols),
		quotedStringList(e.sortedManagesRoles()),
		rolesExpr,
		roleSelect.IncludeOwner,
	), nil
}

func (e *exporter) queryRoleVisibilityColumns(table *schema.Table) ([]string, []string, map[string][]string) {
	var publicCols []string
	var ownerCols []string
	roleCols := map[string][]string{}
	for _, col := range table.Columns {
		storage := graphQLSelectColumnName(col)
		if col.IsPublic {
			publicCols = append(publicCols, storage)
			ownerCols = append(ownerCols, storage)
		} else if col.IsOwnerSees {
			ownerCols = append(ownerCols, storage)
		}
		for role := range col.VisibleTo {
			roleCols[role] = append(roleCols[role], storage)
		}
	}
	return publicCols, ownerCols, roleCols
}

func (e *exporter) sortedManagesRoles() []string {
	var roles []string
	for role := range e.managesRoles {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	return roles
}

func gormVarLockExpr(varName, strength, options string) (ast.Expr, error) {
	if strength == "" {
		strength = "UPDATE"
	}
	expr := fmt.Sprintf("%s.Clauses(clause.Locking{Strength: %q", varName, strength)
	if options != "" {
		expr += fmt.Sprintf(", Options: %q", options)
	}
	expr += "})"
	return parseExpr(expr)
}

func (e *exporter) gormChain(q queryChain) string {
	chain := fmt.Sprintf("%s.Model(&models.%s{})", q.dbRoot(), q.Model)
	if info, ok := e.integrityModels[q.Model]; ok && info.Immutable && !q.DeferLatest && !q.AllVersions && !q.hasVersionFilter() {
		chain += fmt.Sprintf(".Where(%q)", latestVersionPredicate(info.Table.Name))
	}
	if len(q.Selects) > 0 {
		if len(q.Selects) == 1 && q.Selects[0] == "*" {
			chain += ".Select(\"*\")"
		} else {
			chain += fmt.Sprintf(".Select([]string{%s})", quotedStringList(q.Selects))
		}
	}
	if q.RoleScope != "" {
		chain += fmt.Sprintf(".Scopes(%s)", q.RoleScope)
	}
	for _, f := range q.Filters {
		col := f.Column
		if col == "__owner__" {
			col = q.ownerColumn()
		}
		resolved := e.resolveQueryColumn(q.Model, col, f.Op)
		if resolved.Unsupported != "" {
			chain += fmt.Sprintf(".Scopes(models.UnsupportedQueryFilterScope(%q))", resolved.Unsupported)
			continue
		}
		arg, _ := exprString(f.Arg)
		if resolved.Encrypted {
			chain += fmt.Sprintf(".Scopes(models.EncryptedWhereScope(%q, %q, %s))", resolved.Column, f.Op, arg)
			continue
		}
		col = resolved.Column
		if f.Op == "BETWEEN" {
			arg2, _ := exprString(f.Arg2)
			chain += fmt.Sprintf(".Where(%q, %s, %s)", col+" BETWEEN ? AND ?", arg, arg2)
			continue
		}
		chain += fmt.Sprintf(".Where(%q, %s)", col+" "+f.Op+" ?", arg)
	}
	for _, p := range q.Preloads {
		chain += fmt.Sprintf(".Preload(%q)", p)
	}
	for _, order := range q.Orders {
		chain += fmt.Sprintf(".Order(models.OrderClause(%s, %s))", e.resolveQueryOrderColumn(q.Model, order.Column), order.Direction)
	}
	if q.Limit != nil {
		arg, _ := exprString(q.Limit)
		chain += ".Limit(" + arg + ")"
	}
	if q.Offset != nil {
		arg, _ := exprString(q.Offset)
		chain += ".Offset(" + arg + ")"
	}
	if q.LockStrength != "" || q.LockOptions != "" {
		strength := q.LockStrength
		if strength == "" {
			strength = "UPDATE"
		}
		chain += fmt.Sprintf(".Clauses(clause.Locking{Strength: %q", strength)
		if q.LockOptions != "" {
			chain += fmt.Sprintf(", Options: %q", q.LockOptions)
		}
		chain += "})"
	}
	return chain
}

func latestVersionPredicate(table string) string {
	return "version_id = (SELECT MAX(version_id) FROM " + table + " latest WHERE latest.id = " + table + ".id)"
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
		{"NotLike", "NOT LIKE"},
		{"Like", "LIKE"},
		{"Between", "BETWEEN"},
		{"NotIn", "NOT IN"},
		{"In", "IN"},
		{"After", ">"},
		{"Before", "<"},
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

func exportedModelsDBSource() ([]byte, error) {
	src := `package models

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

var DB *gorm.DB
var dbDriver string

func SetDB(db *gorm.DB) { DB = db }

func SetDBWithDriver(db *gorm.DB, driver string) {
	DB = db
	dbDriver = strings.ToLower(strings.TrimSpace(driver))
}

type Tx struct {
	DB *gorm.DB
}

func WithTransaction(fn func(tx *Tx) error) error {
	if DB == nil {
		return fmt.Errorf("models: DB is nil")
	}
	if fn == nil {
		return fmt.Errorf("models: transaction callback is nil")
	}
	return DB.Transaction(func(db *gorm.DB) error {
		return fn(&Tx{DB: db})
	})
}

func (tx *Tx) Transaction(fn func(tx *Tx) error) error {
	if tx == nil || tx.DB == nil {
		return fmt.Errorf("models: transaction is nil")
	}
	if fn == nil {
		return fmt.Errorf("models: transaction callback is nil")
	}
	return tx.DB.Transaction(func(db *gorm.DB) error {
		return fn(&Tx{DB: db})
	})
}

func ApplyLockTimeout(db *gorm.DB, d time.Duration) error {
	if db == nil || d <= 0 {
		return nil
	}
	sql, arg, ok := lockTimeoutStatement(d)
	if !ok {
		return nil
	}
	return db.Exec(sql, arg).Error
}

func lockTimeoutStatement(d time.Duration) (string, any, bool) {
	if d <= 0 {
		return "", nil, false
	}
	switch dbDriver {
	case "pgsql", "postgres":
		return "SET LOCAL lock_timeout = ?", fmt.Sprintf("%dms", d.Milliseconds()), true
	default:
		return "", nil, false
	}
}

func OrderClause(column, direction string) string {
	if !validSQLIdentifier(column) {
		return ""
	}
	dir := strings.ToUpper(strings.TrimSpace(direction))
	if dir != "ASC" && dir != "DESC" {
		return ""
	}
	return column + " " + dir
}

func RoleVisibilitySelectScope(publicCols []string, roleCols map[string][]string, ownerCols []string, managesRoles []string, roles []string, includeOwner bool) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if hasAnyRole(roles, managesRoles) {
			return db.Select("*")
		}
		cols := appendUniqueColumns(nil, publicCols)
		for _, role := range roles {
			cols = appendUniqueColumns(cols, roleCols[role])
		}
		if includeOwner {
			cols = appendUniqueColumns(cols, ownerCols)
		}
		if len(cols) == 0 {
			return db.Select("*")
		}
		return db.Select(cols)
	}
}

func hasAnyRole(roles []string, candidates []string) bool {
	for _, role := range roles {
		for _, candidate := range candidates {
			if role == candidate {
				return true
			}
		}
	}
	return false
}

func appendUniqueColumns(cols []string, add []string) []string {
	seen := map[string]bool{}
	for _, col := range cols {
		seen[col] = true
	}
	for _, col := range add {
		if col == "" || seen[col] {
			continue
		}
		cols = append(cols, col)
		seen[col] = true
	}
	return cols
}

func validSQLIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && r != '_' {
				return false
			}
			continue
		}
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

type LockOutsideTransactionError struct {
	Model string
}

func NewLockOutsideTransactionError(model string) *LockOutsideTransactionError {
	return &LockOutsideTransactionError{Model: model}
}

func (e *LockOutsideTransactionError) Error() string {
	return fmt.Sprintf("cannot lock %s outside a transaction", e.Model)
}
`
	formatted, err := format.Source([]byte(src))
	if err != nil {
		return nil, err
	}
	return formatted, nil
}

func (e *exporter) writeHTTPX() error {
	return e.writeFile("internal/httpx/httpx.go", []byte(httpxSource))
}

func (e *exporter) writeModels(tables []*schema.Table, views []*schema.View) error {
	dbSource, err := exportedModelsDBSource()
	if err != nil {
		return err
	}
	if err := e.writeFile("app/models/db.go", dbSource); err != nil {
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
	if e.needsModelQuerySupport() {
		data, err := e.generateModelQuerySupport(tables, views, e.hasGraphQLPackage())
		if err != nil {
			return err
		}
		filename := "query_support.go"
		if e.hasGraphQLPackage() {
			filename = "graphql_query_support.go"
		}
		if err := e.writeFile(filepath.Join("app", "models", filename), data); err != nil {
			return err
		}
	}
	if len(e.scopes) > 0 {
		data, err := e.generateCustomScopeSupport()
		if err != nil {
			return err
		}
		if len(data) > 0 {
			if err := e.writeFile(filepath.Join("app", "models", "custom_scopes_gen.go"), data); err != nil {
				return err
			}
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
	var schemaSDL string
	if err := filepath.WalkDir(graphqlDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) != "schema_gen.go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sdl, err := extractStringConstFromGoSource(data, "SchemaSDL")
		if err != nil {
			return fmt.Errorf("extracting exported GraphQL schema SDL: %w", err)
		}
		schemaSDL = sdl
		return nil
	}); err != nil {
		return err
	}
	if schemaSDL == "" {
		return fmt.Errorf("exported GraphQL schema SDL was not found")
	}
	supportedActions, _ := e.exportedGraphQLControllerActions()
	schemaSDL = addGraphQLControllerActionSDL(schemaSDL, supportedActions)
	schemaSDL = normalizeGraphQLSDL(schemaSDL)
	if err := e.writeGraphQLTargetFiles(schemaSDL, supportedActions); err != nil {
		return err
	}
	relationships := exportedGraphQLRelationships(tables)
	relationshipBudgets := e.exportedGraphQLRelationshipBudgets()
	if err := e.writeGraphQLAPIResolverTarget(schemaSDL, tables, relationships, relationshipBudgets, supportedActions); err != nil {
		return err
	}
	return nil
}

func (e *exporter) writeGraphQLTargetFiles(schemaSDL string, actions []exportedGraphQLControllerAction) error {
	schemaSDL = strings.TrimSpace(schemaSDL) + "\n"
	if err := e.writeFile(filepath.Join("app", "graphqlapi", "schema.graphqls"), []byte(schemaSDL)); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("schema:\n")
	b.WriteString("  - app/graphqlapi/schema.graphqls\n\n")
	b.WriteString("exec:\n")
	b.WriteString("  filename: app/graphqlapi/generated/generated.go\n")
	b.WriteString("  package: generated\n\n")
	b.WriteString("model:\n")
	b.WriteString("  filename: app/graphqlapi/model/models_gen.go\n")
	b.WriteString("  package: model\n\n")
	b.WriteString("resolver:\n")
	b.WriteString("  layout: follow-schema\n")
	b.WriteString("  dir: app/graphqlapi/resolver\n")
	b.WriteString("  package: resolver\n\n")
	b.WriteString("autobind:\n")
	fmt.Fprintf(&b, "  - %s/app/models\n", e.modulePath)
	if len(actions) > 0 {
		b.WriteString("\nmodels:\n")
		b.WriteString("  JSON:\n")
		b.WriteString("    model:\n")
		b.WriteString("      - map[string]interface{}\n")
	}
	if err := e.writeFile("gqlgen.yml", []byte(b.String())); err != nil {
		return err
	}
	return e.writeFile(filepath.Join("tools", "gqlgen.go"), []byte(`//go:build tools
// +build tools

package tools

import _ "github.com/99designs/gqlgen"
	`))
}

func (e *exporter) writeGraphQLAPIResolverTarget(schemaSDL string, tables []*schema.Table, relationships []generator.SchemaRelationship, relationshipBudgets map[string]exportedGraphQLRelationshipBudget, actions []exportedGraphQLControllerAction) error {
	exposed := graphQLTablesInSDL(schemaSDL, tables)
	if len(exposed) == 0 && len(actions) == 0 {
		return nil
	}
	if err := e.writeGraphQLAPIHandlerTarget(); err != nil {
		return err
	}
	if len(actions) > 0 {
		if err := e.writeGraphQLAPIGeneratedJSONScalarTarget(); err != nil {
			return err
		}
	}
	if err := e.writeGraphQLAPIComplexityTarget(schemaSDL, exposed, relationships, relationshipBudgets); err != nil {
		return err
	}
	if err := e.writeFile(filepath.Join("app", "graphqlapi", "resolver", "resolver.go"), []byte("package resolver\n\ntype Resolver struct{}\n")); err != nil {
		return err
	}

	var b strings.Builder
	hasQuery := graphQLSDLHasQueryType(schemaSDL)
	b.WriteString("package resolver\n\n")
	b.WriteString("import (\n")
	b.WriteString("\t\"context\"\n")
	if graphQLAPIMutationsNeedJSON(schemaSDL, exposed) {
		b.WriteString("\t\"encoding/json\"\n")
	}
	if len(exposed) > 0 {
		b.WriteString("\t\"fmt\"\n")
		b.WriteString("\t\"time\"\n\n")
		b.WriteString("\t\"github.com/google/uuid\"\n")
	} else {
		b.WriteString("\n")
	}
	if len(actions) > 0 {
		fmt.Fprintf(&b, "\t\"%s/app/http/controllers\"\n", e.modulePath)
	}
	fmt.Fprintf(&b, "\t\"%s/app/graphqlapi/generated\"\n", e.modulePath)
	if len(exposed) > 0 {
		fmt.Fprintf(&b, "\t\"%s/app/graphqlapi/model\"\n", e.modulePath)
		fmt.Fprintf(&b, "\t\"%s/app/models\"\n", e.modulePath)
	}
	if len(actions) > 0 {
		fmt.Fprintf(&b, "\t\"%s/internal/httpx\"\n", e.modulePath)
	}
	b.WriteString(")\n\n")
	if hasQuery {
		b.WriteString("type queryResolver struct{ *Resolver }\n")
	}
	if graphQLSDLHasMutationType(schemaSDL) {
		b.WriteString("type mutationResolver struct{ *Resolver }\n")
	}
	for _, tbl := range exposed {
		if graphQLTableNeedsObjectResolver(schemaSDL, tbl, relationships) {
			fmt.Fprintf(&b, "type %sResolver struct{ *Resolver }\n", graphQLResolverTypeName(tbl))
		}
	}
	b.WriteString("\n")
	for _, tbl := range exposed {
		if graphQLSDLHasQueryField(schemaSDL, graphQLListFieldName(tbl)) {
			writeGraphQLAPIListResolver(&b, tbl)
		}
		if graphQLSDLHasQueryField(schemaSDL, graphQLSingleFieldName(tbl)) {
			writeGraphQLAPISingleResolver(&b, tbl)
		}
		if graphQLSDLHasMutationField(schemaSDL, "create"+tableToStruct(tbl.Name)) {
			writeGraphQLAPICreateResolver(&b, tbl)
		}
		if graphQLSDLHasMutationField(schemaSDL, "update"+tableToStruct(tbl.Name)) {
			writeGraphQLAPIUpdateResolver(&b, tbl)
		}
		if graphQLSDLHasMutationField(schemaSDL, "delete"+tableToStruct(tbl.Name)) {
			writeGraphQLAPIDeleteResolver(&b, tbl)
		}
		if graphQLTableNeedsObjectResolver(schemaSDL, tbl, relationships) {
			writeGraphQLAPIObjectResolvers(&b, schemaSDL, tbl, exposed, relationships, relationshipBudgets)
		}
	}
	for _, action := range actions {
		writeGraphQLAPIControllerActionResolver(&b, action)
	}
	for _, tbl := range exposed {
		if graphQLTableNeedsObjectResolver(schemaSDL, tbl, relationships) {
			structName := tableToStruct(tbl.Name)
			resolverName := graphQLResolverTypeName(tbl)
			fmt.Fprintf(&b, "func (r *Resolver) %s() generated.%sResolver { return &%sResolver{r} }\n\n", structName, structName, resolverName)
		}
	}
	if graphQLSDLHasMutationType(schemaSDL) {
		b.WriteString("func (r *Resolver) Mutation() generated.MutationResolver { return &mutationResolver{r} }\n\n")
	}
	if hasQuery {
		b.WriteString("func (r *Resolver) Query() generated.QueryResolver { return &queryResolver{r} }\n")
	}

	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return fmt.Errorf("formatting exported gqlgen resolver target: %w", err)
	}
	if err := e.writeFile(filepath.Join("app", "graphqlapi", "resolver", "schema.resolvers.go"), formatted); err != nil {
		return err
	}

	var support strings.Builder
	support.WriteString("package resolver\n\n")
	support.WriteString("import (\n")
	if len(actions) > 0 {
		support.WriteString("\t\"bytes\"\n")
	}
	support.WriteString("\t\"context\"\n")
	if len(actions) > 0 {
		support.WriteString("\t\"encoding/json\"\n")
	}
	support.WriteString("\t\"errors\"\n")
	support.WriteString("\t\"fmt\"\n")
	support.WriteString("\t\"reflect\"\n")
	if len(actions) > 0 {
		support.WriteString("\t\"net/http\"\n")
		support.WriteString("\t\"net/http/httptest\"\n")
	}
	support.WriteString("\t\"strings\"\n")
	if len(exposed) > 0 {
		support.WriteString("\t\"time\"\n\n")
		support.WriteString("\t\"github.com/google/uuid\"\n")
	} else {
		support.WriteString("\n")
	}
	support.WriteString("\t\"github.com/vektah/gqlparser/v2/gqlerror\"\n")
	fmt.Fprintf(&support, "\t\"%s/app/graphqlapi/model\"\n", e.modulePath)
	if len(exposed) > 0 {
		fmt.Fprintf(&support, "\t\"%s/app/models\"\n", e.modulePath)
	}
	if len(actions) > 0 {
		fmt.Fprintf(&support, "\t\"%s/internal/httpx\"\n", e.modulePath)
	}
	support.WriteString("\t\"gorm.io/gorm\"\n")
	support.WriteString(")\n\n")
	writeGraphQLAPIResolverSupport(&support)
	if len(actions) > 0 {
		writeGraphQLAPIControllerActionSupport(&support)
	}
	for _, tbl := range exposed {
		if graphQLSDLHasQueryField(schemaSDL, graphQLListFieldName(tbl)) {
			writeGraphQLAPIFilterApplier(&support, tbl)
		}
	}
	formattedSupport, err := format.Source([]byte(support.String()))
	if err != nil {
		return fmt.Errorf("formatting exported gqlgen resolver support: %w", err)
	}
	return e.writeFile(filepath.Join("app", "graphqlapi", "resolver", "support_gen.go"), formattedSupport)
}

func (e *exporter) writeGraphQLAPIGeneratedJSONScalarTarget() error {
	src := `package generated

import (
	"context"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/ast"
)

func (ec *executionContext) unmarshalInputJSON(ctx context.Context, v any) (map[string]any, error) {
	_ = ctx
	return graphql.UnmarshalMap(v)
}

func (ec *executionContext) _JSON(ctx context.Context, sel ast.SelectionSet, v map[string]any) graphql.Marshaler {
	_ = ctx
	_ = sel
	return graphql.MarshalMap(v)
}
`
	formatted, err := format.Source([]byte(src))
	if err != nil {
		return fmt.Errorf("formatting exported gqlgen JSON scalar target: %w", err)
	}
	return e.writeFile(filepath.Join("app", "graphqlapi", "generated", "json_scalar_gen.go"), formatted)
}

func (e *exporter) writeGraphQLAPIHandlerTarget() error {
	src := fmt.Sprintf(exportedGraphQLAPIHandlerSource, e.modulePath, e.modulePath, e.modulePath, e.modulePath)
	formatted, err := format.Source([]byte(src))
	if err != nil {
		return fmt.Errorf("formatting exported gqlgen API handler: %w", err)
	}
	return e.writeFile(filepath.Join("app", "graphqlapi", "handler_gen.go"), formatted)
}

func (e *exporter) writeGraphQLAPIComplexityTarget(schemaSDL string, tables []*schema.Table, relationships []generator.SchemaRelationship, relationshipBudgets map[string]exportedGraphQLRelationshipBudget) error {
	usesModel := false
	for _, tbl := range tables {
		if graphQLSDLHasQueryField(schemaSDL, graphQLListFieldName(tbl)) {
			usesModel = true
			break
		}
	}

	var b strings.Builder
	b.WriteString("package graphqlapi\n\n")
	b.WriteString("import (\n")
	fmt.Fprintf(&b, "\t\"%s/app/graphqlapi/generated\"\n", e.modulePath)
	if usesModel {
		fmt.Fprintf(&b, "\t\"%s/app/graphqlapi/model\"\n", e.modulePath)
	}
	b.WriteString(")\n\n")
	b.WriteString("const defaultGraphQLAPIComplexityPageSize = 25\n")
	b.WriteString("const maxGraphQLAPIComplexityPageSize = 100\n\n")
	b.WriteString("var graphQLAPIRelationshipFields = map[string]bool{\n")
	for _, tbl := range tables {
		parentStruct := tableToStruct(tbl.Name)
		for _, rel := range graphQLTableRelationshipsInSDL(schemaSDL, tbl, relationships) {
			fmt.Fprintf(&b, "\t%q: true,\n", parentStruct+"."+lowerFirst(snakeToPascal(snakeToCamel(rel.ChildTable))))
		}
	}
	b.WriteString("}\n\n")
	b.WriteString("func graphQLAPIComplexityRoot() generated.ComplexityRoot {\n")
	b.WriteString("\tvar root generated.ComplexityRoot\n")
	for _, tbl := range tables {
		structName := tableToStruct(tbl.Name)
		if graphQLSDLHasQueryField(schemaSDL, graphQLListFieldName(tbl)) {
			fieldName := snakeToPascal(graphQLListFieldName(tbl))
			fmt.Fprintf(&b, "\troot.Query.%s = func(childComplexity int, _ *model.%sFilter, _ *model.%sSort, page *model.PageInput) int {\n", fieldName, structName, structName)
			b.WriteString("\t\treturn graphQLAPIListComplexity(childComplexity, 1, graphQLAPIPageInputLimit(page, maxGraphQLAPIComplexityPageSize))\n")
			b.WriteString("\t}\n")
		}
		if graphQLSDLHasQueryField(schemaSDL, graphQLSingleFieldName(tbl)) {
			fieldName := snakeToPascal(graphQLSingleFieldName(tbl))
			fmt.Fprintf(&b, "\troot.Query.%s = func(childComplexity int, _ string) int {\n", fieldName)
			b.WriteString("\t\treturn 1 + childComplexity\n")
			b.WriteString("\t}\n")
		}
	}
	for _, tbl := range tables {
		parentStruct := tableToStruct(tbl.Name)
		for _, rel := range graphQLTableRelationshipsInSDL(schemaSDL, tbl, relationships) {
			if rel.Type != "has_many" {
				continue
			}
			fieldName := snakeToPascal(snakeToCamel(rel.ChildTable))
			relationshipField := snakeToCamel(rel.ChildTable)
			budget := relationshipBudgets[tbl.Name+"."+relationshipField]
			cost := budget.Cost
			if cost <= 0 {
				cost = defaultExportedGraphQLRelationshipCost
			}
			limit := budget.MaxPageSize
			if limit <= 0 {
				limit = maxExportedGraphQLRelationshipPageSize
			}
			fmt.Fprintf(&b, "\troot.%s.%s = func(childComplexity int) int {\n", parentStruct, fieldName)
			fmt.Fprintf(&b, "\t\treturn graphQLAPIListComplexity(childComplexity, %d, min(%d, maxGraphQLAPIComplexityPageSize))\n", cost, limit)
			b.WriteString("\t}\n")
		}
	}
	b.WriteString("\treturn root\n")
	b.WriteString("}\n\n")
	if usesModel {
		b.WriteString("func graphQLAPIPageInputLimit(page *model.PageInput, maxLimit int) int {\n")
		b.WriteString("\tlimit := defaultGraphQLAPIComplexityPageSize\n")
		b.WriteString("\tif page != nil {\n")
		b.WriteString("\t\tif page.First != nil {\n\t\t\tlimit = *page.First\n\t\t} else if page.Last != nil {\n\t\t\tlimit = *page.Last\n\t\t}\n")
		b.WriteString("\t}\n")
		b.WriteString("\tif limit <= 0 || limit > maxLimit {\n\t\treturn maxGraphQLAPIComplexity + 1\n\t}\n")
		b.WriteString("\treturn limit\n")
		b.WriteString("}\n\n")
	}
	b.WriteString("func graphQLAPIListComplexity(childComplexity, baseCost, limit int) int {\n")
	b.WriteString("\tif baseCost <= 0 {\n\t\tbaseCost = 1\n\t}\n")
	b.WriteString("\tif limit <= 0 {\n\t\treturn maxGraphQLAPIComplexity + 1\n\t}\n")
	b.WriteString("\treturn (baseCost + childComplexity) * limit\n")
	b.WriteString("}\n")
	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return fmt.Errorf("formatting exported gqlgen complexity target: %w", err)
	}
	return e.writeFile(filepath.Join("app", "graphqlapi", "complexity_gen.go"), formatted)
}

func writeGraphQLAPIResolverSupport(b *strings.Builder) {
	b.WriteString(`const defaultGraphQLAPIPageSize = 25
const maxGraphQLAPIPageSize = 100
const maxGraphQLAPIInputListSize = 100

type graphQLAPIAuthContextKey struct{}

type GraphQLAPIAuthClaims struct {
	UserID     string
	Role       string
	Roles      []string
	Manages    bool
	RBACLoaded bool
}

func WithGraphQLAPIAuthClaims(ctx context.Context, claims *GraphQLAPIAuthClaims) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, graphQLAPIAuthContextKey{}, claims)
}

func GraphQLAPIAuthFromContext(ctx context.Context) *GraphQLAPIAuthClaims {
	if ctx == nil {
		return nil
	}
	claims, _ := ctx.Value(graphQLAPIAuthContextKey{}).(*GraphQLAPIAuthClaims)
	return claims
}

func graphQLAPIAuthCanManage(claims *GraphQLAPIAuthClaims) bool {
	return claims != nil && (claims.Manages || (!claims.RBACLoaded && claims.Role == "admin"))
}

func graphQLAPIResolverError(message, code string) *gqlerror.Error {
	return &gqlerror.Error{
		Message:    message,
		Extensions: map[string]any{"code": code},
	}
}

func graphQLAPIBadInput(message string) *gqlerror.Error {
	return graphQLAPIResolverError(message, "BAD_USER_INPUT")
}

func graphQLAPIUnauthenticated(message string) *gqlerror.Error {
	return graphQLAPIResolverError(message, "UNAUTHENTICATED")
}

func graphQLAPIRecordNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

func graphQLAPISetZero(record any, fieldName string) {
	value := reflect.ValueOf(record)
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return
	}
	field := value.FieldByName(fieldName)
	if !field.IsValid() || !field.CanSet() {
		return
	}
	field.Set(reflect.Zero(field.Type()))
}

func gqlgenStringPtr(value string) *string {
	return &value
}

func gqlgenPageLimit(page *model.PageInput) (int, error) {
	limit := defaultGraphQLAPIPageSize
	if page != nil {
		if page.First != nil && page.Last != nil {
			return 0, graphQLAPIBadInput("page.first and page.last cannot both be set")
		}
		if page.First != nil {
			limit = *page.First
		} else if page.Last != nil {
			limit = *page.Last
		}
	}
	if limit <= 0 {
		return 0, graphQLAPIBadInput("page limit must be positive")
	}
	if limit > maxGraphQLAPIPageSize {
		return 0, graphQLAPIBadInput(fmt.Sprintf("page limit exceeds maximum %d", maxGraphQLAPIPageSize))
	}
	return limit, nil
}

func gqlgenPageOffset(page *model.PageInput) (int, error) {
	if page == nil {
		return 0, nil
	}
	if page.Before != nil {
		if _, err := gqlgenParseCursor(*page.Before); err != nil {
			return 0, graphQLAPIBadInput("page.before is invalid")
		}
	}
	if page.After == nil {
		return 0, nil
	}
	offset, err := gqlgenParseCursor(*page.After)
	if err != nil {
		return 0, graphQLAPIBadInput("page.after is invalid")
	}
	return offset, nil
}

func gqlgenPageWindow(page *model.PageInput, totalCount, limit int) (int, int, bool, bool, error) {
	start := 0
	end := totalCount
	if page != nil {
		if page.After != nil {
			after, err := gqlgenParseCursor(*page.After)
			if err != nil {
				return 0, 0, false, false, graphQLAPIBadInput("page.after is invalid")
			}
			start = after + 1
		}
		if page.Before != nil {
			before, err := gqlgenParseCursor(*page.Before)
			if err != nil {
				return 0, 0, false, false, graphQLAPIBadInput("page.before is invalid")
			}
			if before < end {
				end = before
			}
		}
	}
	if start > totalCount {
		start = totalCount
	}
	if end > totalCount {
		end = totalCount
	}
	if start > end {
		start = end
	}
	if page != nil && page.Last != nil {
		if end-start > limit {
			start = end - limit
		}
	} else if end-start > limit {
		end = start + limit
	}
	return start, end - start, start > 0, end < totalCount, nil
}

func gqlgenParseCursor(cursor string) (int, error) {
	if !strings.HasPrefix(cursor, "cursor:") {
		return 0, fmt.Errorf("invalid cursor")
	}
	var offset int
	if _, err := fmt.Sscanf(cursor, "cursor:%d", &offset); err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid cursor")
	}
	return offset, nil
}

func gqlgenCursor(offset int) string {
	return fmt.Sprintf("cursor:%d", offset)
}

func gqlgenSortParts(sort string) (string, string) {
	if strings.HasSuffix(sort, "_DESC") {
		return strings.ToLower(strings.TrimSuffix(sort, "_DESC")), "DESC"
	}
	if strings.HasSuffix(sort, "_ASC") {
		return strings.ToLower(strings.TrimSuffix(sort, "_ASC")), "ASC"
	}
	return "", "ASC"
}

`)
}

func writeGraphQLAPIControllerActionResolver(b *strings.Builder, action exportedGraphQLControllerAction) {
	methodName := snakeToPascal(action.Name)
	fmt.Fprintf(b, "func (r *mutationResolver) %s(ctx context.Context, input map[string]any) (map[string]any, error) {\n", methodName)
	fmt.Fprintf(b, "\treturn graphQLAPIControllerAction(ctx, %q, input, func(actionCtx *httpx.Context) httpx.Response {\n", action.Name)
	fmt.Fprintf(b, "\t\treturn controllers.%s{}.%s(actionCtx)\n", action.Controller, action.Method)
	b.WriteString("\t})\n")
	b.WriteString("}\n\n")
}

func writeGraphQLAPIControllerActionSupport(b *strings.Builder) {
	b.WriteString(`type graphQLAPIControllerActionHandler func(*httpx.Context) httpx.Response

func graphQLAPIControllerAction(ctx context.Context, action string, input map[string]any, handler graphQLAPIControllerActionHandler) (map[string]any, error) {
	claims := GraphQLAPIAuthFromContext(ctx)
	if claims == nil {
		return nil, graphQLAPIUnauthenticated(action+": authentication required")
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, graphQLAPIBadInput(action+": invalid input")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/graphql/actions/"+action, bytes.NewReader(body))
	if err != nil {
		return nil, graphQLAPIResolverError("internal server error", "INTERNAL_SERVER_ERROR")
	}
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	actionCtx := httpx.NewContextWithResponse(recorder, req)
	actionCtx.SetAuth(&httpx.AuthInfo{UserID: claims.UserID, Role: claims.Role})
	var roles []httpx.RoleInfo
	for _, role := range claims.Roles {
		roles = append(roles, httpx.RoleInfo{Slug: role, Manages: claims.Manages})
	}
	if len(roles) == 0 && claims.Role != "" {
		roles = append(roles, httpx.RoleInfo{Slug: claims.Role, Manages: claims.Manages})
	}
	if claims.RBACLoaded || len(roles) > 0 {
		actionCtx.SetRoles(roles)
	}
	resp := handler(actionCtx)
	status := resp.Status
	if status == 0 {
		status = resp.StatusCode
	}
	if status == 0 {
		status = http.StatusOK
	}
	if status < 200 || status >= 300 {
		return nil, graphQLAPIControllerActionStatusError(action, status)
	}
	return graphQLAPIMapFromBody(resp.Body), nil
}

func graphQLAPIControllerActionStatusError(action string, status int) error {
	switch status {
	case http.StatusUnauthorized:
		return graphQLAPIUnauthenticated(action + ": unauthenticated")
	case http.StatusForbidden:
		return graphQLAPIResolverError(action+": forbidden", "FORBIDDEN")
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return graphQLAPIBadInput(action + ": invalid input")
	default:
		return graphQLAPIResolverError(action+": failed", "INTERNAL_SERVER_ERROR")
	}
}

func graphQLAPIMapFromBody(body any) map[string]any {
	switch v := body.(type) {
	case nil:
		return map[string]any{}
	case map[string]any:
		return v
	case map[string]string:
		out := map[string]any{}
		for key, value := range v {
			out[key] = value
		}
		return out
	default:
		return map[string]any{"data": graphQLAPIJSONValue(v)}
	}
}

func graphQLAPIJSONValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, child := range v {
			out[key] = graphQLAPIJSONValue(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = graphQLAPIJSONValue(child)
		}
		return out
	default:
		return v
	}
}

`)
}

func graphQLTablesInSDL(schemaSDL string, tables []*schema.Table) []*schema.Table {
	var out []*schema.Table
	for _, tbl := range tables {
		structName := tableToStruct(tbl.Name)
		if strings.Contains(schemaSDL, "type "+structName+" {") || strings.Contains(schemaSDL, "type "+structName+" ") {
			out = append(out, tbl)
		}
	}
	return out
}

func graphQLSDLHasQueryField(schemaSDL, field string) bool {
	return strings.Contains(schemaSDL, "\n  "+field+"(") || strings.Contains(schemaSDL, "\n  "+field+":")
}

func graphQLSDLHasQueryType(schemaSDL string) bool {
	return strings.Contains(schemaSDL, "type Query {")
}

func graphQLSDLHasMutationType(schemaSDL string) bool {
	return strings.Contains(schemaSDL, "type Mutation {")
}

func graphQLSDLHasMutationField(schemaSDL, field string) bool {
	return strings.Contains(schemaSDL, "\n  "+field+"(")
}

func graphQLAPIMutationsNeedJSON(schemaSDL string, tables []*schema.Table) bool {
	for _, tbl := range tables {
		if !graphQLSDLHasMutationField(schemaSDL, "create"+tableToStruct(tbl.Name)) && !graphQLSDLHasMutationField(schemaSDL, "update"+tableToStruct(tbl.Name)) {
			continue
		}
		for _, col := range tbl.Columns {
			if col.Type == schema.JSONB && graphQLAPICreateInputHasColumn(tbl, col) {
				return true
			}
		}
	}
	return false
}

type exportedGraphQLControllerAction struct {
	Name       string
	Handler    string
	Controller string
	Method     string
}

func (e *exporter) exportedGraphQLControllerActions() ([]exportedGraphQLControllerAction, []generator.DerivedAction) {
	if !e.hasGraphQLPolicies() {
		return nil, nil
	}
	state := generator.DeriveGraphQLStateFromDir(filepath.Join(e.project.Dir, "database", "policies", "graphql"))
	var supported []exportedGraphQLControllerAction
	var unsupported []generator.DerivedAction
	for _, action := range state.Actions {
		lowered, ok := exportedGraphQLControllerActionFromDerived(action)
		if ok {
			ok = e.validExportedGraphQLControllerAction(lowered)
		}
		if !ok {
			unsupported = append(unsupported, action)
			continue
		}
		supported = append(supported, lowered)
	}
	return supported, unsupported
}

func exportedGraphQLControllerActionFromDerived(action generator.DerivedAction) (exportedGraphQLControllerAction, bool) {
	if !validExportedGraphQLName(action.Name) || action.Handler == "" || action.Handler == "nil" {
		return exportedGraphQLControllerAction{}, false
	}
	expr, err := parser.ParseExpr(action.Handler)
	if err != nil {
		return exportedGraphQLControllerAction{}, false
	}
	method, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return exportedGraphQLControllerAction{}, false
	}
	composite, ok := method.X.(*ast.CompositeLit)
	if !ok || len(composite.Elts) != 0 {
		return exportedGraphQLControllerAction{}, false
	}
	controllerType, ok := composite.Type.(*ast.SelectorExpr)
	if !ok {
		return exportedGraphQLControllerAction{}, false
	}
	pkg, ok := controllerType.X.(*ast.Ident)
	if !ok || pkg.Name != "controllers" {
		return exportedGraphQLControllerAction{}, false
	}
	if !ast.IsExported(controllerType.Sel.Name) || !ast.IsExported(method.Sel.Name) {
		return exportedGraphQLControllerAction{}, false
	}
	return exportedGraphQLControllerAction{
		Name:       action.Name,
		Handler:    action.Handler,
		Controller: controllerType.Sel.Name,
		Method:     method.Sel.Name,
	}, true
}

func (e *exporter) validExportedGraphQLControllerAction(action exportedGraphQLControllerAction) bool {
	if e == nil || e.project == nil || action.Controller == "" || action.Method == "" {
		return false
	}
	controllerDir := filepath.Join(e.project.Dir, "app", "http", "controllers")
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, controllerDir, func(fi fs.FileInfo) bool {
		return !fi.IsDir() && strings.HasSuffix(fi.Name(), ".go") && !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		return false
	}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Name.Name != action.Method || actionReceiverTypeName(fn.Recv) != action.Controller {
					continue
				}
				return validExportedGraphQLControllerMethodSignature(fn.Type)
			}
		}
	}
	return false
}

func validExportedGraphQLControllerMethodSignature(fn *ast.FuncType) bool {
	return fn != nil &&
		lenFieldList(fn.Params) == 1 &&
		lenFieldList(fn.Results) == 1 &&
		isContextPointerType(fn.Params.List[0].Type) &&
		exprNamedType(fn.Results.List[0].Type) == "Response"
}

func validExportedGraphQLName(name string) bool {
	for i, r := range name {
		if i == 0 {
			if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				continue
			}
			return false
		}
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return name != ""
}

func addGraphQLControllerActionSDL(schemaSDL string, actions []exportedGraphQLControllerAction) string {
	if len(actions) == 0 {
		return schemaSDL
	}
	schemaSDL = strings.TrimSpace(schemaSDL)
	if !strings.Contains(schemaSDL, "scalar JSON") {
		if strings.Contains(schemaSDL, "scalar DateTime\n") {
			schemaSDL = strings.Replace(schemaSDL, "scalar DateTime\n", "scalar DateTime\nscalar JSON\n", 1)
		} else {
			schemaSDL = "scalar JSON\n\n" + schemaSDL
		}
	}
	var fields strings.Builder
	for _, action := range actions {
		if graphQLSDLHasMutationField(schemaSDL, action.Name) {
			continue
		}
		fmt.Fprintf(&fields, "  %s(input: JSON!): JSON! @auth\n", action.Name)
	}
	if fields.Len() == 0 {
		return schemaSDL + "\n"
	}
	if !graphQLSDLHasMutationType(schemaSDL) {
		return schemaSDL + "\n\ntype Mutation {\n" + fields.String() + "}\n"
	}
	start := strings.Index(schemaSDL, "type Mutation {")
	if start < 0 {
		return schemaSDL + "\n"
	}
	closeRel := strings.Index(schemaSDL[start:], "\n}")
	if closeRel < 0 {
		return schemaSDL + "\n"
	}
	insertAt := start + closeRel
	return schemaSDL[:insertAt] + "\n" + fields.String() + schemaSDL[insertAt:] + "\n"
}

func normalizeGraphQLSDL(schemaSDL string) string {
	schemaSDL = removeEmptyGraphQLObjectType(schemaSDL, "Query")
	schemaSDL = removeEmptyGraphQLObjectType(schemaSDL, "Mutation")
	return strings.TrimSpace(schemaSDL) + "\n"
}

func removeEmptyGraphQLObjectType(schemaSDL, typeName string) string {
	marker := "type " + typeName + " {"
	start := strings.Index(schemaSDL, marker)
	if start < 0 {
		return schemaSDL
	}
	open := strings.Index(schemaSDL[start:], "{")
	if open < 0 {
		return schemaSDL
	}
	open += start
	closeRel := strings.Index(schemaSDL[open:], "}")
	if closeRel < 0 {
		return schemaSDL
	}
	close := open + closeRel
	if strings.TrimSpace(schemaSDL[open+1:close]) != "" {
		return schemaSDL
	}
	removeStart := start
	for removeStart > 0 && schemaSDL[removeStart-1] == '\n' {
		removeStart--
	}
	removeEnd := close + 1
	for removeEnd < len(schemaSDL) && schemaSDL[removeEnd] == '\n' {
		removeEnd++
	}
	if removeStart > 0 && removeEnd < len(schemaSDL) {
		return schemaSDL[:removeStart] + "\n\n" + schemaSDL[removeEnd:]
	}
	return schemaSDL[:removeStart] + schemaSDL[removeEnd:]
}

func graphQLListFieldName(tbl *schema.Table) string {
	return snakeToCamel(tbl.Name)
}

func graphQLSingleFieldName(tbl *schema.Table) string {
	return snakeToCamel(modelFileName(tbl.Name))
}

func graphQLResolverTypeName(tbl *schema.Table) string {
	structName := tableToStruct(tbl.Name)
	return strings.ToLower(structName[:1]) + structName[1:]
}

func graphQLTableNeedsObjectResolver(schemaSDL string, tbl *schema.Table, relationships []generator.SchemaRelationship) bool {
	for _, col := range tbl.Columns {
		if graphQLColumnResolverReturnType(col) != "" && !isExcludedFromExportedGraphQL(tbl, col) {
			return true
		}
	}
	return len(graphQLTableRelationshipsInSDL(schemaSDL, tbl, relationships)) > 0
}

func exportedGraphQLRelationships(tables []*schema.Table) []generator.SchemaRelationship {
	var relationships []generator.SchemaRelationship
	parentTables := map[string]bool{}
	for _, tbl := range tables {
		parentTables[tbl.Name] = true
	}
	for _, child := range tables {
		for _, col := range child.Columns {
			if col.ForeignKeyTable == "" || !parentTables[col.ForeignKeyTable] {
				continue
			}
			relationships = append(relationships, generator.SchemaRelationship{
				Type:        "has_many",
				ParentTable: col.ForeignKeyTable,
				ChildTable:  child.Name,
				Collection:  true,
			})
		}
	}
	return relationships
}

const maxExportedGraphQLRelationshipPageSize = 100
const defaultExportedGraphQLRelationshipCost = 1

type exportedGraphQLRelationshipBudget struct {
	Cost        int
	MaxPageSize int
}

func (e *exporter) exportedGraphQLRelationshipBudgets() map[string]exportedGraphQLRelationshipBudget {
	if e == nil || e.project == nil {
		return nil
	}
	state := generator.DeriveGraphQLStateFromDir(filepath.Join(e.project.Dir, "database", "policies", "graphql"))
	if len(state.Exposures) == 0 {
		return nil
	}
	budgets := map[string]exportedGraphQLRelationshipBudget{}
	for _, exposure := range state.Exposures {
		for _, rel := range exposure.Relationships {
			if rel.Name == "" || (rel.Cost <= 0 && rel.MaxPageSize <= 0) {
				continue
			}
			budgets[exposure.Model+"."+rel.Name] = exportedGraphQLRelationshipBudget{
				Cost:        rel.Cost,
				MaxPageSize: rel.MaxPageSize,
			}
		}
	}
	if len(budgets) == 0 {
		return nil
	}
	return budgets
}

func graphQLTableRelationshipsInSDL(schemaSDL string, tbl *schema.Table, relationships []generator.SchemaRelationship) []generator.SchemaRelationship {
	var out []generator.SchemaRelationship
	typeName := tableToStruct(tbl.Name)
	for _, rel := range relationships {
		if rel.ParentTable != tbl.Name {
			continue
		}
		fieldName := snakeToCamel(rel.ChildTable)
		if graphQLSDLTypeHasField(schemaSDL, typeName, fieldName) {
			out = append(out, rel)
		}
	}
	return out
}

func graphQLSDLTypeHasField(schemaSDL, typeName, fieldName string) bool {
	block := graphQLSDLTypeBlock(schemaSDL, typeName)
	if block == "" {
		return false
	}
	return strings.Contains(block, "\n  "+fieldName+":") || strings.Contains(block, "\n  "+fieldName+"(")
}

func graphQLSDLTypeBlock(schemaSDL, typeName string) string {
	marker := "type " + typeName + " {"
	start := strings.Index(schemaSDL, marker)
	if start < 0 {
		return ""
	}
	brace := strings.Index(schemaSDL[start:], "{")
	if brace < 0 {
		return ""
	}
	pos := start + brace
	depth := 0
	for i := pos; i < len(schemaSDL); i++ {
		switch schemaSDL[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return schemaSDL[pos+1 : i]
			}
		}
	}
	return ""
}

func isExcludedFromExportedGraphQL(tbl *schema.Table, col *schema.Column) bool {
	if col.Type == schema.Binary {
		return true
	}
	if col.Name == "password_hash" || col.Name == "password" || col.Name == "row_hash" || col.Name == "prev_hash" || col.Name == "version_id" {
		return true
	}
	if tbl != nil {
		switch tbl.Name {
		case "jwt_tokens":
			return col.Name == "jti"
		case "oauth_tokens":
			return col.Name == "token"
		case "sessions":
			return col.Name == "id"
		}
	}
	return false
}

func graphQLColumnResolverReturnType(col *schema.Column) string {
	switch col.Type {
	case schema.UUID, schema.Timestamp, schema.Date, schema.Time, schema.Decimal, schema.JSONB:
		if col.IsNullable {
			return "*string"
		}
		return "string"
	default:
		return ""
	}
}

func writeGraphQLAPIListResolver(b *strings.Builder, tbl *schema.Table) {
	structName := tableToStruct(tbl.Name)
	listField := graphQLListFieldName(tbl)
	fmt.Fprintf(b, "func (r *queryResolver) %s(ctx context.Context, filter *model.%sFilter, sort *model.%sSort, page *model.PageInput) (*model.%sConnection, error) {\n", snakeToPascal(listField), structName, structName, structName)
	fmt.Fprintf(b, "\tq := models.Query%s()\n", structName)
	if exportedGraphQLTableHasVisibility(tbl) {
		fmt.Fprintf(b, "\tgraphQLAPISelect%sVisibility(ctx, q)\n", structName)
	}
	writeGraphQLAPIScopeTopLevelOwnerReadFromAuth(b, tbl, listField, "nil")
	b.WriteString("\tif filter != nil {\n")
	fmt.Fprintf(b, "\t\tif err := apply%sFilter(q, filter); err != nil {\n\t\t\treturn nil, err\n\t\t}\n", structName)
	b.WriteString("\t}\n")
	b.WriteString("\tif sort != nil {\n\t\tcolumn, dir := gqlgenSortParts(string(*sort))\n\t\tif column != \"\" {\n\t\t\tq.OrderBy(column, dir)\n\t\t}\n\t}\n")
	b.WriteString("\ttotalCount64, err := q.Count()\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\ttotalCount := int(totalCount64)\n")
	b.WriteString("\tlimit, err := gqlgenPageLimit(page)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\toffset, fetchLimit, hasPrevious, hasNext, err := gqlgenPageWindow(page, totalCount, limit)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\tq.Limit(fetchLimit)\n\tif offset > 0 {\n\t\tq.Offset(offset)\n\t}\n")
	b.WriteString("\trecords, err := q.All()\n\tif err != nil {\n\t\treturn nil, err\n\t}\n")
	fmt.Fprintf(b, "\tedges := make([]*model.%sEdge, 0, len(records))\n", structName)
	b.WriteString("\tfor i := range records {\n\t\trecord := records[i]\n")
	if graphQLTableHasOwnerOnlyFields(tbl) {
		fmt.Fprintf(b, "\t\tgraphQLAPIScrub%sVisibility(ctx, &record)\n", structName)
	}
	fmt.Fprintf(b, "\t\tedges = append(edges, &model.%sEdge{Node: &record, Cursor: gqlgenCursor(offset + i)})\n\t}\n", structName)
	b.WriteString("\tvar startCursor *string\n\tvar endCursor *string\n\tif len(edges) > 0 {\n\t\tstartCursor = gqlgenStringPtr(edges[0].Cursor)\n\t\tendCursor = gqlgenStringPtr(edges[len(edges)-1].Cursor)\n\t}\n")
	fmt.Fprintf(b, "\treturn &model.%sConnection{Edges: edges, PageInfo: &model.PageInfo{HasNextPage: hasNext, HasPreviousPage: hasPrevious, StartCursor: startCursor, EndCursor: endCursor}, TotalCount: totalCount}, nil\n", structName)
	b.WriteString("}\n\n")
}

func writeGraphQLAPISingleResolver(b *strings.Builder, tbl *schema.Table) {
	structName := tableToStruct(tbl.Name)
	pk := primaryKeyColumn(tbl)
	if pk == nil {
		return
	}
	fmt.Fprintf(b, "func (r *queryResolver) %s(ctx context.Context, id string) (*models.%s, error) {\n", snakeToPascal(graphQLSingleFieldName(tbl)), structName)
	writeGraphQLAPIPKParse(b, tbl, pk, "nil")
	fmt.Fprintf(b, "\tq := models.Query%s().Where%s(parsedID)\n", structName, snakeToPascal(pk.Name))
	if exportedGraphQLTableHasVisibility(tbl) {
		fmt.Fprintf(b, "\tgraphQLAPISelect%sVisibility(ctx, q)\n", structName)
	}
	writeGraphQLAPIScopeTopLevelOwnerReadFromAuth(b, tbl, graphQLSingleFieldName(tbl), "nil")
	b.WriteString("\trecord, err := q.First()\n\tif err != nil {\n\t\tif graphQLAPIRecordNotFound(err) {\n\t\t\treturn nil, nil\n\t\t}\n\t\treturn nil, err\n\t}\n")
	if graphQLTableHasOwnerOnlyFields(tbl) {
		fmt.Fprintf(b, "\tgraphQLAPIScrub%sVisibility(ctx, record)\n", structName)
	}
	b.WriteString("\treturn record, nil\n")
	b.WriteString("}\n\n")
}

func writeGraphQLAPICreateResolver(b *strings.Builder, tbl *schema.Table) {
	structName := tableToStruct(tbl.Name)
	fmt.Fprintf(b, "func (r *mutationResolver) Create%s(ctx context.Context, input model.Create%sInput) (*models.%s, error) {\n", structName, structName, structName)
	writeGraphQLAPIRequireAuth(b, "create"+structName, "nil")
	writeGraphQLAPIValidateCreateInternalFields(b, tbl)
	fmt.Fprintf(b, "\trecord := &models.%s{}\n", structName)
	writeGraphQLAPIInitializeCreateRecord(b, tbl)
	writeGraphQLAPIAssignOwnerFromAuth(b, tbl, "create"+structName, "nil")
	writeGraphQLAPIAssignCreateInput(b, tbl)
	fmt.Fprintf(b, "\tif err := models.Query%s().Create(record); err != nil {\n\t\treturn nil, err\n\t}\n", structName)
	b.WriteString("\treturn record, nil\n")
	b.WriteString("}\n\n")
}

func writeGraphQLAPIValidateCreateInternalFields(b *strings.Builder, tbl *schema.Table) {
	structName := tableToStruct(tbl.Name)
	for _, col := range tbl.Columns {
		if graphQLAPICreateInputHasColumn(tbl, col) || !graphQLAPIRequiredInternalCreateColumn(tbl, col) {
			continue
		}
		fmt.Fprintf(b, "\treturn nil, graphQLAPIBadInput(%q)\n", "create"+structName+": required internal field "+col.Name+" cannot be populated by GraphQL")
		return
	}
}

func graphQLAPIRequiredInternalCreateColumn(tbl *schema.Table, col *schema.Column) bool {
	if col == nil || col.IsPrimaryKey || col.IsNullable || col.HasDefault || col.Name == "created_at" || col.Name == "updated_at" {
		return false
	}
	return isExcludedFromExportedGraphQL(tbl, col)
}

func writeGraphQLAPIUpdateResolver(b *strings.Builder, tbl *schema.Table) {
	structName := tableToStruct(tbl.Name)
	pk := primaryKeyColumn(tbl)
	if pk == nil {
		return
	}
	fmt.Fprintf(b, "func (r *mutationResolver) Update%s(ctx context.Context, id string, input model.Update%sInput) (*models.%s, error) {\n", structName, structName, structName)
	writeGraphQLAPIRequireAuth(b, "update"+structName, "nil")
	writeGraphQLAPIPKParse(b, tbl, pk, "nil")
	fmt.Fprintf(b, "\tq := models.Query%s().Where%s(parsedID)\n", structName, snakeToPascal(pk.Name))
	writeGraphQLAPIScopeOwnerFromAuth(b, tbl, "update"+structName, "nil")
	b.WriteString("\trecord, err := q.First()\n")
	b.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
	writeGraphQLAPIAssignUpdateInput(b, tbl)
	if graphQLTableHasColumn(tbl, "updated_at") {
		b.WriteString("\trecord.UpdatedAt = time.Now().UTC()\n")
	}
	fmt.Fprintf(b, "\tif err := models.Query%s().Update(record); err != nil {\n\t\treturn nil, err\n\t}\n", structName)
	b.WriteString("\treturn record, nil\n")
	b.WriteString("}\n\n")
}

func writeGraphQLAPIDeleteResolver(b *strings.Builder, tbl *schema.Table) {
	structName := tableToStruct(tbl.Name)
	pk := primaryKeyColumn(tbl)
	if pk == nil {
		return
	}
	fmt.Fprintf(b, "func (r *mutationResolver) Delete%s(ctx context.Context, id string) (bool, error) {\n", structName)
	writeGraphQLAPIRequireAuth(b, "delete"+structName, "false")
	writeGraphQLAPIPKParse(b, tbl, pk, "false")
	fmt.Fprintf(b, "\tq := models.Query%s().Where%s(parsedID)\n", structName, snakeToPascal(pk.Name))
	writeGraphQLAPIScopeOwnerFromAuth(b, tbl, "delete"+structName, "false")
	b.WriteString("\trecord, err := q.First()\n")
	b.WriteString("\tif err != nil {\n\t\treturn false, err\n\t}\n")
	fmt.Fprintf(b, "\tif err := models.Query%s().Delete(record); err != nil {\n\t\treturn false, err\n\t}\n", structName)
	b.WriteString("\treturn true, nil\n")
	b.WriteString("}\n\n")
}

func writeGraphQLAPIRequireAuth(b *strings.Builder, operation, failureReturn string) {
	b.WriteString("\tif GraphQLAPIAuthFromContext(ctx) == nil {\n")
	fmt.Fprintf(b, "\t\treturn %s, graphQLAPIUnauthenticated(%q)\n", failureReturn, operation+": authentication required")
	b.WriteString("\t}\n")
}

func writeGraphQLAPIPKParse(b *strings.Builder, tbl *schema.Table, pk *schema.Column, failureReturn string) {
	switch pk.Type {
	case schema.UUID:
		b.WriteString("\tparsedID, err := uuid.Parse(id)\n\tif err != nil {\n")
		fmt.Fprintf(b, "\t\treturn %s, graphQLAPIBadInput(%q)\n", failureReturn, graphQLSingleFieldName(tbl)+": invalid id")
		b.WriteString("\t}\n")
	case schema.Integer:
		fmt.Fprintf(b, "\tvar parsedID int\n\tif _, err := fmt.Sscanf(id, \"%%d\", &parsedID); err != nil {\n\t\treturn %s, graphQLAPIBadInput(\"invalid id\")\n\t}\n", failureReturn)
	case schema.BigInteger:
		fmt.Fprintf(b, "\tvar parsedID int64\n\tif _, err := fmt.Sscanf(id, \"%%d\", &parsedID); err != nil {\n\t\treturn %s, graphQLAPIBadInput(\"invalid id\")\n\t}\n", failureReturn)
	default:
		b.WriteString("\tparsedID := id\n")
	}
}

func writeGraphQLAPIInitializeCreateRecord(b *strings.Builder, tbl *schema.Table) {
	nowNeeded := false
	for _, col := range tbl.Columns {
		field := snakeToPascal(col.Name)
		switch {
		case col.IsPrimaryKey && col.Type == schema.UUID:
			fmt.Fprintf(b, "\trecord.%s = uuid.New()\n", field)
		case col.IsPrimaryKey && col.Type == schema.String:
			fmt.Fprintf(b, "\trecord.%s = uuid.NewString()\n", field)
		case col.Name == "created_at" || col.Name == "updated_at":
			nowNeeded = true
		}
	}
	if nowNeeded {
		b.WriteString("\tnow := time.Now().UTC()\n")
		for _, col := range tbl.Columns {
			field := snakeToPascal(col.Name)
			if col.Name == "created_at" || col.Name == "updated_at" {
				fmt.Fprintf(b, "\trecord.%s = now\n", field)
			}
		}
	}
}

func writeGraphQLAPIAssignOwnerFromAuth(b *strings.Builder, tbl *schema.Table, operation, failureReturn string) {
	ownerCol := exportedGraphQLOwnerColumn(tbl)
	if ownerCol == nil {
		return
	}
	field := snakeToPascal(ownerCol.Name)
	b.WriteString("\tauth := GraphQLAPIAuthFromContext(ctx)\n")
	switch ownerCol.Type {
	case schema.UUID:
		b.WriteString("\townerID, err := uuid.Parse(auth.UserID)\n")
		fmt.Fprintf(b, "\tif err != nil {\n\t\treturn %s, graphQLAPIBadInput(%q)\n\t}\n", failureReturn, operation+": invalid owner ID")
		fmt.Fprintf(b, "\trecord.%s = ownerID\n", field)
	case schema.String, schema.Text:
		fmt.Fprintf(b, "\trecord.%s = auth.UserID\n", field)
	default:
		fmt.Fprintf(b, "\treturn %s, graphQLAPIBadInput(%q)\n", failureReturn, operation+": unsupported owner ID type")
	}
}

func writeGraphQLAPIScopeOwnerFromAuth(b *strings.Builder, tbl *schema.Table, operation, failureReturn string) {
	ownerCol := exportedGraphQLOwnerColumn(tbl)
	if ownerCol == nil {
		return
	}
	field := snakeToPascal(ownerCol.Name)
	b.WriteString("\tauth := GraphQLAPIAuthFromContext(ctx)\n")
	switch ownerCol.Type {
	case schema.UUID:
		b.WriteString("\townerID, err := uuid.Parse(auth.UserID)\n")
		fmt.Fprintf(b, "\tif err != nil {\n\t\treturn %s, graphQLAPIBadInput(%q)\n\t}\n", failureReturn, operation+": invalid owner ID")
		fmt.Fprintf(b, "\tq.Where%s(ownerID)\n", field)
	case schema.String, schema.Text:
		fmt.Fprintf(b, "\tq.Where%s(auth.UserID)\n", field)
	default:
		fmt.Fprintf(b, "\treturn %s, graphQLAPIBadInput(%q)\n", failureReturn, operation+": unsupported owner ID type")
	}
}

func writeGraphQLAPIScopeTopLevelOwnerReadFromAuth(b *strings.Builder, tbl *schema.Table, operation, failureReturn string) {
	ownerCol := exportedGraphQLOwnerColumn(tbl)
	if ownerCol == nil {
		return
	}
	field := snakeToPascal(ownerCol.Name)
	b.WriteString("\tif auth := GraphQLAPIAuthFromContext(ctx); auth != nil && !graphQLAPIAuthCanManage(auth) {\n")
	switch ownerCol.Type {
	case schema.UUID:
		b.WriteString("\t\townerID, err := uuid.Parse(auth.UserID)\n")
		fmt.Fprintf(b, "\t\tif err != nil {\n\t\t\treturn %s, graphQLAPIBadInput(%q)\n\t\t}\n", failureReturn, operation+": invalid owner ID")
		fmt.Fprintf(b, "\t\tq.Where%s(ownerID)\n", field)
	case schema.String, schema.Text:
		fmt.Fprintf(b, "\t\tq.Where%s(auth.UserID)\n", field)
	default:
		fmt.Fprintf(b, "\t\treturn %s, graphQLAPIBadInput(%q)\n", failureReturn, operation+": unsupported owner ID type")
	}
	b.WriteString("\t}\n")
}

func writeGraphQLAPIScopeRelationshipOwnerFromAuth(b *strings.Builder, tbl *schema.Table, operation, failureReturn string) {
	ownerCol := exportedGraphQLOwnerColumn(tbl)
	if ownerCol == nil {
		return
	}
	field := snakeToPascal(ownerCol.Name)
	b.WriteString("\tauth := GraphQLAPIAuthFromContext(ctx)\n")
	fmt.Fprintf(b, "\tif auth == nil {\n\t\treturn %s, graphQLAPIUnauthenticated(%q)\n\t}\n", failureReturn, operation+": authentication required")
	b.WriteString("\tif !graphQLAPIAuthCanManage(auth) {\n")
	switch ownerCol.Type {
	case schema.UUID:
		b.WriteString("\t\townerID, err := uuid.Parse(auth.UserID)\n")
		fmt.Fprintf(b, "\t\tif err != nil {\n\t\t\treturn %s, graphQLAPIBadInput(%q)\n\t\t}\n", failureReturn, operation+": invalid owner ID")
		fmt.Fprintf(b, "\t\tq.Where%s(ownerID)\n", field)
	case schema.String, schema.Text:
		fmt.Fprintf(b, "\t\tq.Where%s(auth.UserID)\n", field)
	default:
		fmt.Fprintf(b, "\t\treturn %s, graphQLAPIBadInput(%q)\n", failureReturn, operation+": unsupported owner ID type")
	}
	b.WriteString("\t}\n")
}

func writeGraphQLAPIAssignCreateInput(b *strings.Builder, tbl *schema.Table) {
	for _, col := range tbl.Columns {
		if !graphQLAPICreateInputHasColumn(tbl, col) {
			continue
		}
		field := snakeToPascal(col.Name)
		if col.IsNullable || col.HasDefault {
			fmt.Fprintf(b, "\tif input.%s != nil {\n", field)
			writeGraphQLAPIAssignInputField(b, col, "*input."+field, true)
			b.WriteString("\t}\n")
			continue
		}
		writeGraphQLAPIAssignInputField(b, col, "input."+field, false)
	}
}

func writeGraphQLAPIAssignUpdateInput(b *strings.Builder, tbl *schema.Table) {
	for _, col := range tbl.Columns {
		if !graphQLAPIUpdateInputHasColumn(tbl, col) {
			continue
		}
		field := snakeToPascal(col.Name)
		fmt.Fprintf(b, "\tif input.%s != nil {\n", field)
		writeGraphQLAPIAssignInputField(b, col, "*input."+field, true)
		b.WriteString("\t}\n")
	}
}

func writeGraphQLAPIAssignInputField(b *strings.Builder, col *schema.Column, expr string, update bool) {
	field := snakeToPascal(col.Name)
	switch col.Type {
	case schema.UUID:
		fmt.Fprintf(b, "\t{\n\t\tvalue, err := uuid.Parse(%s)\n", expr)
		b.WriteString("\t\tif err != nil {\n\t\t\treturn nil, graphQLAPIBadInput(\"invalid GraphQL ID input\")\n\t\t}\n")
		fmt.Fprintf(b, "\t\trecord.%s = value\n\t}\n", field)
	case schema.Timestamp, schema.Date, schema.Time:
		fmt.Fprintf(b, "\t{\n\t\tvalue, err := time.Parse(time.RFC3339, %s)\n", expr)
		b.WriteString("\t\tif err != nil {\n\t\t\treturn nil, graphQLAPIBadInput(\"invalid GraphQL timestamp input\")\n\t\t}\n")
		if col.IsNullable {
			fmt.Fprintf(b, "\t\trecord.%s = &value\n", field)
		} else {
			fmt.Fprintf(b, "\t\trecord.%s = value\n", field)
		}
		b.WriteString("\t}\n")
	case schema.JSONB:
		fmt.Fprintf(b, "\tvalue := json.RawMessage(%s)\n", expr)
		fmt.Fprintf(b, "\trecord.%s = &value\n", field)
	case schema.String, schema.Text:
		if col.IsNullable && !update {
			fmt.Fprintf(b, "\trecord.%s = %s\n", field, expr)
		} else if col.IsNullable && update {
			fmt.Fprintf(b, "\tvalue := %s\n\trecord.%s = &value\n", expr, field)
		} else {
			fmt.Fprintf(b, "\trecord.%s = %s\n", field, expr)
		}
	case schema.Integer, schema.BigInteger, schema.Boolean:
		if col.IsNullable && update {
			fmt.Fprintf(b, "\tvalue := %s\n\trecord.%s = &value\n", expr, field)
		} else {
			fmt.Fprintf(b, "\trecord.%s = %s\n", field, expr)
		}
	default:
		fmt.Fprintf(b, "\trecord.%s = %s\n", field, expr)
	}
}

func graphQLAPICreateInputHasColumn(tbl *schema.Table, col *schema.Column) bool {
	if col.IsPrimaryKey || col.IsOwnerColumn || col.Name == "created_at" || col.Name == "updated_at" || isExcludedFromExportedGraphQL(tbl, col) {
		return false
	}
	return true
}

func graphQLAPIUpdateInputHasColumn(tbl *schema.Table, col *schema.Column) bool {
	if col.IsPrimaryKey || col.IsOwnerColumn || col.Name == "created_at" || col.Name == "updated_at" || isExcludedFromExportedGraphQL(tbl, col) {
		return false
	}
	return true
}

func graphQLTableHasColumn(tbl *schema.Table, name string) bool {
	for _, col := range tbl.Columns {
		if col.Name == name {
			return true
		}
	}
	return false
}

func writeGraphQLAPIFilterApplier(b *strings.Builder, tbl *schema.Table) {
	structName := tableToStruct(tbl.Name)
	if exportedGraphQLTableHasVisibility(tbl) {
		fmt.Fprintf(b, "func graphQLAPISelect%sVisibility(ctx context.Context, q *models.%sQuery) {\n", structName, structName)
		b.WriteString("\tclaims := GraphQLAPIAuthFromContext(ctx)\n")
		b.WriteString("\tif claims == nil {\n\t\tq.SelectPublic()\n\t\treturn\n\t}\n")
		b.WriteString("\tif claims.Manages || (!claims.RBACLoaded && claims.Role == \"admin\") {\n\t\tq.SelectAll()\n\t\treturn\n\t}\n")
		b.WriteString("\tq.SelectOwner()\n")
		b.WriteString("}\n\n")
	}
	if graphQLTableHasOwnerOnlyFields(tbl) {
		fmt.Fprintf(b, "func graphQLAPIScrub%sVisibility(ctx context.Context, record *models.%s) {\n", structName, structName)
		b.WriteString("\tif record == nil {\n\t\treturn\n\t}\n")
		b.WriteString("\tclaims := GraphQLAPIAuthFromContext(ctx)\n")
		b.WriteString("\tif claims == nil || claims.Manages || (!claims.RBACLoaded && claims.Role == \"admin\") {\n\t\treturn\n\t}\n")
		ownerField := "ID"
		if ownerCol := exportedGraphQLOwnerColumn(tbl); ownerCol != nil {
			ownerField = snakeToPascal(ownerCol.Name)
		}
		fmt.Fprintf(b, "\tif claims.UserID != \"\" && fmt.Sprint(record.%s) == claims.UserID {\n\t\treturn\n\t}\n", ownerField)
		for _, col := range tbl.Columns {
			if col.IsOwnerSees && !isExcludedFromExportedGraphQL(tbl, col) {
				fmt.Fprintf(b, "\tgraphQLAPISetZero(record, %q)\n", snakeToPascal(col.Name))
			}
		}
		b.WriteString("}\n\n")
	}
	fmt.Fprintf(b, "func apply%sFilter(q *models.%sQuery, filter *model.%sFilter) error {\n", structName, structName, structName)
	for _, col := range tbl.Columns {
		if isExcludedFromExportedGraphQL(tbl, col) {
			continue
		}
		field := snakeToPascal(col.Name)
		filterField := field
		b.WriteString(fmt.Sprintf("\tif filter.%s != nil {\n", filterField))
		writeGraphQLAPIColumnFilter(b, col, field)
		b.WriteString("\t}\n")
	}
	b.WriteString("\treturn nil\n")
	b.WriteString("}\n\n")
}

func writeGraphQLAPIColumnFilter(b *strings.Builder, col *schema.Column, field string) {
	switch col.Type {
	case schema.UUID:
		b.WriteString(fmt.Sprintf("\t\tif filter.%s.Eq != nil { value, err := uuid.Parse(*filter.%s.Eq); if err != nil { return graphQLAPIBadInput(\"invalid GraphQL ID filter\") }; q.Where%s(value) }\n", field, field, field))
		b.WriteString(fmt.Sprintf("\t\tif len(filter.%s.In) > maxGraphQLAPIInputListSize { return graphQLAPIBadInput(fmt.Sprintf(\"%s.in exceeds maximum %%d\", maxGraphQLAPIInputListSize)) }\n", field, col.Name))
		b.WriteString(fmt.Sprintf("\t\tif len(filter.%s.In) > 0 { values := make([]uuid.UUID, 0, len(filter.%s.In)); for _, raw := range filter.%s.In { value, err := uuid.Parse(raw); if err != nil { return graphQLAPIBadInput(\"invalid GraphQL ID filter\") }; values = append(values, value) }; q.Where%sIn(values) }\n", field, field, field, field))
	case schema.String, schema.Text:
		b.WriteString(fmt.Sprintf("\t\tif filter.%s.Eq != nil { q.Where%s(*filter.%s.Eq) }\n", field, field, field))
		b.WriteString(fmt.Sprintf("\t\tif filter.%s.Like != nil { q.Where%sLike(*filter.%s.Like) }\n", field, field, field))
		b.WriteString(fmt.Sprintf("\t\tif len(filter.%s.In) > maxGraphQLAPIInputListSize { return graphQLAPIBadInput(fmt.Sprintf(\"%s.in exceeds maximum %%d\", maxGraphQLAPIInputListSize)) }\n", field, col.Name))
		b.WriteString(fmt.Sprintf("\t\tif len(filter.%s.In) > 0 { q.Where%sIn(filter.%s.In) }\n", field, field, field))
	case schema.Integer, schema.BigInteger:
		b.WriteString(fmt.Sprintf("\t\tif filter.%s.Eq != nil { q.Where%s(*filter.%s.Eq) }\n", field, field, field))
		b.WriteString(fmt.Sprintf("\t\tif filter.%s.Gt != nil { q.Where%sGT(*filter.%s.Gt) }\n", field, field, field))
		b.WriteString(fmt.Sprintf("\t\tif filter.%s.Gte != nil { q.Where%sGTE(*filter.%s.Gte) }\n", field, field, field))
		b.WriteString(fmt.Sprintf("\t\tif filter.%s.Lt != nil { q.Where%sLT(*filter.%s.Lt) }\n", field, field, field))
		b.WriteString(fmt.Sprintf("\t\tif filter.%s.Lte != nil { q.Where%sLTE(*filter.%s.Lte) }\n", field, field, field))
		b.WriteString(fmt.Sprintf("\t\tif len(filter.%s.In) > maxGraphQLAPIInputListSize { return graphQLAPIBadInput(fmt.Sprintf(\"%s.in exceeds maximum %%d\", maxGraphQLAPIInputListSize)) }\n", field, col.Name))
		b.WriteString(fmt.Sprintf("\t\tif len(filter.%s.In) > 0 { q.Where%sIn(filter.%s.In) }\n", field, field, field))
	case schema.Timestamp, schema.Date, schema.Time:
		for _, op := range []struct{ gql, method string }{{"Gt", "After"}, {"Gte", "GTE"}, {"Lt", "Before"}, {"Lte", "LTE"}} {
			b.WriteString(fmt.Sprintf("\t\tif filter.%s.%s != nil { value, err := time.Parse(time.RFC3339, *filter.%s.%s); if err != nil { return graphQLAPIBadInput(\"invalid GraphQL timestamp filter\") }; q.Where%s%s(value) }\n", field, op.gql, field, op.gql, field, op.method))
		}
	case schema.Boolean:
		b.WriteString(fmt.Sprintf("\t\tif filter.%s.Eq != nil { q.Where%s(*filter.%s.Eq) }\n", field, field, field))
	}
}

func writeGraphQLAPIObjectResolvers(b *strings.Builder, schemaSDL string, tbl *schema.Table, tables []*schema.Table, relationships []generator.SchemaRelationship, relationshipBudgets map[string]exportedGraphQLRelationshipBudget) {
	structName := tableToStruct(tbl.Name)
	resolverType := graphQLResolverTypeName(tbl)
	for _, col := range tbl.Columns {
		returnType := graphQLColumnResolverReturnType(col)
		if returnType == "" || isExcludedFromExportedGraphQL(tbl, col) {
			continue
		}
		goField := snakeToPascal(col.Name)
		fmt.Fprintf(b, "func (r *%sResolver) %s(ctx context.Context, obj *models.%s) (%s, error) {\n", resolverType, goField, structName, returnType)
		if strings.HasPrefix(returnType, "*") {
			b.WriteString("\tif obj == nil {\n\t\treturn nil, nil\n\t}\n")
		} else {
			b.WriteString("\tif obj == nil {\n\t\treturn \"\", nil\n\t}\n")
		}
		writeGraphQLAPIFieldReturn(b, col, goField, returnType)
		b.WriteString("}\n\n")
	}
	for _, rel := range graphQLTableRelationshipsInSDL(schemaSDL, tbl, relationships) {
		child := graphQLTableByName(tables, rel.ChildTable)
		if child == nil {
			continue
		}
		writeGraphQLAPIRelationshipResolver(b, tbl, child, rel, relationshipBudgets)
	}
}

func writeGraphQLAPIRelationshipResolver(b *strings.Builder, parent, child *schema.Table, rel generator.SchemaRelationship, relationshipBudgets map[string]exportedGraphQLRelationshipBudget) {
	parentPK := primaryKeyColumn(parent)
	fk := graphQLRelationshipFKColumn(parent, child)
	if parentPK == nil || fk == nil {
		return
	}
	parentStruct := tableToStruct(parent.Name)
	childStruct := tableToStruct(child.Name)
	resolverType := graphQLResolverTypeName(parent)
	methodName := snakeToPascal(snakeToCamel(rel.ChildTable))
	parentPKField := snakeToPascal(parentPK.Name)
	fkField := snakeToPascal(fk.Name)
	relationshipField := snakeToCamel(rel.ChildTable)
	limitExpr := "maxGraphQLAPIPageSize"
	if budget, ok := relationshipBudgets[parent.Name+"."+relationshipField]; ok && budget.MaxPageSize > 0 {
		limitExpr = fmt.Sprintf("min(%d, maxGraphQLAPIPageSize)", budget.MaxPageSize)
	}
	switch rel.Type {
	case "has_one":
		fmt.Fprintf(b, "func (r *%sResolver) %s(ctx context.Context, obj *models.%s) (*models.%s, error) {\n", resolverType, methodName, parentStruct, childStruct)
		b.WriteString("\tif obj == nil {\n\t\treturn nil, nil\n\t}\n")
		fmt.Fprintf(b, "\tq := models.Query%s().Where%s(obj.%s)\n", childStruct, fkField, parentPKField)
		if exportedGraphQLTableHasVisibility(child) {
			fmt.Fprintf(b, "\tgraphQLAPISelect%sVisibility(ctx, q)\n", childStruct)
		}
		writeGraphQLAPIScopeRelationshipOwnerFromAuth(b, child, strings.ToLower(methodName), "nil")
		b.WriteString("\trecord, err := q.First()\n\tif err != nil {\n\t\tif graphQLAPIRecordNotFound(err) {\n\t\t\treturn nil, nil\n\t\t}\n\t\treturn nil, err\n\t}\n")
		if graphQLTableHasOwnerOnlyFields(child) {
			fmt.Fprintf(b, "\tgraphQLAPIScrub%sVisibility(ctx, record)\n", childStruct)
		}
		b.WriteString("\treturn record, nil\n")
		b.WriteString("}\n\n")
	default:
		fmt.Fprintf(b, "func (r *%sResolver) %s(ctx context.Context, obj *models.%s) ([]*models.%s, error) {\n", resolverType, methodName, parentStruct, childStruct)
		fmt.Fprintf(b, "\tif obj == nil {\n\t\treturn []*models.%s{}, nil\n\t}\n", childStruct)
		fmt.Fprintf(b, "\tq := models.Query%s().Where%s(obj.%s)\n", childStruct, fkField, parentPKField)
		if exportedGraphQLTableHasVisibility(child) {
			fmt.Fprintf(b, "\tgraphQLAPISelect%sVisibility(ctx, q)\n", childStruct)
		}
		writeGraphQLAPIScopeRelationshipOwnerFromAuth(b, child, strings.ToLower(methodName), "nil")
		fmt.Fprintf(b, "\trelationshipLimit := %s\n", limitExpr)
		b.WriteString("\tq.Limit(relationshipLimit + 1)\n")
		b.WriteString("\trecords, err := q.All()\n\tif err != nil {\n\t\treturn nil, err\n\t}\n")
		b.WriteString("\tif len(records) > relationshipLimit {\n\t\treturn nil, graphQLAPIBadInput(\"GraphQL relationship exceeds maximum page size; expose a paginated relationship field\")\n\t}\n")
		fmt.Fprintf(b, "\titems := make([]*models.%s, 0, len(records))\n", childStruct)
		b.WriteString("\tfor i := range records {\n")
		if graphQLTableHasOwnerOnlyFields(child) {
			fmt.Fprintf(b, "\t\tgraphQLAPIScrub%sVisibility(ctx, &records[i])\n", childStruct)
		}
		b.WriteString("\t\titems = append(items, &records[i])\n\t}\n\treturn items, nil\n")
		b.WriteString("}\n\n")
	}
}

func graphQLTableByName(tables []*schema.Table, name string) *schema.Table {
	for _, tbl := range tables {
		if tbl.Name == name {
			return tbl
		}
	}
	return nil
}

func graphQLRelationshipFKColumn(parent, child *schema.Table) *schema.Column {
	for _, col := range child.Columns {
		if col.ForeignKeyTable == parent.Name {
			return col
		}
	}
	fallback := strings.TrimSuffix(parent.Name, "s") + "_id"
	for _, col := range child.Columns {
		if col.Name == fallback {
			return col
		}
	}
	return nil
}

func writeGraphQLAPIFieldReturn(b *strings.Builder, col *schema.Column, goField, returnType string) {
	nullable := strings.HasPrefix(returnType, "*")
	if nullable {
		b.WriteString(fmt.Sprintf("\tif obj.%s == nil {\n\t\treturn nil, nil\n\t}\n", goField))
	}
	switch col.Type {
	case schema.UUID:
		if nullable {
			fmt.Fprintf(b, "\tvalue := obj.%s.String()\n\treturn &value, nil\n", goField)
		} else {
			fmt.Fprintf(b, "\treturn obj.%s.String(), nil\n", goField)
		}
	case schema.Timestamp, schema.Date, schema.Time:
		if nullable {
			fmt.Fprintf(b, "\tvalue := obj.%s.Format(time.RFC3339)\n\treturn &value, nil\n", goField)
		} else {
			fmt.Fprintf(b, "\treturn obj.%s.Format(time.RFC3339), nil\n", goField)
		}
	case schema.Decimal:
		if nullable {
			fmt.Fprintf(b, "\tvalue := obj.%s.String()\n\treturn &value, nil\n", goField)
		} else {
			fmt.Fprintf(b, "\treturn obj.%s.String(), nil\n", goField)
		}
	default:
		if nullable {
			fmt.Fprintf(b, "\tvalue := fmt.Sprint(obj.%s)\n\treturn &value, nil\n", goField)
		} else {
			fmt.Fprintf(b, "\treturn fmt.Sprint(obj.%s), nil\n", goField)
		}
	}
}

func exportedGraphQLTableHasVisibility(tbl *schema.Table) bool {
	for _, col := range tbl.Columns {
		if col.IsPublic || col.IsOwnerSees {
			return true
		}
	}
	return false
}

func graphQLTableHasOwnerOnlyFields(tbl *schema.Table) bool {
	if tbl == nil {
		return false
	}
	for _, col := range tbl.Columns {
		if col.IsOwnerSees && !isExcludedFromExportedGraphQL(tbl, col) {
			return true
		}
	}
	return false
}

func exportedGraphQLOwnerColumn(tbl *schema.Table) *schema.Column {
	if tbl == nil {
		return nil
	}
	for _, col := range tbl.Columns {
		if col.IsOwnerColumn {
			return col
		}
	}
	return nil
}

func extractStringConstFromGoSource(src []byte, name string) (string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "", src, 0)
	if err != nil {
		return "", err
	}
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST {
			continue
		}
		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, ident := range valueSpec.Names {
				if ident.Name != name {
					continue
				}
				if i >= len(valueSpec.Values) {
					return "", fmt.Errorf("const %s has no value", name)
				}
				lit, ok := valueSpec.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return "", fmt.Errorf("const %s is not a string literal", name)
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					return "", err
				}
				return value, nil
			}
		}
	}
	return "", fmt.Errorf("const %s was not found", name)
}

func (e *exporter) needsModelQuerySupport() bool {
	return e.hasGraphQLPackage() || len(e.scopes) > 0
}

func (e *exporter) generateModelQuerySupport(tables []*schema.Table, views []*schema.View, graphQLErrors bool) ([]byte, error) {
	hasEncrypted := tablesHaveEncryptedColumns(tables)
	var b strings.Builder
	b.WriteString("package models\n\n")
	b.WriteString("import (\n")
	if !graphQLErrors {
		b.WriteString("\t\"errors\"\n")
	}
	b.WriteString("\t\"strings\"\n")
	b.WriteString("\n")
	if graphQLErrors {
		b.WriteString("\t\"github.com/vektah/gqlparser/v2/gqlerror\"\n")
		b.WriteString("\n")
	}
	b.WriteString("\t\"gorm.io/gorm\"\n")
	b.WriteString(")\n\n")
	if graphQLErrors {
		b.WriteString(exportedGraphQLModelErrorHelpers)
	} else {
		b.WriteString(exportedModelErrorHelpers)
	}
	if hasEncrypted {
		if graphQLErrors {
			b.WriteString(exportedGraphQLQuerySupportHelpers)
		} else {
			b.WriteString(exportedQuerySupportHelpers)
		}
	}
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
		return nil, fmt.Errorf("formatting exported query support: %w", err)
	}
	return formatted, nil
}

func (e *exporter) generateCustomScopeSupport() ([]byte, error) {
	if len(e.scopes) == 0 {
		return nil, nil
	}
	imports := map[string]string{}
	var models []string
	for modelDir := range e.scopes {
		models = append(models, modelDir)
	}
	sort.Strings(models)

	scopeNames := map[string]map[string]string{}
	for _, modelDir := range models {
		scopeNames[modelDir] = map[string]string{}
		for _, scope := range e.scopes[modelDir] {
			scopeNames[modelDir][scope.Name] = exportedScopeHelperName(modelDir, scope.Name)
		}
	}

	var wrappers []string
	var funcs []string
	for _, modelDir := range models {
		tableName := modelDir + "s"
		scopeBuilderType := tableToStruct(tableName) + "ScopeBuilder"
		scopes := append([]generator.ScopeDef(nil), e.scopes[modelDir]...)
		sort.Slice(scopes, func(i, j int) bool {
			if scopes[i].SourceFile == scopes[j].SourceFile {
				return scopes[i].Name < scopes[j].Name
			}
			return scopes[i].SourceFile < scopes[j].SourceFile
		})
		for _, scope := range scopes {
			src, err := e.exportedScopeFunction(modelDir, scopeBuilderType, scope, scopeNames[modelDir], imports)
			if err != nil {
				return nil, err
			}
			funcs = append(funcs, src)
			var paramSig, paramCall string
			if len(scope.ExtraParams) > 0 {
				var sigs, calls []string
				for _, p := range scope.ExtraParams {
					sigs = append(sigs, fmt.Sprintf("%s %s", p.Name, exportedScopeType(p.Type)))
					calls = append(calls, p.Name)
				}
				paramSig = strings.Join(sigs, ", ")
				paramCall = ", " + strings.Join(calls, ", ")
			}
			queryType := tableToStruct(tableName) + "Query"
			var wrapper strings.Builder
			fmt.Fprintf(&wrapper, "func (q *%s) %s(%s) *%s {\n", queryType, scope.Name, paramSig, queryType)
			fmt.Fprintf(&wrapper, "\treturn q.ApplyScope(%s(q.ToScopeBuilder()%s))\n", exportedScopeHelperName(modelDir, scope.Name), paramCall)
			wrapper.WriteString("}\n\n")
			wrappers = append(wrappers, wrapper.String())
		}
	}

	var b strings.Builder
	b.WriteString("// Code generated by Pickle. DO NOT EDIT.\n")
	b.WriteString("package models\n\n")
	if len(imports) > 0 {
		var keys []string
		for key := range imports {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var importBlock strings.Builder
		importBlock.WriteString("import (\n")
		for _, key := range keys {
			namePath := strings.SplitN(key, "\x00", 2)
			if namePath[0] != "" {
				fmt.Fprintf(&importBlock, "\t%s %q\n", namePath[0], namePath[1])
			} else {
				fmt.Fprintf(&importBlock, "\t%q\n", namePath[1])
			}
		}
		importBlock.WriteString(")\n\n")
		b.WriteString(importBlock.String())
	}
	for _, wrapper := range wrappers {
		b.WriteString(wrapper)
	}
	for _, fn := range funcs {
		b.WriteString(fn)
		b.WriteString("\n")
	}
	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("formatting exported custom scope support: %w\n%s", err, b.String())
	}
	return formatted, nil
}

func (e *exporter) exportedScopeFunction(modelDir, scopeBuilderType string, scope generator.ScopeDef, scopeNames map[string]string, imports map[string]string) (string, error) {
	path := filepath.Join(e.project.Dir, filepath.FromSlash(scope.SourceFile))
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("parsing scope %s: %w", scope.SourceFile, err)
	}
	for _, imp := range file.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if p == e.sourceModule+"/app/models" {
			continue
		}
		if strings.HasPrefix(p, e.sourceModule+"/") {
			p = e.modulePath + strings.TrimPrefix(p, e.sourceModule)
		}
		name := ""
		if imp.Name != nil {
			name = imp.Name.Name
		}
		imports[name+"\x00"+p] = p
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != scope.Name {
			continue
		}
		rewriteScopeBody(fn.Body, scopeNames)
		body := scopeBodyString(fset, fn.Body)
		var paramSig string
		if len(scope.ExtraParams) > 0 {
			var sigs []string
			for _, p := range scope.ExtraParams {
				sigs = append(sigs, fmt.Sprintf("%s %s", p.Name, exportedScopeType(p.Type)))
			}
			paramSig = ", " + strings.Join(sigs, ", ")
		}
		var b strings.Builder
		fmt.Fprintf(&b, "func %s(q *%s%s) *%s {\n", exportedScopeHelperName(modelDir, scope.Name), scopeBuilderType, paramSig, scopeBuilderType)
		b.WriteString(body)
		if fn.Type.Results == nil || len(fn.Type.Results.List) == 0 {
			b.WriteString("\treturn q\n")
		}
		b.WriteString("}\n\n")
		return b.String(), nil
	}
	return "", fmt.Errorf("scope %s not found in %s", scope.Name, scope.SourceFile)
}

func rewriteScopeBody(body *ast.BlockStmt, scopeNames map[string]string) {
	if body == nil {
		return
	}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok {
			if helper, exists := scopeNames[id.Name]; exists {
				id.Name = helper
			}
		}
		return true
	})
}

func scopeBodyString(fset *token.FileSet, body *ast.BlockStmt) string {
	if body == nil {
		return ""
	}
	var b strings.Builder
	for _, stmt := range body.List {
		var stmtBuf bytes.Buffer
		if err := format.Node(&stmtBuf, fset, stmt); err != nil {
			continue
		}
		lines := strings.Split(strings.TrimRight(stmtBuf.String(), "\n"), "\n")
		for _, line := range lines {
			b.WriteString("\t")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func exportedScopeHelperName(modelDir, scopeName string) string {
	return lowerFirst(tableToStruct(modelDir + "_scope_" + pascalToSnake(scopeName)))
}

func exportedScopeType(t string) string {
	return strings.ReplaceAll(t, "models.", "")
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func writeGraphQLModelQuerySupport(b *strings.Builder, tableName string, columns []*schema.Column, readOnly bool) {
	structName := tableToStruct(tableName)
	queryName := structName + "Query"
	scopeBuilderName := structName + "ScopeBuilder"
	publicCols := graphQLVisibilitySelectColumns(columns, func(col *schema.Column) bool {
		return col.IsPublic
	})
	ownerCols := graphQLVisibilitySelectColumns(columns, func(col *schema.Column) bool {
		return col.IsPublic || col.IsOwnerSees
	})
	b.WriteString(fmt.Sprintf("type %s struct { db *gorm.DB }\n\n", queryName))
	b.WriteString(fmt.Sprintf("func Query%s() *%s { return &%s{db: DB.Model(&%s{})} }\n\n", structName, queryName, queryName, structName))
	b.WriteString(fmt.Sprintf("func (tx *Tx) Query%s() *%s {\n", structName, queryName))
	b.WriteString(fmt.Sprintf("\treturn &%s{db: tx.DB.Model(&%s{})}\n", queryName, structName))
	b.WriteString("}\n")
	b.WriteString(fmt.Sprintf("func (q *%s) SelectPublic() *%s { q.db = q.db.Select([]string{%s}); return q }\n", queryName, queryName, quotedStringList(publicCols)))
	b.WriteString(fmt.Sprintf("func (q *%s) SelectOwner() *%s { q.db = q.db.Select([]string{%s}); return q }\n", queryName, queryName, quotedStringList(ownerCols)))
	b.WriteString(fmt.Sprintf("func (q *%s) SelectAll() *%s { q.db = q.db.Select(\"*\"); return q }\n", queryName, queryName))
	b.WriteString(fmt.Sprintf("func (q *%s) ToScopeBuilder() *%s { return &%s{db: q.db} }\n", queryName, scopeBuilderName, scopeBuilderName))
	b.WriteString(fmt.Sprintf("func (q *%s) ApplyScope(sb *%s) *%s { if sb != nil { q.db = sb.db }; return q }\n", queryName, scopeBuilderName, queryName))
	b.WriteString(fmt.Sprintf("func (q *%s) AnyOwner() *%s { return q }\n", queryName, queryName))
	b.WriteString(fmt.Sprintf("func (q *%s) Limit(n int) *%s { q.db = q.db.Limit(n); return q }\n", queryName, queryName))
	b.WriteString(fmt.Sprintf("func (q *%s) Offset(n int) *%s { q.db = q.db.Offset(n); return q }\n", queryName, queryName))
	b.WriteString(fmt.Sprintf("func (q *%s) OrderBy(column, direction string) *%s {\n", queryName, queryName))
	b.WriteString("\tdir := strings.ToUpper(strings.TrimSpace(direction))\n")
	b.WriteString("\tif dir != \"DESC\" { dir = \"ASC\" }\n")
	b.WriteString("\tswitch column {\n")
	for _, col := range columns {
		if !graphQLSortableColumn(col) {
			continue
		}
		b.WriteString(fmt.Sprintf("\tcase %q:\n", col.Name))
		if storage := graphQLSelectColumnName(col); storage != col.Name {
			b.WriteString(fmt.Sprintf("\t\tcolumn = %q\n", storage))
		}
	}
	b.WriteString("\tdefault:\n")
	b.WriteString("\t\treturn q\n")
	b.WriteString("\t}\n")
	b.WriteString("\tq.db = q.db.Order(OrderClause(column, dir))\n")
	b.WriteString("\treturn q\n")
	b.WriteString("}\n")
	for _, col := range columns {
		fieldName := snakeToPascal(col.Name)
		for _, suffix := range []string{"", "Like", "In", "After", "Before", "GTE", "GT", "LTE", "LT", "Not", "NotIn"} {
			columnName := graphQLSelectColumnName(col)
			op := whereSuffixOperator(suffix)
			if col.IsSealed || (col.IsEncrypted && !graphQLEncryptedWhereSuffixSupported(suffix)) {
				b.WriteString(fmt.Sprintf("func (q *%s) Where%s%s(value any) *%s { q.db.AddError(graphQLModelBadInput(%q)); q.db = q.db.Where(\"1 = 0\"); return q }\n", queryName, fieldName, suffix, queryName, graphQLUnsupportedFilterMessage(col, suffix)))
				continue
			}
			if col.IsEncrypted {
				b.WriteString(fmt.Sprintf("func (q *%s) Where%s%s(value any) *%s { q.db = graphQLEncryptedWhere(q.db, %q, %q, value); return q }\n", queryName, fieldName, suffix, queryName, columnName, op))
				continue
			}
			b.WriteString(fmt.Sprintf("func (q *%s) Where%s%s(value any) *%s { q.db = q.db.Where(%q, value); return q }\n", queryName, fieldName, suffix, queryName, columnName+" "+op+" ?"))
		}
	}
	b.WriteString(fmt.Sprintf("type %s struct { db *gorm.DB }\n\n", scopeBuilderName))
	b.WriteString(fmt.Sprintf("func (sb *%s) SelectPublic() *%s { sb.db = sb.db.Select([]string{%s}); return sb }\n", scopeBuilderName, scopeBuilderName, quotedStringList(publicCols)))
	b.WriteString(fmt.Sprintf("func (sb *%s) SelectOwner() *%s { sb.db = sb.db.Select([]string{%s}); return sb }\n", scopeBuilderName, scopeBuilderName, quotedStringList(ownerCols)))
	b.WriteString(fmt.Sprintf("func (sb *%s) SelectAll() *%s { sb.db = sb.db.Select(\"*\"); return sb }\n", scopeBuilderName, scopeBuilderName))
	b.WriteString(fmt.Sprintf("func (sb *%s) Limit(n int) *%s { sb.db = sb.db.Limit(n); return sb }\n", scopeBuilderName, scopeBuilderName))
	b.WriteString(fmt.Sprintf("func (sb *%s) Offset(n int) *%s { sb.db = sb.db.Offset(n); return sb }\n", scopeBuilderName, scopeBuilderName))
	b.WriteString(fmt.Sprintf("func (sb *%s) OrderBy(column, direction string) *%s {\n", scopeBuilderName, scopeBuilderName))
	b.WriteString("\tdir := strings.ToUpper(strings.TrimSpace(direction))\n")
	b.WriteString("\tif dir != \"DESC\" { dir = \"ASC\" }\n")
	b.WriteString("\tswitch column {\n")
	for _, col := range columns {
		if !graphQLSortableColumn(col) {
			continue
		}
		b.WriteString(fmt.Sprintf("\tcase %q:\n", col.Name))
		if storage := graphQLSelectColumnName(col); storage != col.Name {
			b.WriteString(fmt.Sprintf("\t\tcolumn = %q\n", storage))
		}
	}
	b.WriteString("\tdefault:\n")
	b.WriteString("\t\treturn sb\n")
	b.WriteString("\t}\n")
	b.WriteString("\tsb.db = sb.db.Order(OrderClause(column, dir))\n")
	b.WriteString("\treturn sb\n")
	b.WriteString("}\n")
	for _, col := range columns {
		fieldName := snakeToPascal(col.Name)
		for _, suffix := range []string{"", "Like", "In", "After", "Before", "GTE", "GT", "LTE", "LT", "Not", "NotIn"} {
			columnName := graphQLSelectColumnName(col)
			op := whereSuffixOperator(suffix)
			if col.IsSealed || (col.IsEncrypted && !graphQLEncryptedWhereSuffixSupported(suffix)) {
				b.WriteString(fmt.Sprintf("func (sb *%s) Where%s%s(value any) *%s { sb.db.AddError(graphQLModelBadInput(%q)); sb.db = sb.db.Where(\"1 = 0\"); return sb }\n", scopeBuilderName, fieldName, suffix, scopeBuilderName, graphQLUnsupportedFilterMessage(col, suffix)))
				continue
			}
			if col.IsEncrypted {
				b.WriteString(fmt.Sprintf("func (sb *%s) Where%s%s(value any) *%s { sb.db = graphQLEncryptedWhere(sb.db, %q, %q, value); return sb }\n", scopeBuilderName, fieldName, suffix, scopeBuilderName, columnName, op))
				continue
			}
			b.WriteString(fmt.Sprintf("func (sb *%s) Where%s%s(value any) *%s { sb.db = sb.db.Where(%q, value); return sb }\n", scopeBuilderName, fieldName, suffix, scopeBuilderName, columnName+" "+op+" ?"))
		}
	}
	b.WriteString(fmt.Sprintf("func (q *%s) First() (*%s, error) { var record %s; err := q.db.First(&record).Error; return &record, err }\n", queryName, structName, structName))
	b.WriteString(fmt.Sprintf("func (q *%s) All() ([]%s, error) { var records []%s; err := q.db.Find(&records).Error; return records, err }\n", queryName, structName, structName))
	b.WriteString(fmt.Sprintf("func (q *%s) Count() (int64, error) { var count int64; err := q.db.Count(&count).Error; return count, err }\n", queryName))
	if !readOnly {
		b.WriteString(fmt.Sprintf("func (q *%s) Create(record *%s) error { return q.db.Session(&gorm.Session{NewDB: true}).Create(record).Error }\n", queryName, structName))
		b.WriteString(fmt.Sprintf("func (q *%s) Update(record *%s) error { return q.db.Session(&gorm.Session{NewDB: true}).Save(record).Error }\n", queryName, structName))
		b.WriteString(fmt.Sprintf("func (q *%s) Delete(record *%s) error { return q.db.Session(&gorm.Session{NewDB: true}).Delete(record).Error }\n", queryName, structName))
	}
	b.WriteByte('\n')
}

func graphQLSortableColumn(col *schema.Column) bool {
	if col == nil {
		return false
	}
	return !col.IsEncrypted && !col.IsSealed
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

func graphQLEncryptedWhereSuffixSupported(suffix string) bool {
	switch suffix {
	case "", "In", "Not", "NotIn":
		return true
	default:
		return false
	}
}

func graphQLUnsupportedFilterMessage(col *schema.Column, suffix string) string {
	if col.IsSealed {
		return fmt.Sprintf("sealed column %s cannot be filtered", col.Name)
	}
	return fmt.Sprintf("encrypted column %s does not support %s filters", col.Name, strings.TrimPrefix(suffix, "Where"))
}

const exportedGraphQLModelErrorHelpers = `func graphQLModelBadInput(message string) *gqlerror.Error {
	return &gqlerror.Error{
		Message:    message,
		Extensions: map[string]any{"code": "BAD_USER_INPUT"},
	}
}

`

const exportedModelErrorHelpers = `func graphQLModelBadInput(message string) error {
	return errors.New(message)
}

`

const exportedGraphQLQuerySupportHelpers = `func graphQLEncryptedWhere(db *gorm.DB, column, op string, value any) *gorm.DB {
	encryptedValue, err := graphQLEncryptFilterValue(value)
	if err != nil {
		db.AddError(err)
		return db.Where("1 = 0")
	}
	return db.Where(column+" "+op+" ?", encryptedValue)
}

func graphQLEncryptFilterValue(value any) (any, error) {
	key, err := exportedEncryptionKey()
	if err != nil {
		return nil, err
	}
	switch v := value.(type) {
	case string:
		return encryptDeterministic(key, []byte(v))
	case *string:
		if v == nil {
			return nil, nil
		}
		return encryptDeterministic(key, []byte(*v))
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			encrypted, err := encryptDeterministic(key, []byte(item))
			if err != nil {
				return nil, err
			}
			out = append(out, encrypted)
		}
		return out, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, graphQLModelBadInput("encrypted GraphQL filter values must be strings")
			}
			encrypted, err := encryptDeterministic(key, []byte(s))
			if err != nil {
				return nil, err
			}
			out = append(out, encrypted)
		}
		return out, nil
	default:
		return nil, graphQLModelBadInput("encrypted GraphQL filter value must be a string or []string")
	}
}

`

const exportedQuerySupportHelpers = `func graphQLEncryptedWhere(db *gorm.DB, column, op string, value any) *gorm.DB {
	encryptedValue, err := graphQLEncryptFilterValue(value)
	if err != nil {
		db.AddError(err)
		return db.Where("1 = 0")
	}
	return db.Where(column+" "+op+" ?", encryptedValue)
}

func graphQLEncryptFilterValue(value any) (any, error) {
	return EncryptDeterministicFilterValue(value)
}

`

const exportedGraphQLAPIHandlerSource = `// Code generated by Pickle. DO NOT EDIT.
package graphqlapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"reflect"
	"strings"

	"github.com/vektah/gqlparser/v2"
	gqlgen "github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"github.com/vektah/gqlparser/v2/parser"

	"%s/app/graphqlapi/generated"
	"%s/app/graphqlapi/resolver"
	appauth "%s/app/http/auth"
	appmodels "%s/app/models"
)

const maxGraphQLAPIRequestBodyBytes = 1 << 20
const maxGraphQLAPIRequestEnvelopeFieldBytes = 32
const maxGraphQLAPIQueryBytes = 64 << 10
const maxGraphQLAPIOperationNameBytes = 256
const maxGraphQLAPIVariables = 64
const maxGraphQLAPIVariableNameBytes = 256
const maxGraphQLAPIVariableDepth = 8
const maxGraphQLAPIVariableCollectionItems = 256
const maxGraphQLAPIVariableStringBytes = 4096
const maxGraphQLAPITokenMultiplier = 20
const maxGraphQLAPIComplexity = 1000
const maxGraphQLAPIDepth = 10
const maxGraphQLAPIRelationshipDepth = 3
const maxGraphQLAPIFields = 200
const maxGraphQLAPIAliases = 25
const maxGraphQLAPIInputNodes = 500
const maxGraphQLAPIOperations = 1

type graphQLAPIRoleRow struct {
	Slug    string
	Manages bool
}

func PlaygroundHandler(endpoint string) http.Handler {
	return playground.Handler("GraphQL", endpoint)
}

func Handler() http.Handler {
	execSchema := generated.NewExecutableSchema(generated.Config{
		Resolvers: &resolver.Resolver{},
		Complexity: graphQLAPIComplexityRoot(),
		Directives: generated.DirectiveRoot{
			Auth:        graphQLAPIAuthDirective,
			Public:      graphQLAPIPublicDirective,
			OwnerOnly:   graphQLAPIOwnerOnlyDirective,
			RequireRole: graphQLAPIRequireRoleDirective,
		},
	})
	srv := handler.New(execSchema)
	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.POST{})
	srv.SetQueryCache(lru.New[*ast.QueryDocument](1000))
	srv.SetParserTokenLimit(maxGraphQLAPIQueryBytes / maxGraphQLAPITokenMultiplier)
	srv.Use(extension.FixedComplexityLimit(maxGraphQLAPIComplexity))
	srv.SetRecoverFunc(graphQLAPIRecover)
	srv.SetErrorPresenter(graphQLAPIErrorPresenter)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r == nil {
			writeGraphQLAPIHTTPStatusError(w, http.StatusBadRequest, "invalid GraphQL request", "BAD_USER_INPUT")
			return
		}
		if r.Method != http.MethodPost && r.Method != http.MethodOptions {
			writeGraphQLAPIHTTPStatusError(w, http.StatusMethodNotAllowed, "GraphQL endpoint accepts POST requests", "BAD_USER_INPUT")
			return
		}
		if r.Method == http.MethodPost {
			if !prepareGraphQLAPIPostBody(w, r, execSchema.Schema()) {
				return
			}
			claims, err := extractGraphQLAPIAuth(r)
			if err != nil {
				log.Printf("graphqlapi auth failed")
				writeGraphQLAPIHTTPError(w, "unauthenticated", "UNAUTHENTICATED")
				return
			}
			r = r.WithContext(resolver.WithGraphQLAPIAuthClaims(r.Context(), claims))
		}
		srv.ServeHTTP(graphQLAPIStatusWriter{ResponseWriter: w}, r)
	})
}

func graphQLAPIAuthDirective(ctx context.Context, obj any, next gqlgen.Resolver) (any, error) {
	if resolver.GraphQLAPIAuthFromContext(ctx) == nil {
		return nil, graphQLAPICodedError("unauthenticated", "UNAUTHENTICATED")
	}
	if next == nil {
		return nil, graphQLAPICodedError("internal server error", "INTERNAL_SERVER_ERROR")
	}
	return next(ctx)
}

func graphQLAPIPublicDirective(ctx context.Context, obj any, next gqlgen.Resolver) (any, error) {
	if next == nil {
		return nil, graphQLAPICodedError("internal server error", "INTERNAL_SERVER_ERROR")
	}
	return next(ctx)
}

func graphQLAPIOwnerOnlyDirective(ctx context.Context, obj any, next gqlgen.Resolver) (any, error) {
	auth := resolver.GraphQLAPIAuthFromContext(ctx)
	if auth == nil {
		return nil, graphQLAPICodedError("unauthenticated", "UNAUTHENTICATED")
	}
	if !graphQLAPICanSeeOwnerObject(auth, obj) {
		return nil, nil
	}
	if next == nil {
		return nil, graphQLAPICodedError("internal server error", "INTERNAL_SERVER_ERROR")
	}
	return next(ctx)
}

func graphQLAPIRequireRoleDirective(ctx context.Context, obj any, next gqlgen.Resolver, roles []string) (any, error) {
	auth := resolver.GraphQLAPIAuthFromContext(ctx)
	if auth == nil {
		return nil, graphQLAPICodedError("unauthenticated", "UNAUTHENTICATED")
	}
	for _, role := range roles {
		if graphQLAPIHasRole(auth, role) {
			if next == nil {
				return nil, graphQLAPICodedError("internal server error", "INTERNAL_SERVER_ERROR")
			}
			return next(ctx)
		}
	}
	return nil, graphQLAPICodedError("forbidden", "FORBIDDEN")
}

func graphQLAPICanSeeOwnerObject(auth *resolver.GraphQLAPIAuthClaims, obj any) bool {
	if auth == nil {
		return false
	}
	if auth.Manages || (!auth.RBACLoaded && auth.Role == "admin") {
		return true
	}
	ownerID, ok := graphQLAPIOwnerID(obj)
	if !ok {
		return true
	}
	return ownerID != "" && auth.UserID != "" && ownerID == auth.UserID
}

func graphQLAPIOwnerID(obj any) (string, bool) {
	value := reflect.ValueOf(obj)
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return "", false
		}
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return "", false
	}
	for _, fieldName := range []string{"UserID", "OwnerID"} {
		field := value.FieldByName(fieldName)
		if !field.IsValid() {
			continue
		}
		return graphQLAPIFieldString(field), true
	}
	field := value.FieldByName("ID")
	if field.IsValid() {
		return graphQLAPIFieldString(field), true
	}
	return "", false
}

func graphQLAPIFieldString(value reflect.Value) string {
	if !value.IsValid() {
		return ""
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return ""
		}
		value = value.Elem()
	}
	if value.CanInterface() {
		if stringer, ok := value.Interface().(fmt.Stringer); ok {
			return stringer.String()
		}
	}
	switch value.Kind() {
	case reflect.String:
		return value.String()
	default:
		if value.CanInterface() {
			return fmt.Sprint(value.Interface())
		}
		return ""
	}
}

func extractGraphQLAPIAuth(r *http.Request) (*resolver.GraphQLAPIAuthClaims, error) {
	if r == nil || r.Header.Get("Authorization") == "" {
		return nil, nil
	}
	info, err := appauth.Authenticate(r)
	if err != nil {
		return nil, err
	}
	claims := &resolver.GraphQLAPIAuthClaims{UserID: info.UserID, Role: info.Role}
	if err := loadGraphQLAPIRBACClaims(claims); err != nil {
		return nil, err
	}
	return claims, nil
}

func graphQLAPIHasRole(claims *resolver.GraphQLAPIAuthClaims, role string) bool {
	if claims == nil {
		return false
	}
	for _, assigned := range claims.Roles {
		if assigned == role {
			return true
		}
	}
	return !claims.RBACLoaded && claims.Role == role
}

func loadGraphQLAPIRBACClaims(claims *resolver.GraphQLAPIAuthClaims) error {
	if claims == nil || claims.UserID == "" {
		return nil
	}
	if appmodels.DB == nil {
		return errors.New("graphql rbac database unavailable")
	}
	var roles []graphQLAPIRoleRow
	err := appmodels.DB.Raw("SELECT r.slug, r.manages FROM roles r JOIN role_user ru ON ru.role_id = r.id WHERE ru.user_id = ?", claims.UserID).Scan(&roles).Error
	if err != nil {
		if isMissingGraphQLAPIRBACTableError(err) {
			return handleMissingGraphQLAPIRBACSchema()
		}
		return err
	}
	claims.RBACLoaded = true
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

func handleMissingGraphQLAPIRBACSchema() error {
	rolesExists, err := graphQLAPIRBACTableExists("roles")
	if err != nil {
		return err
	}
	roleUserExists, err := graphQLAPIRBACTableExists("role_user")
	if err != nil {
		return err
	}
	if !rolesExists && !roleUserExists {
		return nil
	}
	return errors.New("graphql rbac schema incomplete")
}

func graphQLAPIRBACTableExists(table string) (bool, error) {
	switch table {
	case "roles", "role_user":
	default:
		return false, errors.New("graphql rbac schema check rejected")
	}
	err := appmodels.DB.Exec("SELECT 1 FROM " + table + " LIMIT 0").Error
	if err == nil {
		return true, nil
	}
	if isMissingGraphQLAPIRBACTableError(err) {
		return false, nil
	}
	return false, err
}

func isMissingGraphQLAPIRBACTableError(err error) bool {
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

func graphQLAPIRecover(_ context.Context, _ any) error {
	log.Printf("graphqlapi panic recovered")
	return graphQLAPICodedError("internal server error", "INTERNAL_SERVER_ERROR")
}

func graphQLAPIErrorPresenter(ctx context.Context, err error) *gqlerror.Error {
	presented := gqlgen.DefaultErrorPresenter(ctx, err)
	if presented == nil {
		return nil
	}
	if presented.Extensions == nil || presented.Extensions["code"] == nil {
		return graphQLAPICodedError("internal server error", "INTERNAL_SERVER_ERROR")
	}
	if presented.Message == "internal server error" || presented.Extensions["code"] == "INTERNAL_SERVER_ERROR" {
		presented.Message = "internal server error"
		presented.Extensions["code"] = "INTERNAL_SERVER_ERROR"
	}
	return presented
}

func prepareGraphQLAPIPostBody(w http.ResponseWriter, r *http.Request, schema *ast.Schema) bool {
	if !isGraphQLAPIJSONContentType(r.Header.Get("Content-Type")) {
		writeGraphQLAPIHTTPStatusError(w, http.StatusUnsupportedMediaType, "GraphQL POST requests require application/json", "BAD_USER_INPUT")
		return false
	}
	if r.ContentLength > maxGraphQLAPIRequestBodyBytes {
		writeGraphQLAPIHTTPStatusError(w, http.StatusRequestEntityTooLarge, "graphql request body too large", "BAD_USER_INPUT")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxGraphQLAPIRequestBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeGraphQLAPIHTTPStatusError(w, http.StatusRequestEntityTooLarge, "graphql request body too large", "BAD_USER_INPUT")
			return false
		}
		writeGraphQLAPIHTTPError(w, "invalid GraphQL request body", "BAD_USER_INPUT")
		return false
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		writeGraphQLAPIHTTPError(w, "invalid GraphQL request body", "BAD_USER_INPUT")
		return false
	}
	if trimmed[0] == '[' {
		writeGraphQLAPIHTTPError(w, "batched GraphQL requests are not supported", "BAD_USER_INPUT")
		return false
	}
	if !validateGraphQLAPIRequestEnvelopeFieldUniqueness(w, trimmed) {
		return false
	}
	var raw map[string]any
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		writeGraphQLAPIHTTPError(w, "invalid GraphQL request body", "BAD_USER_INPUT")
		return false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeGraphQLAPIHTTPError(w, "invalid GraphQL request body", "BAD_USER_INPUT")
		return false
	}
	if !validateGraphQLAPIRequestEnvelope(w, raw, schema) {
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return true
}

func validateGraphQLAPIRequestEnvelopeFieldUniqueness(w http.ResponseWriter, body []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		writeGraphQLAPIHTTPError(w, "invalid GraphQL request body", "BAD_USER_INPUT")
		return false
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		writeGraphQLAPIHTTPError(w, "invalid GraphQL request body", "BAD_USER_INPUT")
		return false
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			writeGraphQLAPIHTTPError(w, "invalid GraphQL request body", "BAD_USER_INPUT")
			return false
		}
		field, ok := token.(string)
		if !ok {
			writeGraphQLAPIHTTPError(w, "invalid GraphQL request body", "BAD_USER_INPUT")
			return false
		}
		if seen[field] {
			writeGraphQLAPIHTTPError(w, "GraphQL request contains duplicate field", "BAD_USER_INPUT")
			return false
		}
		seen[field] = true
		var discard any
		if err := decoder.Decode(&discard); err != nil {
			writeGraphQLAPIHTTPError(w, "invalid GraphQL request body", "BAD_USER_INPUT")
			return false
		}
	}
	return true
}

func validateGraphQLAPIRequestEnvelope(w http.ResponseWriter, raw map[string]any, schema *ast.Schema) bool {
	if !validateGraphQLAPIRequestEnvelopeFields(w, raw) {
		return false
	}
	query, ok := raw["query"].(string)
	if !ok || query == "" {
		writeGraphQLAPIHTTPError(w, "GraphQL query must be a string", "BAD_USER_INPUT")
		return false
	}
	if len(query) > maxGraphQLAPIQueryBytes {
		writeGraphQLAPIHTTPError(w, "GraphQL query is too large", "BAD_USER_INPUT")
		return false
	}
	if graphQLAPIQueryRequestsIntrospection(query) {
		writeGraphQLAPIHTTPError(w, "GraphQL introspection is disabled", "BAD_USER_INPUT")
		return false
	}
	if err := validateGraphQLAPIQueryShape(schema, query); err != nil {
		writeGraphQLAPIHTTPError(w, err.Error(), "BAD_USER_INPUT")
		return false
	}
	if operationName, ok := raw["operationName"]; ok {
		name, ok := operationName.(string)
		if operationName != nil && !ok {
			writeGraphQLAPIHTTPError(w, "GraphQL operationName must be a string", "BAD_USER_INPUT")
			return false
		}
		if len(name) > maxGraphQLAPIOperationNameBytes {
			writeGraphQLAPIHTTPError(w, "GraphQL operationName is too large", "BAD_USER_INPUT")
			return false
		}
		if name != "" && !isGraphQLAPIName(name) {
			writeGraphQLAPIHTTPError(w, "GraphQL operationName is invalid", "BAD_USER_INPUT")
			return false
		}
	}
	if variables, ok := raw["variables"]; ok && variables != nil {
		values, ok := variables.(map[string]any)
		if !ok {
			writeGraphQLAPIHTTPError(w, "GraphQL variables must be an object", "BAD_USER_INPUT")
			return false
		}
		if err := validateGraphQLAPIVariables(values); err != nil {
			writeGraphQLAPIHTTPError(w, err.Error(), "BAD_USER_INPUT")
			return false
		}
	}
	if extensions, ok := raw["extensions"]; ok && extensions != nil {
		values, ok := extensions.(map[string]any)
		if !ok {
			writeGraphQLAPIHTTPError(w, "GraphQL extensions must be an object", "BAD_USER_INPUT")
			return false
		}
		if err := validateGraphQLAPIExtensions(values); err != nil {
			writeGraphQLAPIHTTPError(w, err.Error(), "BAD_USER_INPUT")
			return false
		}
	}
	return true
}

func validateGraphQLAPIRequestEnvelopeFields(w http.ResponseWriter, raw map[string]any) bool {
	for field := range raw {
		if len(field) > maxGraphQLAPIRequestEnvelopeFieldBytes {
			writeGraphQLAPIHTTPError(w, "GraphQL request field is too large", "BAD_USER_INPUT")
			return false
		}
		switch field {
		case "query", "operationName", "variables", "extensions":
			continue
		default:
			writeGraphQLAPIHTTPError(w, "GraphQL request contains unsupported field", "BAD_USER_INPUT")
			return false
		}
	}
	return true
}

func graphQLAPIQueryRequestsIntrospection(query string) bool {
	return strings.Contains(query, "__schema") || strings.Contains(query, "__type")
}

type graphQLAPIQueryShape struct {
	Depth             int
	Fields            int
	Aliases           int
	InputNodes        int
	InputDepth        int
	Variables         int
	RelationshipDepth int
}

func validateGraphQLAPIQueryShape(schema *ast.Schema, query string) error {
	doc, err := parser.ParseQuery(&ast.Source{Input: query})
	if err != nil {
		return graphQLAPICodedError("invalid GraphQL query", "BAD_USER_INPUT")
	}
	fragments := map[string]*ast.FragmentDefinition{}
	for _, fragment := range doc.Fragments {
		if fragment != nil {
			fragments[fragment.Name] = fragment
		}
	}
	seenFragments := map[string]bool{}
	shape := graphQLAPIQueryShape{}
	operations := 0
	for _, operation := range doc.Operations {
		if operation == nil {
			continue
		}
		operations++
		variableShape := graphQLAPIVariableDefinitionShape(operation.VariableDefinitions)
		graphQLAPIAddShape(&variableShape, graphQLAPIDirectiveInputShape(operation.Directives))
		if variableShape.Depth > shape.Depth {
			shape.Depth = variableShape.Depth
		}
		shape.Variables += variableShape.Variables
		shape.InputNodes += variableShape.InputNodes
		if variableShape.InputDepth > shape.InputDepth {
			shape.InputDepth = variableShape.InputDepth
		}
		operationShape := graphQLAPISelectionShape(schema, graphQLAPIRootDefinition(schema, operation.Operation), operation.SelectionSet, fragments, seenFragments, 1, 0)
		if operationShape.Depth > shape.Depth {
			shape.Depth = operationShape.Depth
		}
		shape.Fields += operationShape.Fields
		shape.Aliases += operationShape.Aliases
		shape.InputNodes += operationShape.InputNodes
		if operationShape.RelationshipDepth > shape.RelationshipDepth {
			shape.RelationshipDepth = operationShape.RelationshipDepth
		}
		if operationShape.InputDepth > shape.InputDepth {
			shape.InputDepth = operationShape.InputDepth
		}
	}
	if operations > maxGraphQLAPIOperations {
		return graphQLAPICodedError("GraphQL query operations exceed safety limit", "BAD_USER_INPUT")
	}
	if shape.Variables > maxGraphQLAPIVariables {
		return graphQLAPICodedError("GraphQL variable definitions exceed safety limit", "BAD_USER_INPUT")
	}
	if shape.Depth > maxGraphQLAPIDepth {
		return graphQLAPICodedError("GraphQL query depth exceeds safety limit", "BAD_USER_INPUT")
	}
	if shape.RelationshipDepth > maxGraphQLAPIRelationshipDepth {
		return graphQLAPICodedError("GraphQL relationship depth exceeds safety limit", "BAD_USER_INPUT")
	}
	if shape.Fields > maxGraphQLAPIFields {
		return graphQLAPICodedError("GraphQL query fields exceed safety limit", "BAD_USER_INPUT")
	}
	if shape.Aliases > maxGraphQLAPIAliases {
		return graphQLAPICodedError("GraphQL query aliases exceed safety limit", "BAD_USER_INPUT")
	}
	if shape.InputNodes > maxGraphQLAPIInputNodes {
		return graphQLAPICodedError("GraphQL query inputs exceed safety limit", "BAD_USER_INPUT")
	}
	if shape.InputDepth > maxGraphQLAPIVariableDepth {
		return graphQLAPICodedError("GraphQL query inputs exceed safety limit", "BAD_USER_INPUT")
	}
	if _, errs := gqlparser.LoadQuery(schema, query); len(errs) > 0 {
		return graphQLAPICodedError("invalid GraphQL query", "BAD_USER_INPUT")
	}
	return nil
}

func graphQLAPIRootDefinition(schema *ast.Schema, operation ast.Operation) *ast.Definition {
	if schema == nil {
		return nil
	}
	switch operation {
	case ast.Mutation:
		return schema.Mutation
	case ast.Subscription:
		return schema.Subscription
	default:
		return schema.Query
	}
}

func graphQLAPISelectionShape(schema *ast.Schema, parent *ast.Definition, selections ast.SelectionSet, fragments map[string]*ast.FragmentDefinition, seenFragments map[string]bool, depth, relationshipDepth int) graphQLAPIQueryShape {
	shape := graphQLAPIQueryShape{Depth: depth}
	for _, selection := range selections {
		switch sel := selection.(type) {
		case *ast.Field:
			if sel == nil {
				continue
			}
			shape.Fields++
			if sel.Alias != "" && sel.Alias != sel.Name {
				shape.Aliases++
			}
			graphQLAPIAddShape(&shape, graphQLAPIArgumentInputShape(sel.Arguments))
			graphQLAPIAddShape(&shape, graphQLAPIDirectiveInputShape(sel.Directives))
			nextRelationshipDepth := relationshipDepth
			if parent != nil && graphQLAPIRelationshipFields[parent.Name+"."+sel.Name] {
				nextRelationshipDepth++
			}
			if nextRelationshipDepth > shape.RelationshipDepth {
				shape.RelationshipDepth = nextRelationshipDepth
			}
			child := graphQLAPISelectionShape(schema, graphQLAPIFieldDefinition(schema, parent, sel.Name), sel.SelectionSet, fragments, seenFragments, depth+1, nextRelationshipDepth)
			if child.Depth > shape.Depth {
				shape.Depth = child.Depth
			}
			shape.Fields += child.Fields
			shape.Aliases += child.Aliases
			shape.InputNodes += child.InputNodes
			if child.RelationshipDepth > shape.RelationshipDepth {
				shape.RelationshipDepth = child.RelationshipDepth
			}
			if child.InputDepth > shape.InputDepth {
				shape.InputDepth = child.InputDepth
			}
		case *ast.InlineFragment:
			if sel == nil {
				continue
			}
			graphQLAPIAddShape(&shape, graphQLAPIDirectiveInputShape(sel.Directives))
			childParent := parent
			if sel.TypeCondition != "" && schema != nil {
				childParent = schema.Types[sel.TypeCondition]
			}
			child := graphQLAPISelectionShape(schema, childParent, sel.SelectionSet, fragments, seenFragments, depth, relationshipDepth)
			if child.Depth > shape.Depth {
				shape.Depth = child.Depth
			}
			shape.Fields += child.Fields
			shape.Aliases += child.Aliases
			shape.InputNodes += child.InputNodes
			if child.RelationshipDepth > shape.RelationshipDepth {
				shape.RelationshipDepth = child.RelationshipDepth
			}
			if child.InputDepth > shape.InputDepth {
				shape.InputDepth = child.InputDepth
			}
		case *ast.FragmentSpread:
			if sel == nil || seenFragments[sel.Name] {
				continue
			}
			graphQLAPIAddShape(&shape, graphQLAPIDirectiveInputShape(sel.Directives))
			fragment := fragments[sel.Name]
			if fragment == nil {
				continue
			}
			seenFragments[sel.Name] = true
			fragmentShape := graphQLAPIVariableDefinitionShape(fragment.VariableDefinition)
			graphQLAPIAddShape(&fragmentShape, graphQLAPIDirectiveInputShape(fragment.Directives))
			if fragmentShape.Depth > shape.Depth {
				shape.Depth = fragmentShape.Depth
			}
			shape.Variables += fragmentShape.Variables
			shape.InputNodes += fragmentShape.InputNodes
			if fragmentShape.InputDepth > shape.InputDepth {
				shape.InputDepth = fragmentShape.InputDepth
			}
			childParent := parent
			if fragment.TypeCondition != "" && schema != nil {
				childParent = schema.Types[fragment.TypeCondition]
			}
			child := graphQLAPISelectionShape(schema, childParent, fragment.SelectionSet, fragments, seenFragments, depth, relationshipDepth)
			delete(seenFragments, sel.Name)
			if child.Depth > shape.Depth {
				shape.Depth = child.Depth
			}
			shape.Fields += child.Fields
			shape.Aliases += child.Aliases
			shape.InputNodes += child.InputNodes
			shape.Variables += child.Variables
			if child.RelationshipDepth > shape.RelationshipDepth {
				shape.RelationshipDepth = child.RelationshipDepth
			}
			if child.InputDepth > shape.InputDepth {
				shape.InputDepth = child.InputDepth
			}
		}
	}
	return shape
}

func graphQLAPIFieldDefinition(schema *ast.Schema, parent *ast.Definition, fieldName string) *ast.Definition {
	if schema == nil || parent == nil {
		return nil
	}
	field := parent.Fields.ForName(fieldName)
	if field == nil || field.Type == nil {
		return nil
	}
	return schema.Types[field.Type.Name()]
}

func graphQLAPIAddShape(shape *graphQLAPIQueryShape, child graphQLAPIQueryShape) {
	if child.Depth > shape.Depth {
		shape.Depth = child.Depth
	}
	if child.InputDepth > shape.InputDepth {
		shape.InputDepth = child.InputDepth
	}
	shape.Fields += child.Fields
	shape.Aliases += child.Aliases
	shape.InputNodes += child.InputNodes
	shape.Variables += child.Variables
	if child.RelationshipDepth > shape.RelationshipDepth {
		shape.RelationshipDepth = child.RelationshipDepth
	}
}

func graphQLAPIVariableDefinitionShape(definitions ast.VariableDefinitionList) graphQLAPIQueryShape {
	shape := graphQLAPIQueryShape{}
	for _, definition := range definitions {
		if definition == nil {
			continue
		}
		shape.Variables++
		graphQLAPIAddShape(&shape, graphQLAPIValueInputShape(definition.DefaultValue, 0))
		graphQLAPIAddShape(&shape, graphQLAPIDirectiveInputShape(definition.Directives))
	}
	return shape
}

func graphQLAPIArgumentInputShape(arguments ast.ArgumentList) graphQLAPIQueryShape {
	shape := graphQLAPIQueryShape{}
	for _, argument := range arguments {
		if argument == nil {
			continue
		}
		graphQLAPIAddShape(&shape, graphQLAPIValueInputShape(argument.Value, 0))
	}
	return shape
}

func graphQLAPIDirectiveInputShape(directives ast.DirectiveList) graphQLAPIQueryShape {
	shape := graphQLAPIQueryShape{}
	for _, directive := range directives {
		if directive == nil {
			continue
		}
		graphQLAPIAddShape(&shape, graphQLAPIArgumentInputShape(directive.Arguments))
	}
	return shape
}

func graphQLAPIValueInputShape(value *ast.Value, depth int) graphQLAPIQueryShape {
	if value == nil {
		return graphQLAPIQueryShape{}
	}
	shape := graphQLAPIQueryShape{InputNodes: 1, InputDepth: depth}
	for _, child := range value.Children {
		graphQLAPIAddShape(&shape, graphQLAPIValueInputShape(child.Value, depth+1))
	}
	return shape
}

func validateGraphQLAPIVariables(variables map[string]any) error {
	if len(variables) > maxGraphQLAPIVariables {
		return graphQLAPICodedError("too many GraphQL variables", "BAD_USER_INPUT")
	}
	for name, value := range variables {
		if len(name) > maxGraphQLAPIVariableNameBytes {
			return graphQLAPICodedError("GraphQL variable name is too large", "BAD_USER_INPUT")
		}
		if !isGraphQLAPIName(name) {
			return graphQLAPICodedError("GraphQL variable name is invalid", "BAD_USER_INPUT")
		}
		if !validGraphQLAPIVariableValue(value, 0) {
			return graphQLAPICodedError("GraphQL variables exceed safety limits", "BAD_USER_INPUT")
		}
	}
	return nil
}

func validateGraphQLAPIExtensions(extensions map[string]any) error {
	if len(extensions) > maxGraphQLAPIVariableCollectionItems {
		return graphQLAPICodedError("GraphQL extensions exceed safety limits", "BAD_USER_INPUT")
	}
	for name, value := range extensions {
		if len(name) > maxGraphQLAPIVariableNameBytes {
			return graphQLAPICodedError("GraphQL extension name is too large", "BAD_USER_INPUT")
		}
		if !validGraphQLAPIVariableValue(value, 0) {
			return graphQLAPICodedError("GraphQL extensions exceed safety limits", "BAD_USER_INPUT")
		}
	}
	return nil
}

func validGraphQLAPIVariableValue(value any, depth int) bool {
	if depth > maxGraphQLAPIVariableDepth {
		return false
	}
	switch v := value.(type) {
	case string:
		return len(v) <= maxGraphQLAPIVariableStringBytes
	case json.Number:
		return len(v.String()) <= maxGraphQLAPIVariableStringBytes
	case []any:
		if len(v) > maxGraphQLAPIVariableCollectionItems {
			return false
		}
		for _, item := range v {
			if !validGraphQLAPIVariableValue(item, depth+1) {
				return false
			}
		}
		return true
	case map[string]any:
		if len(v) > maxGraphQLAPIVariableCollectionItems {
			return false
		}
		for key, item := range v {
			if len(key) > maxGraphQLAPIVariableNameBytes {
				return false
			}
			if !validGraphQLAPIVariableValue(item, depth+1) {
				return false
			}
		}
		return true
	default:
		return true
	}
}

func isGraphQLAPIName(name string) bool {
	for i, r := range name {
		if i == 0 {
			if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				continue
			}
			return false
		}
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return name != ""
}

func isGraphQLAPIJSONContentType(contentType string) bool {
	if contentType == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == "application/json"
}

func writeGraphQLAPIHTTPError(w http.ResponseWriter, message, code string) {
	writeGraphQLAPIHTTPStatusError(w, http.StatusOK, message, code)
}

func writeGraphQLAPIHTTPStatusError(w http.ResponseWriter, status int, message, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"data": nil,
		"errors": []map[string]any{{
			"message":    message,
			"extensions": map[string]any{"code": code},
		}},
	}); err != nil {
		log.Printf("graphqlapi: failed to write error response")
	}
}

func graphQLAPICodedError(message, code string) *gqlerror.Error {
	return &gqlerror.Error{
		Message:    message,
		Extensions: map[string]any{"code": code},
	}
}

type graphQLAPIStatusWriter struct {
	http.ResponseWriter
}

func (w graphQLAPIStatusWriter) WriteHeader(status int) {
	if status == http.StatusBadRequest || status == http.StatusUnprocessableEntity {
		status = http.StatusOK
	}
	w.ResponseWriter.WriteHeader(status)
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
		return fmt.Errorf("exporting actions for %s: %w", set.Model, err)
	}
	if err := e.validateExportedActionSignatures(set); err != nil {
		return err
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

func (e *exporter) validateExportedActionSignatures(set *generator.ActionSet) error {
	if e == nil || e.project == nil || set == nil {
		return nil
	}
	modelType := tableToStruct(set.Model)
	for _, action := range set.Actions {
		if err := e.validateExportedActionSignature(action, modelType); err != nil {
			return err
		}
	}
	for _, gate := range set.Gates {
		if err := e.validateExportedGateSignature(gate, modelType); err != nil {
			return err
		}
	}
	return nil
}

func (e *exporter) validateExportedActionSignature(action generator.ActionDef, modelType string) error {
	path := filepath.Join(e.project.Dir, action.SourceFile)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return err
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != action.Name || actionReceiverTypeName(fn.Recv) != action.StructName {
			continue
		}
		if lenFieldList(fn.Type.Params) != 2 || !validExportedActionParams(fn.Type.Params, modelType) {
			return actionSignatureExportError(action.SourceFile, fset.Position(fn.Pos()).Line, action.Name)
		}
		results := fn.Type.Results
		switch lenFieldList(results) {
		case 1:
			if !isErrorType(results.List[0].Type) {
				return actionSignatureExportError(action.SourceFile, fset.Position(fn.Pos()).Line, action.Name)
			}
		case 2:
			if !isErrorType(results.List[1].Type) || isErrorType(results.List[0].Type) {
				return actionSignatureExportError(action.SourceFile, fset.Position(fn.Pos()).Line, action.Name)
			}
		default:
			return actionSignatureExportError(action.SourceFile, fset.Position(fn.Pos()).Line, action.Name)
		}
		return nil
	}
	return actionSignatureExportError(action.SourceFile, 0, action.Name)
}

func (e *exporter) validateExportedGateSignature(gate generator.GateDef, modelType string) error {
	path := filepath.Join(e.project.Dir, gate.SourceFile)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return err
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != gate.Name || fn.Recv != nil {
			continue
		}
		if lenFieldList(fn.Type.Params) != 2 || !validExportedGateParams(fn.Type.Params, modelType) || lenFieldList(fn.Type.Results) != 1 || !isUUIDPointerType(fn.Type.Results.List[0].Type) {
			return gateSignatureExportError(gate.SourceFile, fset.Position(fn.Pos()).Line, gate.Name)
		}
		return nil
	}
	return gateSignatureExportError(gate.SourceFile, 0, gate.Name)
}

func validExportedActionParams(params *ast.FieldList, modelType string) bool {
	exprs := fieldListExprs(params)
	return len(exprs) == 2 && isContextPointerType(exprs[0]) && isPointerToNamedType(exprs[1], modelType)
}

func validExportedGateParams(params *ast.FieldList, modelType string) bool {
	exprs := fieldListExprs(params)
	if len(exprs) != 2 || !isContextPointerType(exprs[0]) {
		return false
	}
	return isPointerToNamedType(exprs[1], modelType) || isOwnerIDInterfaceType(exprs[1])
}

func actionSignatureExportError(file string, line int, action string) exportError {
	return exportError{
		File:    file,
		Line:    line,
		Rule:    "action_export_unsupported_signature",
		Message: fmt.Sprintf("action %s has a signature that cannot be lowered safely", action),
	}
}

func gateSignatureExportError(file string, line int, gate string) exportError {
	return exportError{
		File:    file,
		Line:    line,
		Rule:    "gate_export_unsupported_signature",
		Message: fmt.Sprintf("gate %s has a signature that cannot be lowered safely", gate),
	}
}

func lenFieldList(list *ast.FieldList) int {
	if list == nil {
		return 0
	}
	total := 0
	for _, field := range list.List {
		if len(field.Names) == 0 {
			total++
			continue
		}
		total += len(field.Names)
	}
	return total
}

func fieldListExprs(list *ast.FieldList) []ast.Expr {
	if list == nil {
		return nil
	}
	var exprs []ast.Expr
	for _, field := range list.List {
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		for i := 0; i < count; i++ {
			exprs = append(exprs, field.Type)
		}
	}
	return exprs
}

func isErrorType(expr ast.Expr) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == "error"
}

func isContextPointerType(expr ast.Expr) bool {
	return isPointerToNamedType(expr, "Context")
}

func isPointerToNamedType(expr ast.Expr, name string) bool {
	ptr, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	return exprNamedType(ptr.X) == name
}

func exprNamedType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	default:
		return ""
	}
}

func isOwnerIDInterfaceType(expr ast.Expr) bool {
	iface, ok := expr.(*ast.InterfaceType)
	if !ok || iface.Methods == nil || len(iface.Methods.List) != 1 {
		return false
	}
	method := iface.Methods.List[0]
	if len(method.Names) != 1 || method.Names[0].Name != "OwnerID" {
		return false
	}
	fn, ok := method.Type.(*ast.FuncType)
	if !ok || lenFieldList(fn.Params) != 0 || lenFieldList(fn.Results) != 1 {
		return false
	}
	return exprNamedType(fn.Results.List[0].Type) == "string"
}

func isUUIDPointerType(expr ast.Expr) bool {
	ptr, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	switch t := ptr.X.(type) {
	case *ast.SelectorExpr:
		return t.Sel.Name == "UUID"
	case *ast.Ident:
		return t.Name == "UUID"
	default:
		return false
	}
}

func actionReceiverTypeName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	switch t := recv.List[0].Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
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
		var exportErr exportError
		if errors.As(err, &exportErr) && exportErr.Rule == "query_export_unsupported" {
			return actionQueryExportError(sourceFile, exportErr.Line, exportErr.Message)
		}
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

	"%s/internal/httpx"
)

type Context = httpx.Context

var ErrUnauthorized = errors.New("unauthorized")

type AuditFunc func(ctx *httpx.Context, action, model string, resourceID any, extra string)

var OnAuditPerformed AuditFunc
var OnAuditDenied AuditFunc
var OnAuditFailed AuditFunc

func sanitizedAuditFailureReason(_ error) string {
	return "action failed"
}

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
		OnAuditFailed(ctx, action, model, resourceID, sanitizedAuditFailureReason(err))
		return
	}
	log.Printf("audit.failed user_id=%%s roles=%%v action=%%s model=%%s resource_id=%%v error=action failed ip=%%s request_id=%%s",
		auditUserID(ctx), auditRoles(ctx), action, model, resourceID, auditContextIP(ctx), auditContextRequestID(ctx))
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
	if ctx == nil {
		return ""
	}
	return ctx.ClientIP()
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
	b.WriteString("\t\"github.com/google/uuid\"\n")
	b.WriteString(fmt.Sprintf("\t\"%s/internal/httpx\"\n", e.modulePath))
	b.WriteString(")\n\n")
	seenGates := map[string]bool{}
	for _, gate := range set.Gates {
		if seenGates[gate.ActionName] {
			continue
		}
		seenGates[gate.ActionName] = true
		fmt.Fprintf(&b, "func Authorize%s(ctx *httpx.Context, m *%s) (*uuid.UUID, error) {\n", gate.ActionName, structName)
		fmt.Fprintf(&b, "\troleID := Can%s(ctx, m)\n", gate.ActionName)
		b.WriteString("\tif roleID == nil {\n\t\treturn nil, ErrUnauthorized\n\t}\n")
		b.WriteString("\treturn roleID, nil\n")
		b.WriteString("}\n\n")
	}
	for _, action := range set.Actions {
		resultType := action.ResultType
		b.WriteString(fmt.Sprintf("func (m *%s) %s(ctx *httpx.Context, action %s) ", structName, action.Name, action.StructName))
		if action.HasResult {
			b.WriteString(fmt.Sprintf("(%s, error) {\n", resultType))
		} else {
			b.WriteString("error {\n")
		}
		b.WriteString(fmt.Sprintf("\troleID, err := Authorize%s(ctx, m)\n", action.Name))
		b.WriteString("\tif err != nil {\n")
		b.WriteString(fmt.Sprintf("\t\tAuditDenied(ctx, %q, %q, m.ID, \"gate denied\")\n", action.Name, structName))
		if action.HasResult {
			b.WriteString("\t\treturn nil, err\n")
		} else {
			b.WriteString("\t\treturn err\n")
		}
		b.WriteString("\t}\n")
		if action.HasResult {
			b.WriteString(fmt.Sprintf("\tvar result %s\n", resultType))
			b.WriteString(fmt.Sprintf("\terr = runAuditedAction(ctx, %q, %q, m.ID, actionVersionID(m), roleID, func() error {\n", structName, action.Name))
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
var errAuditDatabase = errors.New("audit database error")
var errAuditUserID = errors.New("audit user id")
var errAuditSeed = errors.New("audit seed error")

func auditDatabaseError() error {
	return errAuditDatabase
}

func auditUserIDError() error {
	return errAuditUserID
}

func auditSeedError() error {
	return errAuditSeed
}

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
		if err := db.Exec(stmt).Error; err != nil { return auditDatabaseError() }
	}
	for _, model := range actionAuditModelSeeds {
		if err := db.Exec(actionAuditModelTypeUpsertSQL(db), model.ID, model.Name).Error; err != nil { return auditDatabaseError() }
	}
	for _, action := range actionAuditActionSeeds {
		if err := db.Exec(actionAuditActionTypeUpsertSQL(db), action.ID, action.ModelTypeID, action.Action).Error; err != nil { return auditDatabaseError() }
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
		return auditSeedError()
	}
	actionAuditMu.Lock()
	defer actionAuditMu.Unlock()
	previous := DB
	var actionErr error
	err := previous.Transaction(func(tx *gorm.DB) error {
		DB = tx
		defer func() { DB = previous }()
		if err := ensureActionAuditSchema(tx); err != nil {
			return err
		}
		if err := fn(); err != nil {
			AuditFailed(ctx, action, model, resourceID, err)
			actionErr = err
			return err
		}
		return recordActionPerformed(tx, ctx, actionID, resourceID, resourceVersionID, roleID)
	})
	DB = previous
	if err == nil {
		AuditPerformed(ctx, action, model, resourceID)
	} else if actionErr == nil && !errors.Is(err, errAuditDatabase) && !errors.Is(err, errAuditUserID) {
		return auditDatabaseError()
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
		return auditUserIDError()
	}
	ip := auditContextIP(ctx)
	requestID := auditContextRequestID(ctx)
	if err := db.Exec("INSERT INTO user_actions (id, user_id, action_type_id, resource_id, resource_version_id, role_id, ip_address, request_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		uuid.New().String(), userID.String(), actionTypeID, resourceID.String(), nullableUUID(resourceVersionID), nullableUUID(roleID), nullableString(ip), nullableString(requestID), time.Now().UTC()).Error; err != nil {
		return auditDatabaseError()
	}
	return nil
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

func (e *exporter) exportedRouteVars() ([]string, error) {
	routeVars, err := generator.ScanRouteVars(filepath.Join(e.project.Dir, "routes"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("scanning exported route vars: %w", err)
	}
	if len(routeVars) == 0 {
		routeVars = []string{"API"}
	}
	sort.Strings(routeVars)
	return routeVars, nil
}

func exportedServiceRouteVars(svc generator.ServiceLayout) ([]string, error) {
	routeVars, err := generator.ScanRouteVars(filepath.Join(svc.Dir, "routes"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("scanning exported route vars for service %s: %w", svc.Name, err)
	}
	if len(routeVars) == 0 {
		routeVars = []string{"API"}
	}
	sort.Strings(routeVars)
	return routeVars, nil
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
	var graphQLActions []exportedGraphQLControllerAction
	if e.hasGraphQLPolicies() {
		graphQLState = generator.DeriveGraphQLStateFromDir(filepath.Join(e.project.Dir, "database", "policies", "graphql"))
		graphQLActions, _ = e.exportedGraphQLControllerActions()
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
	"errors"
	"fmt"
	"sort"
	"strings"
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

var errPolicyDatabase = errors.New("policy database error")

func policyDatabaseError() error {
	return errPolicyDatabase
}

func ensurePolicyDB(db *gorm.DB) error {
	if db == nil {
		return errors.New("policies: DB is nil")
	}
	return nil
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
	for _, action := range graphQLActions {
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
	if err := ensurePolicyDB(db); err != nil { return err }
	if err := db.Transaction(func(tx *gorm.DB) error {
		if len(roleSeeds) > 0 {
			if err := ensureRBACSchema(tx); err != nil { return err }
			if err := seedRoles(tx); err != nil { return err }
			if err := markPoliciesApplied(tx, "rbac_changelog", rolePolicyIDs); err != nil { return err }
		}
		if len(graphQLExposureSeeds) > 0 || len(graphQLActionSeeds) > 0 {
			if err := ensureGraphQLPolicySchema(tx); err != nil { return err }
			if err := seedGraphQLPolicies(tx); err != nil { return err }
			if err := markPoliciesApplied(tx, "graphql_changelog", graphQLPolicyIDs); err != nil { return err }
		}
		return nil
	}); err != nil {
		if errors.Is(err, errPolicyDatabase) {
			return err
		}
		return policyDatabaseError()
	}
	return nil
}

func Rollback(db *gorm.DB, driver string) error {
	if err := ensurePolicyDB(db); err != nil { return err }
	if err := db.Transaction(func(tx *gorm.DB) error {
		if len(graphQLExposureSeeds) > 0 || len(graphQLActionSeeds) > 0 {
			if err := tx.Exec("DELETE FROM graphql_actions").Error; err != nil { return policyDatabaseError() }
			if err := tx.Exec("DELETE FROM graphql_exposures").Error; err != nil { return policyDatabaseError() }
			if err := tx.Exec("DELETE FROM graphql_changelog").Error; err != nil { return policyDatabaseError() }
		}
		if len(roleSeeds) > 0 {
			if err := tx.Exec("DELETE FROM role_actions").Error; err != nil { return policyDatabaseError() }
			if err := tx.Exec("DELETE FROM role_user").Error; err != nil { return policyDatabaseError() }
			if err := tx.Exec("DELETE FROM roles").Error; err != nil { return policyDatabaseError() }
			if err := tx.Exec("DELETE FROM rbac_changelog").Error; err != nil { return policyDatabaseError() }
		}
		return nil
	}); err != nil {
		if errors.Is(err, errPolicyDatabase) {
			return err
		}
		return policyDatabaseError()
	}
	return nil
}

func Fresh(db *gorm.DB, driver string) error {
	if err := ensurePolicyDB(db); err != nil { return err }
	if err := db.Transaction(func(tx *gorm.DB) error {
		for _, table := range []string{"graphql_actions", "graphql_exposures", "graphql_changelog", "role_actions", "role_user", "roles", "rbac_changelog"} {
			if err := tx.Exec("DROP TABLE IF EXISTS " + table).Error; err != nil {
				return fmt.Errorf("policy fresh drop %s", table)
			}
		}
		return nil
	}); err != nil {
		if strings.HasPrefix(err.Error(), "policy fresh drop ") {
			return err
		}
		return fmt.Errorf("policy fresh drop")
	}
	return Migrate(db, driver)
}

func Status(db *gorm.DB, driver string) ([]PolicyStatus, error) {
	if err := ensurePolicyDB(db); err != nil { return nil, err }
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
		if err := db.Exec(stmt).Error; err != nil { return policyDatabaseError() }
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
		if err := db.Exec(stmt).Error; err != nil { return policyDatabaseError() }
	}
	return nil
}

func seedRoles(db *gorm.DB) error {
	now := time.Now().UTC()
	for _, role := range roleSeeds {
		if err := db.Exec(roleUpsertSQL(db), uuid.New().String(), role.Slug, role.Name, role.Manages, role.Default, role.BirthPolicy, now, now).Error; err != nil { return policyDatabaseError() }
		if err := db.Exec("DELETE FROM role_actions WHERE role_slug = ?", role.Slug).Error; err != nil { return policyDatabaseError() }
		for _, action := range role.Actions {
			if err := db.Exec("INSERT INTO role_actions (id, role_slug, action, created_at, updated_at) VALUES (?, ?, ?, ?, ?)", uuid.New().String(), role.Slug, action, now, now).Error; err != nil { return policyDatabaseError() }
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
	if err := db.Exec("DELETE FROM graphql_exposures").Error; err != nil { return policyDatabaseError() }
	for _, exposure := range graphQLExposureSeeds {
		if err := db.Exec("INSERT INTO graphql_exposures (id, model, operation, created_at, updated_at) VALUES (?, ?, ?, ?, ?)", uuid.New().String(), exposure.Model, exposure.Operation, now, now).Error; err != nil { return policyDatabaseError() }
	}
	if err := db.Exec("DELETE FROM graphql_actions").Error; err != nil { return policyDatabaseError() }
	for _, action := range graphQLActionSeeds {
		if err := db.Exec("INSERT INTO graphql_actions (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)", uuid.New().String(), action.Name, now, now).Error; err != nil { return policyDatabaseError() }
	}
	return nil
}

func markPoliciesApplied(db *gorm.DB, table string, ids []string) error {
	now := time.Now().UTC()
	for _, id := range ids {
		if err := db.Exec(policyAppliedUpsertSQL(db, table), id, 1, "applied", now, now).Error; err != nil { return policyDatabaseError() }
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
	if err != nil { return nil, policyDatabaseError() }
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var batch int
		if err := rows.Scan(&id, &batch); err != nil { return nil, policyDatabaseError() }
		out[id] = batch
	}
	if err := rows.Err(); err != nil { return nil, policyDatabaseError() }
	return out, nil
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
	"errors"
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

var errMigrationDatabase = errors.New("migration database error")

func migrationDatabaseError() error {
	return errMigrationDatabase
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

func (r *Runner) ensureDB() error {
	if r == nil || r.DB == nil {
		return fmt.Errorf("migrations: DB is nil")
	}
	return nil
}

func (r *Runner) ensureMigrationsTable() error {
	if err := r.ensureDB(); err != nil {
		return err
	}
	if err := r.DB.Exec(migrationsTableSQL(r.Driver)).Error; err != nil {
		return migrationDatabaseError()
	}
	return nil
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
		return nil, migrationDatabaseError()
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var batch int
		if err := rows.Scan(&id, &batch); err != nil {
			return nil, migrationDatabaseError()
		}
		out[id] = batch
	}
	if err := rows.Err(); err != nil {
		return nil, migrationDatabaseError()
	}
	return out, nil
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
	if err := r.ensureDB(); err != nil {
		return err
	}
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
		if err := r.DB.Transaction(func(tx *gorm.DB) error {
			if err := r.execMigrationFileOn(tx, entry.UpFile); err != nil {
				return fmt.Errorf("migrating %s: %w", entry.ID, err)
			}
			if err := tx.Exec("INSERT INTO migrations (migration, batch) VALUES (?, ?)", entry.ID, batch).Error; err != nil {
				return fmt.Errorf("recording %s: %w", entry.ID, migrationDatabaseError())
			}
			return nil
		}); err != nil {
			return err
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
	if err := r.ensureDB(); err != nil {
		return err
	}
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
		if err := r.DB.Transaction(func(tx *gorm.DB) error {
			if err := r.execMigrationFileOn(tx, entry.DownFile); err != nil {
				return fmt.Errorf("rolling back %s: %w", entry.ID, err)
			}
			if err := tx.Exec("DELETE FROM migrations WHERE migration = ?", entry.ID).Error; err != nil {
				return fmt.Errorf("removing %s: %w", entry.ID, migrationDatabaseError())
			}
			return nil
		}); err != nil {
			return err
		}
		fmt.Printf("  rolled back: %s\n", entry.ID)
	}
	return nil
}

func (r *Runner) Fresh(entries []MigrationEntry) error {
	if err := r.ensureDB(); err != nil {
		return err
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if err := r.execMigrationFile(entries[i].DownFile); err != nil {
			return fmt.Errorf("fresh rollback %s: %w", entries[i].ID, err)
		}
	}
	if err := r.DB.Exec("DROP TABLE IF EXISTS migrations").Error; err != nil {
		return migrationDatabaseError()
	}
	return r.Migrate(entries)
}

func (r *Runner) Status(entries []MigrationEntry) ([]MigrationStatus, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
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
	if err := r.ensureDB(); err != nil {
		return err
	}
	return r.execMigrationFileOn(r.DB, name)
}

func (r *Runner) execMigrationFileOn(db *gorm.DB, name string) error {
	if db == nil {
		return fmt.Errorf("migrations: DB is nil")
	}
	data, err := migrationFiles.ReadFile(name)
	if err != nil {
		return err
	}
	sql := normalizeSQLForDriver(string(data), r.Driver)
	for i, statement := range splitSQLStatements(sql) {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("executing migration statement %d: %w", i+1, migrationDatabaseError())
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
	var b strings.Builder
	inSingleQuote := false
	inDoubleQuote := false
	inLineComment := false
	inBlockComment := false
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		if inLineComment {
			b.WriteByte(ch)
			if ch == '\n' || ch == '\r' {
				inLineComment = false
			}
			continue
		}
		if inBlockComment {
			b.WriteByte(ch)
			if ch == '*' && i+1 < len(sql) && sql[i+1] == '/' {
				i++
				b.WriteByte(sql[i])
				inBlockComment = false
			}
			continue
		}
		if !inSingleQuote && !inDoubleQuote && ch == '-' && i+1 < len(sql) && sql[i+1] == '-' {
			b.WriteByte(ch)
			i++
			b.WriteByte(sql[i])
			inLineComment = true
			continue
		}
		if !inSingleQuote && !inDoubleQuote && ch == '/' && i+1 < len(sql) && sql[i+1] == '*' {
			b.WriteByte(ch)
			i++
			b.WriteByte(sql[i])
			inBlockComment = true
			continue
		}
		if ch == '\'' && !inDoubleQuote {
			b.WriteByte(ch)
			if inSingleQuote && i+1 < len(sql) && sql[i+1] == '\'' {
				i++
				b.WriteByte(sql[i])
				continue
			}
			inSingleQuote = !inSingleQuote
			continue
		}
		if ch == '"' && !inSingleQuote {
			b.WriteByte(ch)
			if inDoubleQuote && i+1 < len(sql) && sql[i+1] == '"' {
				i++
				b.WriteByte(sql[i])
				continue
			}
			inDoubleQuote = !inDoubleQuote
			continue
		}
		if ch == ';' && !inSingleQuote && !inDoubleQuote {
			stmt := strings.TrimSpace(b.String())
			if stmt != "" {
				statements = append(statements, stmt)
			}
			b.Reset()
			continue
		}
		b.WriteByte(ch)
	}
	stmt := strings.TrimSpace(b.String())
	if stmt != "" {
		statements = append(statements, stmt)
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
	userCommands, err := generator.ScanCommands(e.project.Layout.CommandsDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("scanning exported user commands: %w", err)
	}
	sort.Strings(userCommands)
	routeVars, err := e.exportedRouteVars()
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("package commands\n\n")
	b.WriteString("import (\n")
	if hasSchedule {
		b.WriteString("\t\"context\"\n")
	}
	b.WriteString("\t\"fmt\"\n")
	b.WriteString("\t\"log\"\n")
	b.WriteString("\t\"net/http\"\n")
	b.WriteString("\t\"time\"\n")
	if hasSchedule {
		b.WriteString("\t\"os\"\n")
		b.WriteString("\t\"os/signal\"\n")
	}
	b.WriteString("\t\"time\"\n\n")
	if hasGraphQL {
		b.WriteString(fmt.Sprintf("\t\"%s/app/graphqlapi\"\n", e.modulePath))
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
		if cmd == nil || cmd.Name() == "" {
			continue
		}
		if _, exists := app.commands[cmd.Name()]; exists {
			continue
		}
		app.commands[cmd.Name()] = cmd
	}
	return app
}

func (a *App) Run(args []string) {
	if a == nil {
		return
	}
	if err := a.run(args); err != nil {
		log.Fatal(err.Error())
	}
}

func (a *App) run(args []string) error {
	if a == nil {
		return nil
	}
	if a.initFn != nil {
		if len(args) == 0 {
			a.initFn()
		}
	}
	if len(args) > 0 {
		cmd, ok := a.commands[args[0]]
		if !ok {
			return fmt.Errorf("%s", unknownCommandMessage())
		}
		if a.initFn != nil {
			a.initFn()
		}
		if err := cmd.Run(args[1:]); err != nil {
			return fmt.Errorf("%s", commandFailureMessage())
		}
		return nil
	}
	if a.serveFn != nil {
		a.serveFn()
	}
	return nil
}

func commandFailureMessage() string {
	return "command failed"
}

func unknownCommandMessage() string {
	return "unknown command"
}

func commandStartupFailureMessage(component string) string {
	if component == "" {
		return "startup failed"
	}
	return fmt.Sprintf("%s startup failed", component)
}

func serverFailureMessage(_ error) string {
	return "server failed"
}

func (a *App) PrintCommands() {
	fmt.Println("Available commands:")
	if a == nil {
		return
	}
	for name, cmd := range a.commands {
		if cmd == nil {
			continue
		}
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
	return []Command{
`)
	for _, cmd := range userCommands {
		fmt.Fprintf(&b, "\t\t%s{},\n", cmd)
	}
	b.WriteString(`	}
}

func Commands() []Command {
	return append(BuiltinCommands(), UserCommands()...)
}

func HTTPHandler() http.Handler {
	mux := http.NewServeMux()
`)
	for _, routeVar := range routeVars {
		fmt.Fprintf(&b, "\troutes.%s.RegisterRoutes(mux)\n", routeVar)
	}
	if hasGraphQL {
		b.WriteString("\tmux.Handle(\"/graphql\", graphqlapi.Handler())\n")
		b.WriteString("\tmux.Handle(\"/graphql/playground\", graphqlapi.PlaygroundHandler(\"/graphql\"))\n")
	}
	b.WriteString(`	mux.HandleFunc("/", exportedNotFound)
	return mux
}

func exportedNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(` + "`" + `{"error":"not found"}
` + "`" + `))
}

func NewApp() *App {
	return BuildApp(
		func() {
			config.Init()
			conn := config.Database.Connection()
			db := config.OpenGORM(conn)
			models.SetDBWithDriver(db, conn.Driver)
			sqlDB, err := db.DB()
			if err != nil {
				log.Fatal(commandStartupFailureMessage("database"))
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
	b.WriteString(`			addr := ":" + config.App.Port
			log.Printf("listening on %s", addr)
			server := &http.Server{
				Addr: addr,
				Handler: HTTPHandler(),
				ReadHeaderTimeout: 10 * time.Second,
				ReadTimeout: 30 * time.Second,
				WriteTimeout: 60 * time.Second,
				IdleTimeout: 120 * time.Second,
				MaxHeaderBytes: 1 << 20,
			}
			if err := server.ListenAndServe(); err != nil {
				log.Fatal(serverFailureMessage(err))
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
			up, err := sqlForMigrationOps(migration.Up, tables...)
			if err != nil {
				return nil, migrationExportError("database/migrations", migration.Name, err)
			}
			down, err := sqlForMigrationOps(migration.Down, tables...)
			if err != nil {
				return nil, migrationExportError("database/migrations", migration.Name, err)
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
			out = append(out, sqlMigration{Name: exportName, Up: createViewSQL(view, tables...) + ";\n", Down: dropViewSQL(view.Name) + ";\n"})
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

func sqlForMigrationOps(ops []generator.MigrationOperation, tables ...*schema.Table) (string, error) {
	tableByName := map[string]*schema.Table{}
	for _, table := range tables {
		tableByName[table.Name] = table
	}
	var statements []string
	for _, op := range ops {
		sql, err := sqlForMigrationOp(op, tableByName)
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

func mapValues[K comparable, V any](m map[K]V) []V {
	values := make([]V, 0, len(m))
	for _, v := range m {
		values = append(values, v)
	}
	return values
}

func sqlForMigrationOp(op generator.MigrationOperation, tableByName map[string]*schema.Table) (string, error) {
	switch op.Type {
	case "create_table":
		if op.TableDef == nil {
			return "", fmt.Errorf("create_table missing table definition")
		}
		tableByName[op.TableDef.Name] = op.TableDef
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
			for _, storageCol := range sqlStorageColumns(col) {
				statements = append(statements, "ALTER TABLE "+quoteIdent(op.Table)+" ADD COLUMN "+columnSQL(storageCol, false))
			}
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
		if table, ok := tableByName[idx.Table]; ok {
			idx = *indexForTableStorage(table, &idx)
		}
		return createIndexSQL(&idx), nil
	case "create_view":
		if op.ViewDef == nil {
			return "", fmt.Errorf("create_view missing view definition")
		}
		return createViewSQL(op.ViewDef, mapValues(tableByName)...), nil
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
	return migrationExportError(filepath.Join("database", "migrations", fileName), fileName, fmt.Errorf("%s migrations are not lowered yet", kind))
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
		for _, storageCol := range sqlStorageColumns(col) {
			cols = append(cols, "\t"+columnSQL(storageCol, compositePrimaryKey))
		}
	}
	if len(pk) > 1 {
		cols = append(cols, "\tPRIMARY KEY ("+strings.Join(pk, ", ")+")")
	}
	return "CREATE TABLE " + quoteIdent(table.Name) + " (\n" + strings.Join(cols, ",\n") + "\n)"
}

func sqlStorageColumns(col *schema.Column) []*schema.Column {
	if col.IsEncrypted || col.IsSealed {
		return []*schema.Column{
			encryptedStorageColumn(col, col.Name+"_encrypted", col.IsNullable),
			encryptedStorageColumn(col, col.Name+"_encrypted_v2", true),
		}
	}
	return []*schema.Column{col}
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
		parts = append(parts, createIndexSQL(indexForTableStorage(table, idx)))
	}
	return ";\n" + strings.Join(parts, ";\n")
}

func indexForTableStorage(table *schema.Table, idx *schema.Index) *schema.Index {
	out := *idx
	out.Columns = make([]string, 0, len(idx.Columns))
	storageColumns := map[string]string{}
	for _, col := range table.Columns {
		if col.IsEncrypted || col.IsSealed {
			storageColumns[col.Name] = col.Name + "_encrypted"
		}
	}
	for _, col := range idx.Columns {
		if storage, ok := storageColumns[col]; ok {
			out.Columns = append(out.Columns, storage)
			continue
		}
		out.Columns = append(out.Columns, col)
	}
	return &out
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

func createViewSQL(view *schema.View, tables ...*schema.Table) string {
	storageColumns := viewStorageColumns(view, tables...)
	var selectCols []string
	for _, col := range view.Columns {
		if col.RawExpr != "" {
			selectCols = append(selectCols, "\t"+col.RawExpr+" AS "+quoteIdent(col.OutputName()))
			continue
		}
		sourceColumn := col.SourceColumn
		if storage, ok := storageColumns[col.SourceAlias+"."+col.SourceColumn]; ok {
			sourceColumn = storage
		}
		expr := quoteIdent(col.SourceAlias) + "." + quoteIdent(sourceColumn)
		if col.OutputName() != sourceColumn {
			expr += " AS " + quoteIdent(col.OutputName())
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
		var groupBy []string
		for _, col := range view.GroupByCols {
			groupBy = append(groupBy, viewGroupByStorageColumn(col, storageColumns))
		}
		b.WriteString(strings.Join(groupBy, ", "))
	}
	return b.String()
}

func viewStorageColumns(view *schema.View, tables ...*schema.Table) map[string]string {
	tableByName := map[string]*schema.Table{}
	for _, table := range tables {
		tableByName[table.Name] = table
	}
	aliasTable := map[string]string{}
	for _, src := range view.Sources {
		aliasTable[src.Alias] = src.Table
	}
	out := map[string]string{}
	for alias, tableName := range aliasTable {
		table, ok := tableByName[tableName]
		if !ok {
			continue
		}
		for _, col := range table.Columns {
			if col.IsEncrypted || col.IsSealed {
				out[alias+"."+col.Name] = col.Name + "_encrypted"
			}
		}
	}
	return out
}

func viewGroupByStorageColumn(col string, storageColumns map[string]string) string {
	parts := strings.Split(col, ".")
	if len(parts) != 2 {
		return col
	}
	key := parts[0] + "." + parts[1]
	if storage, ok := storageColumns[key]; ok {
		return parts[0] + "." + storage
	}
	return col
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
	routeVars, err := e.exportedRouteVars()
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("package main\n\n")
	b.WriteString("import (\n")
	if hasSchedule {
		b.WriteString("\t\"context\"\n")
	}
	b.WriteString("\t\"log\"\n")
	b.WriteString("\t\"net/http\"\n")
	b.WriteString("\t\"time\"\n")
	if hasSchedule {
		b.WriteString("\t\"os\"\n")
		b.WriteString("\t\"os/signal\"\n")
	}
	b.WriteString("\n")
	if hasGraphQL {
		b.WriteString(fmt.Sprintf("\t\"%s/app/graphqlapi\"\n", e.modulePath))
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
		b.WriteString("\tconn := config.Database.Connection()\n")
		b.WriteString("\tmodels.SetDBWithDriver(config.OpenGORM(conn), conn.Driver)\n")
	}
	if hasSchedule {
		b.WriteString("\tctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)\n")
		b.WriteString("\tdefer stop()\n")
		b.WriteString("\tgo schedule.Schedule.Start(ctx)\n")
	}
	b.WriteString("\tmux := http.NewServeMux()\n")
	for _, routeVar := range routeVars {
		fmt.Fprintf(&b, "\troutes.%s.RegisterRoutes(mux)\n", routeVar)
	}
	if hasGraphQL {
		b.WriteString("\tmux.Handle(\"/graphql\", graphqlapi.Handler())\n")
		b.WriteString("\tmux.Handle(\"/graphql/playground\", graphqlapi.PlaygroundHandler(\"/graphql\"))\n")
	}
	b.WriteString("\tmux.HandleFunc(\"/\", exportedNotFound)\n")
	b.WriteString("\taddr := \":\" + config.App.Port\n")
	b.WriteString("\tlog.Printf(\"listening on %s\", addr)\n")
	b.WriteString("\tserver := &http.Server{\n")
	b.WriteString("\t\tAddr:              addr,\n")
	b.WriteString("\t\tHandler:           mux,\n")
	b.WriteString("\t\tReadHeaderTimeout: 10 * time.Second,\n")
	b.WriteString("\t\tReadTimeout:       30 * time.Second,\n")
	b.WriteString("\t\tWriteTimeout:      60 * time.Second,\n")
	b.WriteString("\t\tIdleTimeout:       120 * time.Second,\n")
	b.WriteString("\t\tMaxHeaderBytes:    1 << 20,\n")
	b.WriteString("\t}\n")
	b.WriteString("\tif err := server.ListenAndServe(); err != nil {\n")
	b.WriteString("\t\tlog.Fatal(\"server failed\")\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n")
	writeExportedNotFoundFunc(&b)
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
	b.WriteString("\t\"time\"\n")
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
		b.WriteString("\tconn := config.Database.Connection()\n")
		b.WriteString("\tmodels.SetDBWithDriver(config.OpenGORM(conn), conn.Driver)\n")
	}
	if hasSchedule {
		b.WriteString("\tctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)\n")
		b.WriteString("\tdefer stop()\n")
		b.WriteString("\tgo schedule.Schedule.Start(ctx)\n")
	}
	b.WriteString("\tmux := http.NewServeMux()\n")
	for i, svc := range e.project.Services {
		routeVars, err := exportedServiceRouteVars(svc)
		if err != nil {
			return nil, err
		}
		prefix := "/" + strings.Trim(svc.Name, "/") + "/"
		if i == 0 && svc.Name == "api" {
			for _, routeVar := range routeVars {
				fmt.Fprintf(&b, "\t%sRoutes.%s.RegisterRoutes(mux)\n", safeImportAlias(svc.Name), routeVar)
			}
			continue
		}
		stripPrefix := strings.TrimSuffix(prefix, "/")
		serviceMux := safeImportAlias(svc.Name) + "Mux"
		fmt.Fprintf(&b, "\t%s := http.NewServeMux()\n", serviceMux)
		for _, routeVar := range routeVars {
			fmt.Fprintf(&b, "\t%sRoutes.%s.RegisterRoutes(%s)\n", safeImportAlias(svc.Name), routeVar, serviceMux)
		}
		b.WriteString(fmt.Sprintf("\tmux.Handle(%q, http.StripPrefix(%q, %s))\n", prefix, stripPrefix, serviceMux))
	}
	b.WriteString("\tmux.HandleFunc(\"/\", exportedNotFound)\n")
	b.WriteString("\taddr := \":\" + config.App.Port\n")
	b.WriteString("\tlog.Printf(\"listening on %s\", addr)\n")
	b.WriteString("\tserver := &http.Server{\n")
	b.WriteString("\t\tAddr:              addr,\n")
	b.WriteString("\t\tHandler:           mux,\n")
	b.WriteString("\t\tReadHeaderTimeout: 10 * time.Second,\n")
	b.WriteString("\t\tReadTimeout:       30 * time.Second,\n")
	b.WriteString("\t\tWriteTimeout:      60 * time.Second,\n")
	b.WriteString("\t\tIdleTimeout:       120 * time.Second,\n")
	b.WriteString("\t\tMaxHeaderBytes:    1 << 20,\n")
	b.WriteString("\t}\n")
	b.WriteString("\tif err := server.ListenAndServe(); err != nil {\n")
	b.WriteString("\t\tlog.Fatal(\"server failed\")\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n")
	writeExportedNotFoundFunc(&b)
	return format.Source([]byte(b.String()))
}

func writeExportedNotFoundFunc(b *strings.Builder) {
	b.WriteString(`

func exportedNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(` + "`" + `{"error":"not found"}
` + "`" + `))
}
`)
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

func (e *exporter) addGraphQLActionFindings() {
	if e.result == nil || !e.hasGraphQLPolicies() {
		return
	}
	_, unsupported := e.exportedGraphQLControllerActions()
	for _, action := range unsupported {
		message := fmt.Sprintf("GraphQL controller action %s is not lowered by the exported Go GraphQL target backed by gqlgen", action.Name)
		if action.Handler != "" && action.Handler != "nil" {
			message = fmt.Sprintf("GraphQL controller action %s (%s) is not lowered by the exported Go GraphQL target backed by gqlgen", action.Name, action.Handler)
		}
		e.result.Findings = append(e.result.Findings, Finding{
			File:    filepath.Join("database", "policies", "graphql"),
			Rule:    "graphql_action_export_unsupported",
			Message: message,
		})
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
	b.WriteString("## Exported\n\n")
	b.WriteString("- Standalone Go module with rewritten imports and no Pickle runtime dependency\n")
	b.WriteString("- GORM models and database handle setup\n")
	b.WriteString("- SQL migrations for supported schema operations\n")
	b.WriteString("- HTTP routing, request binding, auth, config, and server support\n")
	if e.hasGraphQLPackage() {
		b.WriteString("- Exported Go GraphQL target backed by gqlgen, with GORM query support and /graphql server mount\n")
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
	case "action_export_unsupported_query", "action_export_unsupported_signature",
		"gate_export_unsupported_signature", "graphql_action_export_unsupported",
		"migration_export_unsupported", "orm_export_unsupported", "query_export_unsupported":
		return "unsupported"
	case "rbac_policy_export":
		return "partial"
	case "generated_graphql", "generated_graphql_policies", "generated_policies", "generated_actions",
		"action_export_import_cycle":
		return "omitted"
	case "encrypted_columns", "integrity_tables", "raw_sql_migration", "actions_audit":
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
		fmt.Fprintf(&b, "\tif DB == nil { return integrityDatabaseError() }\n")
		fmt.Fprintf(&b, "\tif err := DB.Order(%q).Find(&records).Error; err != nil { return integrityDatabaseError() }\n", verifyOrder)
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

func quotedRoleColumnMap(values map[string][]string) string {
	var keys []string
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%q: []string{%s}", key, quotedStringList(values[key])))
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
	"errors"
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
	"gorm.io/gorm/logger"
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
		return errors.New("unsupported database driver")
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
		return "", errors.New("unsupported database driver")
	}
}

func (c ConnectionConfig) driverName() (string, error) {
	switch c.Driver {
	case "pgsql", "postgres": return "pgx", nil
	case "mysql": return "mysql", nil
	case "sqlite": return "sqlite3", nil
	default: return "", errors.New("unsupported database driver")
	}
}

func TryOpenDB(conn ConnectionConfig) (*sql.DB, error) {
	driverName, err := conn.driverName()
	if err != nil { return nil, err }
	dsn, err := conn.dsn()
	if err != nil { return nil, err }
	db, err := sql.Open(driverName, dsn)
	if err != nil { return nil, errors.New("open database") }
	if err := db.Ping(); err != nil { db.Close(); return nil, errors.New("ping database") }
	return db, nil
}

func OpenDB(conn ConnectionConfig) *sql.DB {
	db, err := TryOpenDB(conn)
	if err != nil { log.Fatal(sanitizedDatabaseStartupError("open")) }
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
		return nil, errors.New("unsupported database driver")
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil { sqlDB.Close(); return nil, errors.New("initialize gorm") }
	return db, nil
}

func OpenGORM(conn ConnectionConfig) *gorm.DB {
	db, err := TryOpenGORM(conn)
	if err != nil { log.Fatal(sanitizedDatabaseStartupError("initialize")) }
	return db
}

func sanitizedDatabaseStartupError(operation string) string {
	switch operation {
	case "open":
		return "config: failed to open database"
	case "initialize":
		return "config: failed to initialize database"
	default:
		return "config: database startup failed"
	}
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
	if err != nil { log.Fatal(sanitizedDatabaseStartupError("config")) }
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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

const maxJSONRequestBodyBytes = 1 << 20

var validate = newValidator()

func newValidator() *validator.Validate {
	v := validator.New()
	v.RegisterTagNameFunc(func(field reflect.StructField) string {
		name := strings.SplitN(field.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})
	return v
}

type ValidationError struct { Field string ` + "`" + `json:"field"` + "`" + `; Message string ` + "`" + `json:"message"` + "`" + ` }
type BindingError struct { Status int ` + "`" + `json:"-"` + "`" + `; Errors []ValidationError ` + "`" + `json:"errors"` + "`" + ` }
func (e *BindingError) Error() string { if e == nil { return "binding failed" }; parts := make([]string, 0, len(e.Errors)); for _, ve := range e.Errors { if ve.Field == "" && ve.Message == "" { continue }; parts = append(parts, ve.Field + ": " + ve.Message) }; if len(parts) == 0 { return "binding failed" }; return strings.Join(parts, "; ") }
func formatValidationErrors(err error) *BindingError { ve, ok := err.(validator.ValidationErrors); if !ok { return &BindingError{Status: 422, Errors: []ValidationError{{"{{"}}Field: "_body", Message: "validation failed"{{"}}"}}} }; out := make([]ValidationError, len(ve)); for i, fe := range ve { out[i] = ValidationError{Field: fe.Field(), Message: fmt.Sprintf("failed %s validation", fe.Tag())} }; return &BindingError{Status: 422, Errors: out} }
func bindJSONBody(r *http.Request, dest any) *BindingError { if r == nil || r.Body == nil { return &BindingError{Status: 400, Errors: []ValidationError{{"{{"}}Field: "_body", Message: "invalid request body"{{"}}"}}} }; if !isJSONContentType(r.Header.Get("Content-Type")) { return &BindingError{Status: http.StatusUnsupportedMediaType, Errors: []ValidationError{{"{{"}}Field: "_body", Message: "Content-Type must be application/json"{{"}}"}}} }; if r.ContentLength > maxJSONRequestBodyBytes { return &BindingError{Status: http.StatusRequestEntityTooLarge, Errors: []ValidationError{{"{{"}}Field: "_body", Message: "request body too large"{{"}}"}}} }; body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONRequestBodyBytes+1)); if err != nil { return &BindingError{Status: 400, Errors: []ValidationError{{"{{"}}Field: "_body", Message: "invalid request body"{{"}}"}}} }; if len(body) > maxJSONRequestBodyBytes { return &BindingError{Status: http.StatusRequestEntityTooLarge, Errors: []ValidationError{{"{{"}}Field: "_body", Message: "request body too large"{{"}}"}}} }; decoder := json.NewDecoder(bytes.NewReader(body)); decoder.DisallowUnknownFields(); if err := decoder.Decode(dest); err != nil { return &BindingError{Status: 400, Errors: []ValidationError{{"{{"}}Field: "_body", Message: "invalid request body"{{"}}"}}} }; if decoder.Decode(&struct{}{}) != io.EOF { return &BindingError{Status: 400, Errors: []ValidationError{{"{{"}}Field: "_body", Message: "invalid request body"{{"}}"}}} }; return nil }
func isJSONContentType(contentType string) bool { if contentType == "" { return false }; mediaType, _, err := mime.ParseMediaType(contentType); return err == nil && mediaType == "application/json" }
{{ range .Requests }}
func Bind{{ .Name }}(r *http.Request) ({{ .Name }}, *BindingError) { var req {{ .Name }}; if err := bindJSONBody(r, &req); err != nil { return req, err }; if err := validate.Struct(req); err != nil { return req, formatValidationErrors(err) }; return req, nil }
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

func snakeToCamel(s string) string {
	pascal := snakeToPascal(s)
	if pascal == "" {
		return pascal
	}
	if strings.HasPrefix(pascal, "ID") && len(pascal) > 2 {
		return "id" + pascal[2:]
	}
	return strings.ToLower(pascal[:1]) + pascal[1:]
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

func primaryKeyColumn(tbl *schema.Table) *schema.Column {
	for _, col := range tbl.Columns {
		if col.IsPrimaryKey {
			return col
		}
	}
	return nil
}

func modelSet(tables []*schema.Table) map[string]bool {
	out := map[string]bool{}
	for _, tbl := range tables {
		out[tableToStruct(tbl.Name)] = true
	}
	return out
}

func schemaTableSet(tables []*schema.Table) map[string]*schema.Table {
	out := map[string]*schema.Table{}
	for _, tbl := range tables {
		out[tableToStruct(tbl.Name)] = tbl
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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Controller struct{}
type Response struct { Status int; StatusCode int; Body any; Headers map[string]string; Cookies []*http.Cookie }
type AuthInfo struct { UserID string; Role string; Claims any }
type RoleInfo struct { Slug string; Manages bool }
type Context struct { request *http.Request; response http.ResponseWriter; auth *AuthInfo; params map[string]string; roles []string; rolesLoaded bool; isAdmin bool }

func NewContext(r *http.Request) *Context { return &Context{request: r, params: map[string]string{}} }
func NewContextWithResponse(w http.ResponseWriter, r *http.Request) *Context { ctx := NewContext(r); ctx.response = w; return ctx }
func (c *Context) Request() *http.Request { if c == nil { return nil }; return c.request }
func (c *Context) ResponseWriter() http.ResponseWriter { if c == nil { return nil }; return c.response }
func (c *Context) Param(name string) string { if c == nil { return "" }; value, ok := c.params[name]; if !ok { panic("route parameter is missing: " + name) }; return value }
func (c *Context) SetParam(name, value string) { if c == nil { return }; if c.params == nil { c.params = map[string]string{} }; c.params[name] = value }
func (c *Context) ParamUUID(name string) (uuid.UUID, error) { value, err := uuid.Parse(c.Param(name)); if err != nil { return uuid.Nil, fmt.Errorf("invalid uuid parameter") }; return value, nil }
func (c *Context) Cookie(name string) (string, error) { if c == nil || c.request == nil { return "", http.ErrNoCookie }; cookie, err := c.request.Cookie(name); if err != nil { return "", err }; return cookie.Value, nil }
func (c *Context) Query(name string) string { if c == nil || c.request == nil || c.request.URL == nil { return "" }; return c.request.URL.Query().Get(name) }
func (c *Context) BearerToken() string { if c == nil || c.request == nil { return "" }; h := c.request.Header.Get("Authorization"); parts := strings.Fields(h); if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") { return parts[1] }; return "" }
func (c *Context) ClientIP() string { if c == nil || c.request == nil { return "" }; return clientIP(c.request) }
func (c *Context) Auth() *AuthInfo { if c == nil || c.auth == nil { return &AuthInfo{} }; return c.auth }
func (c *Context) SetAuth(claims any) { if c == nil { return }; switch v := claims.(type) { case nil: c.auth = nil; case *AuthInfo: c.auth = v; default: panic(fmt.Sprintf("SetAuth requires *AuthInfo, got %T", claims)) } }
func (c *Context) IsAuthenticated() bool { return c != nil && c.auth != nil && c.auth.UserID != "" }
func (c *Context) SetRoles(roles []RoleInfo) { if c == nil { return }; c.rolesLoaded = true; c.roles = make([]string, len(roles)); c.isAdmin = false; for i, role := range roles { c.roles[i] = role.Slug; if role.Manages { c.isAdmin = true } } }
func (c *Context) Role() string { if c == nil { return "" }; if len(c.roles) > 0 { return c.roles[0] }; if !c.rolesLoaded && c.auth != nil { return c.auth.Role }; return "" }
func (c *Context) Roles() []string { if c == nil { return nil }; roles := append([]string{}, c.roles...); if !c.rolesLoaded && len(roles) == 0 && c.auth != nil && c.auth.Role != "" { roles = append(roles, c.auth.Role) }; return roles }
func (c *Context) HasRole(slug string) bool { if c == nil { return false }; for _, role := range c.roles { if role == slug { return true } }; return !c.rolesLoaded && c.auth != nil && c.auth.Role == slug }
func (c *Context) HasAnyRole(roles ...string) bool { for _, role := range roles { if c.HasRole(role) { return true } }; return false }
func (c *Context) IsAdmin() bool { return c != nil && (c.isAdmin || (!c.rolesLoaded && c.auth != nil && c.auth.Role == "admin")) }
func (c *Context) JSON(status int, body any) Response { return Response{Status: status, StatusCode: status, Body: body} }
func (c *Context) Error(err error) Response { if err != nil { log.Printf("http error") }; return c.JSON(500, map[string]string{"error": "internal server error"}) }
func (c *Context) BadRequest(msg string) Response { return c.JSON(400, map[string]string{"error": msg}) }
func (c *Context) Unauthorized(msg string) Response { return c.JSON(401, map[string]string{"error": msg}) }
func (c *Context) Forbidden(msg string) Response { return c.JSON(403, map[string]string{"error": msg}) }
func (c *Context) NotFound(msg string) Response { return c.JSON(404, map[string]string{"error": msg}) }
func (c *Context) NoContent() Response { return Response{Status: 204, StatusCode: 204} }

type ResourceQuery interface { FetchResource(ownerID string) (any, error) }
type ResourceListQuery interface { FetchResources(ownerID string) (any, error) }
func isResourceNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) || errors.Is(err, gorm.ErrRecordNotFound) }
func (c *Context) Resource(q ResourceQuery) Response { ownerID := ""; if c != nil && c.auth != nil { ownerID = c.auth.UserID }; result, err := q.FetchResource(ownerID); if err != nil { if isResourceNotFound(err) { return c.NotFound("not found") }; return c.Error(err) }; return c.JSON(http.StatusOK, result) }
func (c *Context) Resources(q ResourceListQuery) Response { ownerID := ""; if c != nil && c.auth != nil { ownerID = c.auth.UserID }; result, err := q.FetchResources(ownerID); if err != nil { return c.Error(err) }; return c.JSON(http.StatusOK, result) }

func (r Response) WithCookie(cookie *http.Cookie) Response { if cookie != nil { r.Cookies = append(r.Cookies, cookie) }; return r }

func (r Response) Write(w http.ResponseWriter) { if w == nil { return }; for k, v := range r.Headers { w.Header().Set(k, v) }; if r.Body != nil { if w.Header().Get("Content-Type") == "" { w.Header().Set("Content-Type", "application/json") }; if w.Header().Get("X-Content-Type-Options") == "" { w.Header().Set("X-Content-Type-Options", "nosniff") } }; for _, cookie := range r.Cookies { if cookie != nil { http.SetCookie(w, cookie) } }; status := r.Status; if status == 0 { status = r.StatusCode }; if status == 0 { status = 200 }; w.WriteHeader(status); if r.Body != nil { _ = json.NewEncoder(w).Encode(r.Body) } }

type HandlerFunc func(*Context) Response
type MiddlewareFunc func(*Context, func() Response) Response
type MiddlewareProvider interface { Middleware() MiddlewareFunc }
type ResourceController interface { Index(*Context) Response; Show(*Context) Response; Store(*Context) Response; Update(*Context) Response; Destroy(*Context) Response }
type ErrorReporter func(*Context, error)
type Route struct { Method string; Path string; Handler HandlerFunc; Middleware []MiddlewareFunc }
type Router struct{ prefix string; middleware []MiddlewareFunc; routes []Route; onError ErrorReporter }
func Routes(fn func(*Router)) *Router { r := &Router{}; if fn != nil { fn(r) }; return r }
func (r *Router) OnError(fn ErrorReporter) { if r != nil { r.onError = fn } }
func (r *Router) OnRateLimit(fn func(*Context, RateLimitEvent)) { rateLimitCallback = fn }
func (r *Router) Group(path string, args ...any) {
	if r == nil {
		return
	}
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
func (r *Router) add(method, path string, handler HandlerFunc, middleware ...any) { if r == nil { return }; r.routes = append(r.routes, Route{Method: method, Path: joinPath(r.prefix, path), Handler: handler, Middleware: append(append([]MiddlewareFunc{}, r.middleware...), resolveMiddleware(middleware)...)} ) }
func (r *Router) Resource(prefix string, c ResourceController, middleware ...any) { r.Get(prefix, c.Index, middleware...); r.Get(prefix + "/:id", c.Show, middleware...); r.Post(prefix, c.Store, middleware...); r.Put(prefix + "/:id", c.Update, middleware...); r.Delete(prefix + "/:id", c.Destroy, middleware...) }
func resolveMiddleware(middleware []any) []MiddlewareFunc { resolved := make([]MiddlewareFunc, 0, len(middleware)); for _, mw := range middleware { switch v := mw.(type) { case MiddlewareFunc: resolved = append(resolved, v); case func(*Context, func() Response) Response: resolved = append(resolved, MiddlewareFunc(v)); case MiddlewareProvider: resolved = append(resolved, v.Middleware()); default: panic("invalid middleware type") } }; return resolved }
func (r *Router) AllRoutes() []Route { if r == nil { return nil }; routes := make([]Route, len(r.routes)); copy(routes, r.routes); return routes }
func writeRecoveredError(w http.ResponseWriter) { Response{StatusCode: http.StatusInternalServerError, Body: map[string]string{"error": "internal server error"}}.Write(w) }
func writeRouterBadRequest(w http.ResponseWriter) { Response{StatusCode: http.StatusBadRequest, Body: map[string]string{"error": "bad request"}}.Write(w) }
func writeRouterNotFound(w http.ResponseWriter) { Response{StatusCode: http.StatusNotFound, Body: map[string]string{"error": "not found"}}.Write(w) }
func writeRouterMethodNotAllowed(w http.ResponseWriter, allow string) { if allow != "" { w.Header().Set("Allow", allow) }; Response{StatusCode: http.StatusMethodNotAllowed, Body: map[string]string{"error": "method not allowed"}}.Write(w) }
func recoveredPanicError(_ any) error { return fmt.Errorf("panic recovered") }
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if w == nil {
		return
	}
	var ctx *Context
	var reporter ErrorReporter
	if r != nil {
		reporter = r.onError
	}
	defer func() { if recovered := recover(); recovered != nil { log.Printf("panic recovered"); if reporter != nil { reporter(ctx, recoveredPanicError(recovered)) }; writeRecoveredError(w) } }()
	if req == nil || req.URL == nil {
		writeRouterBadRequest(w)
		return
	}
	rateLimitResp, rateLimitHeaders := checkRateLimit(req)
	if rateLimitResp != nil {
		rateLimitResp.Write(w)
		return
	}
	if r == nil {
		writeRouterNotFound(w)
		return
	}
	var allowedMethods []string
	for _, rt := range r.routes {
		params, ok := matchPath(rt.Path, req.URL.Path)
		if !ok { continue }
		if rt.Method != req.Method { allowedMethods = appendAllowedMethod(allowedMethods, rt.Method); continue }
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
	if len(allowedMethods) > 0 {
		writeRouterMethodNotAllowed(w, strings.Join(allowedMethods, ", "))
		return
	}
	writeRouterNotFound(w)
}
func appendAllowedMethod(methods []string, method string) []string { for _, existing := range methods { if existing == method { return methods } }; return append(methods, method) }
var paramPattern = regexp.MustCompile(` + "`" + `:(\w+)` + "`" + `)
func (r *Router) RegisterRoutes(mux *http.ServeMux) {
	if r == nil || mux == nil {
		return
	}
	registered := map[string]bool{}
	for _, route := range r.AllRoutes() {
		goPath := paramPattern.ReplaceAllString(route.Path, "{$1}")
		pattern := route.Method + " " + goPath
		if registered[pattern] { panic("duplicate route registered: " + pattern) }
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
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 1 << 20}
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
	if rateLimitCallback != nil { rateLimitCallback(NewContext(r), RateLimitEvent{Key: key, Layer: "ip", Path: requestPath(r), RPS: globalLimiter.rps, Burst: globalLimiter.burst, Remaining: remaining, Allowed: ok}) }
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
		if rateLimitCallback != nil { rateLimitCallback(ctx, RateLimitEvent{Key: key, Layer: "ip", Path: contextPath(ctx), RPS: store.rps, Burst: store.burst, Remaining: remaining, Allowed: ok}) }
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
		if ctx.Auth().UserID != "" { key = ctx.Auth().UserID }
		if key == "" && c.keyFunc != nil { key = c.keyFunc(ctx) }
		if key == "" { key = clientIP(ctx.Request()) }
		rps, burst := c.rps, c.burst
		if role := ctx.Role(); role != "" && c.tiers != nil { if tier, ok := c.tiers[role]; ok { rps = tier.RPS; burst = tier.Burst; key = role + ":" + key } }
		bucket, ok := c.store.allowWithParams(key, rps, burst)
		remaining := bucketRemaining(bucket)
		if rateLimitCallback != nil { rateLimitCallback(ctx, RateLimitEvent{Key: key, Layer: "auth", Path: contextPath(ctx), RPS: rps, Burst: burst, Remaining: remaining, Allowed: ok}) }
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
func contextPath(ctx *Context) string { if ctx == nil { return "" }; return requestPath(ctx.Request()) }
func requestPath(r *http.Request) string { if r == nil || r.URL == nil { return "" }; return r.URL.Path }
func clientIP(r *http.Request) string { if r == nil { return "" }; remote := stripPort(r.RemoteAddr); if !proxyHeadersTrusted(remote) { return remote }; if xff := r.Header.Get("X-Forwarded-For"); xff != "" { return firstUntrustedIP(xff) }; if xri := r.Header.Get("X-Real-IP"); xri != "" { return strings.TrimSpace(xri) }; return remote }
`

const rbacMiddlewareSupportSource = `package middleware

import (
	"errors"

	"%s/app/models"
	"%s/internal/httpx"
)

var errNoRoleDatabase = errors.New("rbac: models.DB is not configured for role loading")
var errRoleDatabase = errors.New("rbac: role database error")

type roleRow struct {
	Slug    string
	Manages bool
}

func LoadRoles(ctx *httpx.Context, next func() httpx.Response) httpx.Response {
	if next == nil {
		return httpx.Response{StatusCode: 500, Body: map[string]string{"error": "internal server error"}}
	}
	if !ctx.IsAuthenticated() {
		return ctx.Unauthorized("LoadRoles requires authentication - add Auth middleware before LoadRoles")
	}
	if models.DB == nil {
		return ctx.Error(errNoRoleDatabase)
	}
	var rows []roleRow
	if err := models.DB.Raw("SELECT r.slug, r.manages FROM roles r JOIN role_user ru ON ru.role_id = r.id WHERE ru.user_id = ?", ctx.Auth().UserID).Scan(&rows).Error; err != nil {
		return ctx.Error(errRoleDatabase)
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
		if next == nil {
			return httpx.Response{StatusCode: 500, Body: map[string]string{"error": "internal server error"}}
		}
		if !ctx.HasAnyRole(roles...) {
			return ctx.Forbidden("insufficient role")
		}
		return next()
	}
}

func RequireAdmin(ctx *httpx.Context, next func() httpx.Response) httpx.Response {
	if next == nil {
		return httpx.Response{StatusCode: 500, Body: map[string]string{"error": "internal server error"}}
	}
	if !ctx.IsAdmin() {
		return ctx.Forbidden("admin access required")
	}
	return next()
}
`

const authSupportSource = `package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
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

const maxAuthorizationHeaderBytes = 12 << 10

var (
	registry = map[string]AuthDriver{}
	factories = map[string]func() AuthDriver{}
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
	registry = map[string]AuthDriver{}
	factories = map[string]func() AuthDriver{
		"jwt": func() AuthDriver { return jwt.NewDriver(envFunc, db, driver) },
		"oauth": func() AuthDriver { return oauth.NewDriver(envFunc, db, driver) },
		"session": func() AuthDriver { return session.NewDriver(envFunc, db, driver) },
	}
	if _, err := TryActiveDriver(); err != nil {
		panic("auth: active driver initialization failed")
	}
}

func Driver(name string) AuthDriver {
	d, err := TryDriver(name)
	if err != nil {
		panic("auth: driver unavailable")
	}
	return d
}

func TryDriver(name string) (AuthDriver, error) {
	d, ok := registry[name]
	if ok {
		return d, nil
	}
	factory, ok := factories[name]
	if !ok {
		return nil, fmt.Errorf("auth: unknown driver %%q", name)
	}
	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		d = factory()
	}()
	if recovered != nil {
		return nil, fmt.Errorf("auth: initialize driver %%q: %%v", name, recovered)
	}
	registry[name] = d
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
	if r != nil && len(r.Header.Get("Authorization")) > maxAuthorizationHeaderBytes {
		return nil, errors.New("auth: authorization header too large")
	}
	driver, err := TryActiveDriver()
	if err != nil {
		return nil, err
	}
	return driver.Authenticate(r)
}

func DefaultAuthMiddleware(ctx *httpx.Context, next func() httpx.Response) httpx.Response {
	info, err := Authenticate(ctx.Request())
	if err != nil {
		log.Printf("auth middleware failed")
		return ctx.Unauthorized("unauthorized")
	}
	ctx.SetAuth(info)
	if next == nil {
		log.Printf("auth middleware failed")
		return ctx.Error(errors.New("auth middleware failed"))
	}
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

const (
	maxJWTTokenBytes   = 8 << 10
	maxJWTSegmentBytes = 4 << 10
	maxJWTExpirySeconds = 365 * 24 * 60 * 60
)

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
	expiry = boundedPositiveSeconds(env("JWT_EXPIRY", ""), expiry, maxJWTExpirySeconds)
	secret := env("JWT_SECRET", "")
	alg := env("JWT_ALGORITHM", "HS256")
	if secret == "" {
		panic("jwt: JWT_SECRET is required")
	}
	minLen := map[string]int{"HS256": 32, "HS384": 48, "HS512": 64}
	if min, ok := minLen[alg]; ok && len(secret) < min {
		panic(fmt.Sprintf("jwt: JWT_SECRET must be at least %%d bytes for %%s, got %%d", min, alg, len(secret)))
	}
	return &Driver{
		db:        db,
		driver:    driver,
		secret:    secret,
		issuer:    env("JWT_ISSUER", ""),
		expiry:    expiry,
		algorithm: alg,
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
	if err := d.registerToken(claims); err != nil { return "", errors.New("jwt: database error") }
	return signingInput + "." + base64URLEncode(sig), nil
}

func (d *Driver) ValidateToken(token string) (Claims, error) {
	if d.secret == "" { return Claims{}, ErrInvalidToken }
	parts := strings.SplitN(token, ".", 3)
	if !validJWTShape(token, parts) { return Claims{}, ErrInvalidToken }
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
	if r == nil { return nil, errors.New("missing authorization header") }
	header := r.Header.Get("Authorization")
	if header == "" { return nil, errors.New("missing authorization header") }
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") { return nil, errors.New("invalid authorization header") }
	claims, err := d.ValidateToken(parts[1])
	if err != nil { return nil, err }
	return &httpx.AuthInfo{UserID: claims.Subject, Role: claims.Role, Claims: claims}, nil
}

func validJWTShape(token string, parts []string) bool {
	if token == "" || len(token) > maxJWTTokenBytes || len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || len(part) > maxJWTSegmentBytes || strings.Contains(part, ".") {
			return false
		}
	}
	return true
}

func boundedPositiveSeconds(raw string, fallback, max int) int {
	if raw == "" {
		return fallback
	}
	n := 0
	found := false
	for _, c := range raw {
		if c < '0' || c > '9' {
			continue
		}
		found = true
		digit := int(c - '0')
		if n > (max-digit)/10 {
			return max
		}
		n = n*10 + digit
		if n > max {
			return max
		}
	}
	if !found || n <= 0 {
		return fallback
	}
	return n
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
	var revokedAt sql.NullTime
	err := d.db.QueryRow(bindPlaceholders(d.driver, "SELECT expires_at, revoked_at FROM jwt_tokens WHERE jti = ? AND user_id = ?"), claims.JTI, claims.Subject).Scan(&expiresAt, &revokedAt)
	if err != nil { return err }
	if time.Now().After(expiresAt) { return errors.New("jwt: token expired") }
	if revokedAt.Valid { return errors.New("jwt: token revoked") }
	return nil
}

func (d *Driver) RevokeToken(jti string) error {
	if d.db == nil { return errors.New("jwt: database not configured") }
	if jti == "" { return errors.New("jwt: missing jti") }
	_, err := d.db.Exec(bindPlaceholders(d.driver, "UPDATE jwt_tokens SET revoked_at = ? WHERE jti = ?"), time.Now().UTC(), jti)
	if err != nil { return errors.New("jwt: database error") }
	return nil
}

func (d *Driver) RevokeAllForUser(userID string) error {
	if d.db == nil { return errors.New("jwt: database not configured") }
	if userID == "" { return errors.New("jwt: missing user id") }
	_, err := d.db.Exec(bindPlaceholders(d.driver, "UPDATE jwt_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL"), time.Now().UTC(), userID)
	if err != nil { return errors.New("jwt: database error") }
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
	default: return nil, errors.New("jwt: unsupported algorithm")
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
	"mime"
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

const maxTokenRequestBodyBytes = 8 << 10
const maxOAuthTokenExpirySeconds = 365 * 24 * 60 * 60
const maxOAuthBearerTokenBytes = 8 << 10
const maxOAuthAuthorizationHeaderBytes = 12 << 10

func NewDriver(env func(string, string) string, db *sql.DB, driver string) *Driver {
	expiry := 3600
	expiry = boundedPositiveSeconds(env("OAUTH_TOKEN_EXPIRY", ""), expiry, maxOAuthTokenExpirySeconds)
	return &Driver{db: db, driver: driver, clientID: env("OAUTH_CLIENT_ID", ""), clientSecret: env("OAUTH_CLIENT_SECRET", ""), expiry: expiry}
}

func (d *Driver) Authenticate(r *http.Request) (*httpx.AuthInfo, error) {
	if r == nil { return nil, errors.New("missing bearer token") }
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") { return nil, errors.New("missing bearer token") }
	return d.ValidateToken(h[7:])
}

func (d *Driver) ValidateToken(token string) (*httpx.AuthInfo, error) {
	if d.db == nil { return nil, errors.New("oauth: database not configured") }
	if token == "" || len(token) > maxOAuthBearerTokenBytes {
		return nil, errors.New("oauth: invalid token")
	}
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
	if r == nil {
		return ctx.JSON(400, map[string]string{"error": "invalid_request", "error_description": "missing request"})
	}
	if d.db == nil {
		return ctx.Error(errors.New("oauth: database not configured"))
	}
	if d.clientID == "" || d.clientSecret == "" {
		return ctx.Error(errors.New("oauth: client credentials not configured"))
	}
	if !isFormURLEncoded(r.Header.Get("Content-Type")) {
		return ctx.JSON(400, map[string]string{"error": "invalid_request", "error_description": "Content-Type must be application/x-www-form-urlencoded"})
	}
	if r.ContentLength > maxTokenRequestBodyBytes {
		return ctx.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "invalid_request", "error_description": "request body too large"})
	}
	if r.Body != nil {
		r.Body = http.MaxBytesReader(ctx.ResponseWriter(), r.Body, maxTokenRequestBodyBytes)
	}
	if err := r.ParseForm(); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return ctx.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "invalid_request", "error_description": "request body too large"})
		}
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
	if err != nil { return ctx.Error(errors.New("oauth: token generation error")) }
	expiresAt := time.Now().Add(time.Duration(d.expiry) * time.Second)
	if _, err := d.db.Exec(bindPlaceholders(d.driver, "INSERT INTO oauth_tokens (token, client_id, expires_at, created_at) VALUES (?, ?, ?, ?)"), token, clientID, expiresAt, time.Now().UTC()); err != nil {
		return ctx.Error(errors.New("oauth: database error"))
	}
	resp := ctx.JSON(200, map[string]any{"access_token": token, "token_type": "bearer", "expires_in": d.expiry})
	if resp.Headers == nil { resp.Headers = map[string]string{} }
	resp.Headers["Cache-Control"] = "no-store"
	resp.Headers["Pragma"] = "no-cache"
	return resp
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

func isFormURLEncoded(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return mediaType == "application/x-www-form-urlencoded"
}

func parseBasicAuth(header string) (string, string, bool) {
	if len(header) > maxOAuthAuthorizationHeaderBytes { return "", "", false }
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

func boundedPositiveSeconds(raw string, fallback, max int) int {
	if raw == "" {
		return fallback
	}
	n := 0
	found := false
	for _, c := range raw {
		if c < '0' || c > '9' {
			continue
		}
		found = true
		digit := int(c - '0')
		if n > (max-digit)/10 {
			return max
		}
		n = n*10 + digit
		if n > max {
			return max
		}
	}
	if !found || n <= 0 {
		return fallback
	}
	return n
}
`

const sessionSupportSource = `package session

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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
var csrfNonceReader io.Reader = rand.Reader

const maxSessionCookieNameBytes = 64
const maxSessionTTLSeconds = 365 * 24 * 60 * 60

type Driver struct {
	db *sql.DB
	driver string
	cookieName string
	ttl int
}

func NewDriver(env func(string, string) string, db *sql.DB, driver string) *Driver {
	ttl := 86400
	ttl = boundedPositiveSeconds(env("SESSION_TTL", ""), ttl, maxSessionTTLSeconds)
	cookieName := env("SESSION_COOKIE", "session_id")
	csrfCookieName := env("CSRF_COOKIE", "csrf_token")
	if !validCookieName(cookieName) { panic("session: invalid SESSION_COOKIE") }
	if !validCookieName(csrfCookieName) { panic("session: invalid CSRF_COOKIE") }
	sessionCookieName = cookieName
	csrfConfig.cookieName = csrfCookieName
	if secret := env("SESSION_SECRET", ""); secret != "" { csrfConfig.secret = []byte(secret) } else { csrfConfig.secret = nil }
	d := &Driver{db: db, driver: driver, cookieName: cookieName, ttl: ttl}
	activeDriver = d
	return d
}

func (d *Driver) Authenticate(r *http.Request) (*httpx.AuthInfo, error) {
	if r == nil { return nil, errors.New("session: missing session cookie") }
	cookie, err := r.Cookie(d.cookieName)
	if err != nil { return nil, errors.New("session: missing session cookie") }
	if !validSessionID(cookie.Value) { return nil, errors.New("session: invalid or expired session") }
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

func Create(ctx *httpx.Context, userID, role string) (*SessionCookies, error) {
	_ = ctx
	d := driver()
	if d.db == nil {
		return nil, errors.New("session: database not configured")
	}
	sessionID := uuid.New().String()
	expiresAt := time.Now().UTC().Add(time.Duration(d.ttl) * time.Second)
	if _, err := d.db.Exec(bindPlaceholders(d.driver, "INSERT INTO sessions (id, user_id, role, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)"), sessionID, userID, role, expiresAt, time.Now().UTC(), time.Now().UTC()); err != nil {
		return nil, errors.New("session: database error")
	}
	cookies := &SessionCookies{
		Session: &http.Cookie{Name: d.cookieName, Value: sessionID, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, Expires: expiresAt},
	}
	if len(csrfConfig.secret) > 0 {
		cookies.CSRF = newCSRFCookie(sessionID)
	}
	return cookies, nil
}

type SessionCookies struct {
	Session *http.Cookie
	CSRF    *http.Cookie
}

func (sc *SessionCookies) Apply(resp httpx.Response) httpx.Response {
	if sc == nil {
		return resp
	}
	resp = resp.WithCookie(sc.Session)
	if sc.CSRF != nil {
		resp = resp.WithCookie(sc.CSRF)
	}
	return resp
}

func Destroy(ctx *httpx.Context) (httpx.Response, error) {
	d := driver()
	if d.db == nil {
		return httpx.Response{}, errors.New("session: database not configured")
	}
	sessionID, err := ctx.Cookie(d.cookieName)
	if err != nil {
		return httpx.Response{}, errors.New("session: no session cookie")
	}
	if !validSessionID(sessionID) {
		return httpx.Response{}, errors.New("session: invalid or expired session")
	}
	if _, err := d.db.Exec(bindPlaceholders(d.driver, "DELETE FROM sessions WHERE id = ?"), sessionID); err != nil {
		return httpx.Response{}, errors.New("session: database error")
	}
	expired := time.Unix(0, 0)
	resp := ctx.NoContent().
		WithCookie(&http.Cookie{Name: d.cookieName, Value: "", Path: "/", Expires: expired, MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode}).
		WithCookie(&http.Cookie{Name: csrfConfig.cookieName, Value: "", Path: "/", Expires: expired, MaxAge: -1, Secure: true, SameSite: http.SameSiteStrictMode})
	return resp, nil
}

func Get(ctx *httpx.Context, key string) (string, error) {
	d := driver()
	if d.db == nil {
		return "", errors.New("session: database not configured")
	}
	sessionID, err := ctx.Cookie(d.cookieName)
	if err != nil {
		return "", errors.New("session: no session cookie")
	}
	if !validSessionID(sessionID) {
		return "", errors.New("session: invalid or expired session")
	}
	var payloadRaw sql.NullString
	var expiresAt time.Time
	err = d.db.QueryRow(bindPlaceholders(d.driver, "SELECT payload, expires_at FROM sessions WHERE id = ?"), sessionID).Scan(&payloadRaw, &expiresAt)
	if err == sql.ErrNoRows {
		return "", errors.New("session: invalid or expired session")
	}
	if err != nil {
		return "", errors.New("session: database error")
	}
	if time.Now().After(expiresAt) {
		return "", errors.New("session: expired session")
	}
	if !payloadRaw.Valid || payloadRaw.String == "" {
		return "", nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadRaw.String), &payload); err != nil {
		return "", errors.New("session: invalid session payload")
	}
	val, ok := payload[key]
	if !ok {
		return "", nil
	}
	if s, ok := val.(string); ok {
		return s, nil
	}
	data, err := json.Marshal(val)
	if err != nil {
		return "", errors.New("session: invalid session payload")
	}
	return string(data), nil
}

func Put(ctx *httpx.Context, key string, value any) error {
	d := driver()
	if d.db == nil {
		return errors.New("session: database not configured")
	}
	if key == "" {
		return errors.New("session: key must not be empty")
	}
	sessionID, err := ctx.Cookie(d.cookieName)
	if err != nil {
		return errors.New("session: no session cookie")
	}
	if !validSessionID(sessionID) {
		return errors.New("session: invalid or expired session")
	}
	var payloadRaw sql.NullString
	var expiresAt time.Time
	err = d.db.QueryRow(bindPlaceholders(d.driver, "SELECT payload, expires_at FROM sessions WHERE id = ?"), sessionID).Scan(&payloadRaw, &expiresAt)
	if err == sql.ErrNoRows {
		return errors.New("session: invalid or expired session")
	}
	if err != nil {
		return errors.New("session: database error")
	}
	if time.Now().After(expiresAt) {
		return errors.New("session: expired session")
	}
	payload := map[string]any{}
	if payloadRaw.Valid && payloadRaw.String != "" {
		if err := json.Unmarshal([]byte(payloadRaw.String), &payload); err != nil {
			return errors.New("session: invalid session payload")
		}
	}
	payload[key] = value
	data, err := json.Marshal(payload)
	if err != nil {
		return errors.New("session: invalid session value")
	}
	res, err := d.db.Exec(bindPlaceholders(d.driver, "UPDATE sessions SET payload = ?, updated_at = ? WHERE id = ?"), string(data), time.Now().UTC(), sessionID)
	if err != nil {
		return errors.New("session: database error")
	}
	if rows, err := res.RowsAffected(); err == nil && rows == 0 {
		return errors.New("session: invalid or expired session")
	}
	return nil
}

func CSRF(ctx *httpx.Context, next func() httpx.Response) httpx.Response {
	if len(csrfConfig.secret) == 0 { return ctx.Forbidden("CSRF secret not configured") }
	r := ctx.Request()
	if r == nil { return ctx.Forbidden("CSRF request missing") }
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") { return next() }
	sessionID := sessionIDFromRequest(r)
	method := r.Method
	if method == "GET" || method == "HEAD" || method == "OPTIONS" {
		resp := next()
		if sessionID != "" {
			if _, err := ctx.Cookie(csrfConfig.cookieName); err != nil { resp = resp.WithCookie(newCSRFCookie(sessionID)) }
		}
		return resp
	}
	if sessionID == "" { return ctx.Forbidden("CSRF session missing") }
	token := r.Header.Get("X-CSRF-TOKEN")
	if token == "" { return ctx.Forbidden("CSRF token missing") }
	if !validateCSRFToken(token, sessionID, csrfConfig.secret) { return ctx.Forbidden("CSRF token invalid") }
	return next()
}

func sessionIDFromRequest(r *http.Request) string {
	if r == nil { return "" }
	c, err := r.Cookie(sessionCookieName)
	if err != nil { return "" }
	if !validSessionID(c.Value) { return "" }
	return c.Value
}

func validSessionID(sessionID string) bool {
	if len(sessionID) != 36 {
		return false
	}
	_, err := uuid.Parse(sessionID)
	return err == nil
}

func validCookieName(name string) bool {
	if name == "" || len(name) > maxSessionCookieNameBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			continue
		}
		switch c {
		case '!', '#', '$', '%%', '&', '\'', '*', '+', '-', '.', '^', '_', 0x60, '|', '~':
			continue
		default:
			return false
		}
	}
	return true
}

func boundedPositiveSeconds(raw string, fallback, max int) int {
	if raw == "" {
		return fallback
	}
	n := 0
	found := false
	for _, c := range raw {
		if c < '0' || c > '9' {
			continue
		}
		found = true
		digit := int(c - '0')
		if n > (max-digit)/10 {
			return max
		}
		n = n*10 + digit
		if n > max {
			return max
		}
	}
	if !found || n <= 0 {
		return fallback
	}
	return n
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
	if _, err := io.ReadFull(csrfNonceReader, nonce); err != nil { panic("csrf: failed to generate random nonce") }
	return hex.EncodeToString(nonce) + "." + hex.EncodeToString(computeHMAC(nonce, []byte(sessionID), secret))
}

func validateCSRFToken(token, sessionID string, secret []byte) bool {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 { return false }
	if len(parts[0]) != 64 || len(parts[1]) != 64 { return false }
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
	"errors"
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

func Cron(fn func(s *Scheduler)) *Scheduler {
	s := &Scheduler{}
	if fn != nil {
		fn(s)
	}
	return s
}

func (s *Scheduler) Job(schedule string, job Job) *JobEntry {
	e := &JobEntry{Schedule: schedule, Job: job}
	if s != nil {
		s.entries = append(s.entries, e)
	}
	return e
}

func (s *Scheduler) Entries() []*JobEntry {
	if s == nil {
		return nil
	}
	entries := make([]*JobEntry, len(s.entries))
	copy(entries, s.entries)
	return entries
}

func (e *JobEntry) MaxRetries(n int) *JobEntry {
	if e == nil {
		return e
	}
	if n < 0 {
		n = 0
	}
	e.maxRetries = n
	return e
}
func (e *JobEntry) RetryDelay(d time.Duration) *JobEntry {
	if e == nil {
		return e
	}
	if d < 0 {
		d = 0
	}
	e.retryDelay = d
	return e
}
func (e *JobEntry) Timeout(d time.Duration) *JobEntry {
	if e == nil {
		return e
	}
	if d < 0 {
		d = 0
	}
	e.timeout = d
	return e
}
func (e *JobEntry) SkipIfRunning() *JobEntry {
	if e != nil {
		e.allowOverlap = false
	}
	return e
}
func (e *JobEntry) AllowOverlap() *JobEntry {
	if e != nil {
		e.allowOverlap = true
	}
	return e
}

func (s *Scheduler) Start(ctx context.Context) {
	if s == nil || ctx == nil {
		return
	}
	c := cron.New()
	for _, entry := range s.entries {
		entry := entry
		if entry == nil {
			continue
		}
		var mu sync.Mutex
		running := false
		_, err := c.AddFunc(entry.Schedule, func() {
			if !entry.allowOverlap {
				mu.Lock()
				if running { mu.Unlock(); log.Printf("job skipped: previous run still in progress"); return }
				running = true
				mu.Unlock()
				defer func() { mu.Lock(); running = false; mu.Unlock() }()
			}
			runJob(entry)
		})
		if err != nil { log.Printf("job schedule rejected") }
	}
	c.Start()
	<-ctx.Done()
	c.Stop()
}

func runJob(entry *JobEntry) {
	if entry == nil {
		log.Printf("job failed (attempt 1/1): job failed")
		return
	}
	attempts := entry.maxRetries + 1
	for i := 0; i < attempts; i++ {
		var err error
		if entry.timeout > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), entry.timeout)
			done := make(chan error, 1)
			go func() { done <- safeHandleJob(entry.Job) }()
			select { case err = <-done: case <-ctx.Done(): err = errors.New("job timeout") }
			cancel()
		} else { err = safeHandleJob(entry.Job) }
		if err == nil { return }
		log.Printf("job failed (attempt %d/%d): job failed", i+1, attempts)
		if i < attempts-1 && entry.retryDelay > 0 { time.Sleep(entry.retryDelay) }
	}
}

func safeHandleJob(job Job) (err error) {
	if job == nil {
		return errors.New("job failed")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errors.New("job panic")
		}
	}()
	return job.Handle()
}
`

const serverMainSource = `package main

import (
	"log"
	"net/http"
	"time"

	"%s/config"
	"%s/routes"
)

func main() {
	config.Init()
	addr := ":" + config.App.Port
	log.Printf("listening on %%s", addr)
	mux := http.NewServeMux()
	routes.API.RegisterRoutes(mux)
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 1 << 20}
	if err := server.ListenAndServe(); err != nil {
		log.Fatal("server failed")
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
	"time"

	"%s/app/models"
	"%s/config"
	"%s/routes"
)

func main() {
	config.Init()
	conn := config.Database.Connection()
	models.SetDBWithDriver(config.OpenGORM(conn), conn.Driver)
	addr := ":" + config.App.Port
	log.Printf("listening on %%s", addr)
	mux := http.NewServeMux()
	routes.API.RegisterRoutes(mux)
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 1 << 20}
	if err := server.ListenAndServe(); err != nil {
		log.Fatal("server failed")
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

	"gorm.io/gorm"
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

func EncryptedWhereScope(column, op string, value any) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		encryptedValue, err := EncryptDeterministicFilterValue(value)
		if err != nil {
			db.AddError(err)
			return db.Where("1 = 0")
		}
		return db.Where(column+" "+op+" ?", encryptedValue)
	}
}

func UnsupportedQueryFilterScope(message string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		db.AddError(errors.New(message))
		return db.Where("1 = 0")
	}
}

func EncryptDeterministicFilterValue(value any) (any, error) {
	key, err := exportedEncryptionKey()
	if err != nil {
		return nil, err
	}
	switch v := value.(type) {
	case string:
		return encryptDeterministic(key, []byte(v))
	case *string:
		if v == nil {
			return nil, nil
		}
		return encryptDeterministic(key, []byte(*v))
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			encrypted, err := encryptDeterministic(key, []byte(item))
			if err != nil {
				return nil, err
			}
			out = append(out, encrypted)
		}
		return out, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, errors.New("encrypted filter values must be strings")
			}
			encrypted, err := encryptDeterministic(key, []byte(s))
			if err != nil {
				return nil, err
			}
			out = append(out, encrypted)
		}
		return out, nil
	default:
		return nil, errors.New("encrypted filter value must be a string or []string")
	}
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
func integrityDatabaseError() error {
	return errors.New("integrity database error")
}

func createIntegrityRecord(db *gorm.DB, table string, record any, immutable bool, order string, columns []string) error {
	if db == nil {
		return integrityDatabaseError()
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
	if err := db.Create(record).Error; err != nil {
		return integrityDatabaseError()
	}
	return nil
}

func updateImmutableRecord(db *gorm.DB, table string, record any, order string, columns []string) error {
	if db == nil {
		return integrityDatabaseError()
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
		return integrityDatabaseError()
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
	if err := db.Create(record).Error; err != nil {
		return integrityDatabaseError()
	}
	return nil
}

func latestIntegrityHash(db *gorm.DB, table, order string) ([]byte, error) {
	var row struct { RowHash []byte ` + "`" + `gorm:"column:row_hash"` + "`" + ` }
	if err := db.Table(table).Select("row_hash").Order(order).Limit(1).Scan(&row).Error; err != nil {
		return nil, integrityDatabaseError()
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
