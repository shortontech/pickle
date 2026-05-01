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
	ex.models = modelSet(tables)

	if err := ex.writeGoMod(); err != nil {
		return nil, err
	}
	if err := ex.copyAndRewriteUserSource(); err != nil {
		return nil, err
	}
	if err := ex.writeHTTPX(); err != nil {
		return nil, err
	}
	if err := ex.writeModels(tables); err != nil {
		return nil, err
	}
	if err := ex.writeSQLMigrations(tables); err != nil {
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
	content := fmt.Sprintf("module %s\n\ngo 1.24.0\n\nrequire (\n\tgithub.com/go-playground/validator/v10 v10.30.1\n\tgithub.com/google/uuid v1.6.0\n\tgorm.io/gorm v1.31.1\n)\n", e.modulePath)
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
	if strings.HasSuffix(base, "_gen.go") || strings.HasSuffix(base, "_query.go") {
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
		for i, stmt := range block.List {
			rewritten, err := e.rewriteStmt(path, fset, stmt)
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

func (e *exporter) rewriteStmt(path string, fset *token.FileSet, stmt ast.Stmt) (ast.Stmt, error) {
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		if len(s.Rhs) == 1 {
			call, ok := s.Rhs[0].(*ast.CallExpr)
			if ok {
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
	case *ast.IfStmt:
		if s.Init != nil {
			rewritten, err := e.rewriteStmt(path, fset, s.Init)
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
		methods = append(methods, struct {
			name string
			args []ast.Expr
		}{name: sel.Sel.Name, args: c.Args})
		if root, ok := sel.X.(*ast.SelectorExpr); ok {
			if id, ok := root.X.(*ast.Ident); ok && id.Name == "models" && strings.HasPrefix(root.Sel.Name, "Query") {
				return buildQueryChain(strings.TrimPrefix(root.Sel.Name, "Query"), methods)
			}
		}
		if rootCall, ok := sel.X.(*ast.CallExpr); ok {
			if root, ok := rootCall.Fun.(*ast.SelectorExpr); ok {
				if id, ok := root.X.(*ast.Ident); ok && id.Name == "models" && strings.HasPrefix(root.Sel.Name, "Query") {
					return buildQueryChain(strings.TrimPrefix(root.Sel.Name, "Query"), methods)
				}
			}
		}
		cur = sel.X
	}
}

func buildQueryChain(model string, methods []struct {
	name string
	args []ast.Expr
}) (queryChain, bool, error) {
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
		case m.name == "First" || m.name == "All":
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
	if qc.Terminal == "" {
		return qc, true, fmt.Errorf("query chain has no terminal operation")
	}
	return qc, true, nil
}

func (e *exporter) gormExpr(q queryChain) (ast.Expr, error) {
	if !e.models[q.Model] {
		return nil, fmt.Errorf("unknown exported model %s", q.Model)
	}
	switch q.Terminal {
	case "First":
		return parseExpr(fmt.Sprintf("func() (*models.%s, error) { var record models.%s; err := %s.First(&record).Error; return &record, err }()", q.Model, q.Model, q.gormChain()))
	case "All":
		return parseExpr(fmt.Sprintf("func() ([]models.%s, error) { var records []models.%s; err := %s.Find(&records).Error; return records, err }()", q.Model, q.Model, q.gormChain()))
	case "Create":
		arg, err := exprString(q.Arg)
		if err != nil {
			return nil, err
		}
		return parseExpr(fmt.Sprintf("models.DB.Create(%s).Error", arg))
	case "Update":
		arg, err := exprString(q.Arg)
		if err != nil {
			return nil, err
		}
		return parseExpr(fmt.Sprintf("models.DB.Save(%s).Error", arg))
	case "Delete":
		arg, err := exprString(q.Arg)
		if err != nil {
			return nil, err
		}
		return parseExpr(fmt.Sprintf("models.DB.Delete(%s).Error", arg))
	default:
		return nil, fmt.Errorf("unsupported terminal query method %s", q.Terminal)
	}
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

func (e *exporter) writeModels(tables []*schema.Table) error {
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
	return nil
}

func (e *exporter) writeSQLMigrations(tables []*schema.Table) error {
	migrations, err := e.generateSQLMigrations(tables)
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

func (e *exporter) generateSQLMigrations(tables []*schema.Table) ([]sqlMigration, error) {
	byName := map[string]*schema.Table{}
	for _, table := range tables {
		byName[table.Name] = table
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
			out = append(out, sqlMigration{Name: exportName, Up: "-- TODO(export): lower database view definition from Pickle migration.\n", Down: "-- TODO(export): drop exported database view.\n"})
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
	requests, err := generator.ScanRequests(e.project.Layout.RequestsDir)
	if err != nil {
		if os.IsNotExist(err) {
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
	if err := e.writeFile(filepath.Join("app", "http", "auth", "auth.go"), []byte(fmt.Sprintf(authSupportSource, e.modulePath, e.modulePath))); err != nil {
		return err
	}
	return e.writeFile(filepath.Join("app", "http", "auth", "jwt", "jwt.go"), []byte(jwtSupportSource))
}

func (e *exporter) writeServerMain() error {
	return e.writeFile(filepath.Join("cmd", "server", "main.go"), []byte(serverMainSource))
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
		case schema.String, schema.Text, schema.Decimal, schema.JSONB, schema.Binary:
			return "string", ""
		case schema.Integer:
			return "int", ""
		case schema.BigInteger:
			return "int64", ""
		case schema.Boolean:
			return "bool", ""
		case schema.Timestamp, schema.Date, schema.Time:
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
	if len(out) == len(fields) {
		return nil
	}
	return out
}

func generateBindings(requests []generator.RequestDef) ([]byte, error) {
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
)

type Controller struct{}
type Response struct { Status int; Body any; Headers map[string]string }
type AuthInfo struct { UserID string; Role string }
type Context struct { request *http.Request; auth *AuthInfo; params map[string]string }

func NewContext(r *http.Request) *Context { return &Context{request: r, params: map[string]string{}} }
func (c *Context) Request() *http.Request { return c.request }
func (c *Context) Param(name string) string { return c.params[name] }
func (c *Context) Auth() *AuthInfo { if c.auth == nil { return &AuthInfo{} }; return c.auth }
func (c *Context) SetAuth(info *AuthInfo) { c.auth = info }
func (c *Context) JSON(status int, body any) Response { return Response{Status: status, Body: body} }
func (c *Context) Error(err error) Response { return c.JSON(500, map[string]string{"error": err.Error()}) }
func (c *Context) Unauthorized(msg string) Response { return c.JSON(401, map[string]string{"error": msg}) }
func (c *Context) NotFound(msg string) Response { return c.JSON(404, map[string]string{"error": msg}) }
func (c *Context) NoContent() Response { return Response{Status: 204} }

func (r Response) Write(w http.ResponseWriter) { for k, v := range r.Headers { w.Header().Set(k, v) }; if r.Status == 0 { r.Status = 200 }; w.WriteHeader(r.Status); if r.Body != nil { _ = json.NewEncoder(w).Encode(r.Body) } }

type HandlerFunc func(*Context) Response
type MiddlewareFunc func(*Context, func() Response) Response
type Router struct{}
func Routes(fn func(*Router)) *Router { r := &Router{}; fn(r); return r }
func (r *Router) Group(path string, args ...any) {}
func (r *Router) Get(path string, handler HandlerFunc, middleware ...MiddlewareFunc) {}
func (r *Router) Post(path string, handler HandlerFunc, middleware ...MiddlewareFunc) {}
func (r *Router) Put(path string, handler HandlerFunc, middleware ...MiddlewareFunc) {}
func (r *Router) Patch(path string, handler HandlerFunc, middleware ...MiddlewareFunc) {}
func (r *Router) Delete(path string, handler HandlerFunc, middleware ...MiddlewareFunc) {}
func RateLimit(rps, burst int) MiddlewareFunc { return func(ctx *Context, next func() Response) Response { return next() } }
`

const authSupportSource = `package auth

import (
	"errors"
	"net/http"
	"strings"

	"%s/app/http/auth/jwt"
	"%s/internal/httpx"
)

var jwtDriver = &jwt.Driver{}

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

import "errors"

type Claims struct {
	Subject string
	Role string
}

type Driver struct{}

func (d *Driver) SignToken(claims Claims) (string, error) {
	if claims.Subject == "" { return "", errors.New("jwt: missing subject") }
	return claims.Subject, nil
}

func (d *Driver) ValidateToken(token string) (Claims, error) {
	if token == "" { return Claims{}, errors.New("jwt: invalid token") }
	return Claims{Subject: token, Role: "user"}, nil
}
`

const serverMainSource = `package main

import "fmt"

func main() {
	fmt.Println("exported app: wire database and HTTP server startup here")
}
`
