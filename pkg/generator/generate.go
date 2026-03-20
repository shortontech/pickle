package generator

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/shortontech/pickle/pkg/schema"
	"github.com/shortontech/pickle/pkg/tickle"
)

// Layout describes where generated and user-written files live.
// Follows Laravel conventions: app/http/, app/models/, database/migrations/.
type Layout struct {
	HTTPDir       string // absolute path: where pickle_gen.go (Context, Response, Router) goes
	HTTPPkg       string // package name for HTTPDir ("pickle")
	RequestsDir   string // absolute path: where request structs + bindings_gen.go live
	ModelsDir     string // absolute path: where generated models live
	MigrationsDir string // absolute path: where migration files live
	MigrationsRel string // relative to module root (e.g. "database/migrations")
	ConfigDir     string // absolute path: where config files live
	CommandsDir   string // absolute path: where app/commands/ lives
	AuthDir       string // absolute path: where app/http/auth/ lives
}

// Project represents a Pickle project layout rooted at a directory.
type Project struct {
	Dir        string // project root
	ModulePath string // Go module path from go.mod
	Layout     Layout
}

// DetectProject finds the project layout from the given directory.
func DetectProject(dir string) (*Project, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}

	modPath, err := readModulePath(filepath.Join(absDir, "go.mod"))
	if err != nil {
		return nil, fmt.Errorf("reading go.mod: %w", err)
	}

	return &Project{
		Dir:        absDir,
		ModulePath: modPath,
		Layout: Layout{
			HTTPDir:       filepath.Join(absDir, "app", "http"),
			HTTPPkg:       "pickle",
			RequestsDir:   filepath.Join(absDir, "app", "http", "requests"),
			ModelsDir:     filepath.Join(absDir, "app", "models"),
			MigrationsDir: filepath.Join(absDir, "database", "migrations"),
			MigrationsRel: "database/migrations",
			ConfigDir:     filepath.Join(absDir, "config"),
			CommandsDir:   filepath.Join(absDir, "app", "commands"),
			AuthDir:       filepath.Join(absDir, "app", "http", "auth"),
		},
	}, nil
}

func readModulePath(goModPath string) (string, error) {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return "", err
	}
	re := regexp.MustCompile(`(?m)^module\s+(\S+)`)
	match := re.FindSubmatch(data)
	if match == nil {
		return "", fmt.Errorf("no module directive found in %s", goModPath)
	}
	return string(match[1]), nil
}

// ScanMigrationStructs parses Go files in the migrations/ directory and
// returns the struct names that embed Migration (sorted alphabetically).
func ScanMigrationStructs(migrationsDir string) ([]string, error) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return nil, err
	}

	var structs []string

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}

		path := filepath.Join(migrationsDir, e.Name())
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}

		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				// Check if it embeds Migration
				for _, field := range st.Fields.List {
					if len(field.Names) == 0 {
						if ident, ok := field.Type.(*ast.Ident); ok && ident.Name == "Migration" {
							structs = append(structs, ts.Name.Name)
						}
					}
				}
			}
		}
	}

	sort.Strings(structs)
	return structs, nil
}

// inspectorTableInfo mirrors the JSON output from the schema inspector program.
type inspectorTableInfo struct {
	Name          string                `json:"name"`
	Connection    string                `json:"connection,omitempty"`
	Columns       []inspectorColumnInfo `json:"columns"`
	Indexes       []inspectorIndexInfo  `json:"indexes,omitempty"`
	IsImmutable   bool                  `json:"is_immutable,omitempty"`
	IsAppendOnly  bool                  `json:"is_append_only,omitempty"`
	HasSoftDelete bool                  `json:"has_soft_delete,omitempty"`
}

type inspectorColumnInfo struct {
	Name             string `json:"name"`
	Type             string `json:"type"`
	GoType           string `json:"go_type"`
	Nullable         bool   `json:"nullable"`
	PrimaryKey       bool   `json:"primary_key,omitempty"`
	Unique           bool   `json:"unique,omitempty"`
	Default          any    `json:"default,omitempty"`
	ForeignKeyTable  string `json:"foreign_key_table,omitempty"`
	ForeignKeyColumn string `json:"foreign_key_column,omitempty"`
	Length           int    `json:"length,omitempty"`
	Precision        int    `json:"precision,omitempty"`
	Scale            int    `json:"scale,omitempty"`
	Public           bool   `json:"public,omitempty"`
	OwnerSees        bool   `json:"owner_sees,omitempty"`
	OwnerColumn      bool   `json:"owner_column,omitempty"`
	Encrypted        bool   `json:"encrypted,omitempty"`
	UnsafePublic     bool   `json:"unsafe_public,omitempty"`
}

type inspectorIndexInfo struct {
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
}

type inspectorViewInfo struct {
	Name    string                    `json:"name"`
	Sources []inspectorViewSourceInfo `json:"sources"`
	Columns []inspectorViewColumnInfo `json:"columns"`
	GroupBy []string                  `json:"group_by,omitempty"`
}

type inspectorViewSourceInfo struct {
	Table         string `json:"table"`
	Alias         string `json:"alias"`
	JoinType      string `json:"join_type,omitempty"`
	JoinCondition string `json:"join_condition,omitempty"`
}

type inspectorViewColumnInfo struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	GoType       string `json:"go_type"`
	Nullable     bool   `json:"nullable"`
	SourceAlias  string `json:"source_alias,omitempty"`
	SourceColumn string `json:"source_column,omitempty"`
	RawExpr      string `json:"raw_expr,omitempty"`
	Precision    int    `json:"precision,omitempty"`
	Scale        int    `json:"scale,omitempty"`
}

type inspectorRelationshipInfo struct {
	Type        string `json:"type"`
	ParentTable string `json:"parent_table"`
	ChildTable  string `json:"child_table"`
	Collection  bool   `json:"collection,omitempty"`
	TopLevel    bool   `json:"top_level,omitempty"`
}

type inspectorOutput struct {
	Tables        []inspectorTableInfo        `json:"tables"`
	Views         []inspectorViewInfo         `json:"views,omitempty"`
	Relationships []inspectorRelationshipInfo `json:"relationships,omitempty"`
}

var typeNameToColumnType = map[string]schema.ColumnType{
	"uuid":       schema.UUID,
	"string":     schema.String,
	"text":       schema.Text,
	"integer":    schema.Integer,
	"biginteger": schema.BigInteger,
	"decimal":    schema.Decimal,
	"boolean":    schema.Boolean,
	"timestamp":  schema.Timestamp,
	"jsonb":      schema.JSONB,
	"date":       schema.Date,
	"time":       schema.Time,
	"binary":     schema.Binary,
}

// SchemaRelationship describes a parent-child nesting from the inspector output.
type SchemaRelationship struct {
	Type        string // "has_many" or "has_one"
	ParentTable string
	ChildTable  string
	Collection  bool
	TopLevel    bool
}

// RunSchemaInspector generates a temp inspector program, compiles and runs it,
// and returns the parsed schema tables, views, and relationships.
func RunSchemaInspector(project *Project) ([]*schema.Table, []*schema.View, []SchemaRelationship, error) {
	migrationsDir := project.Layout.MigrationsDir
	structNames, err := ScanMigrationStructs(migrationsDir)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("scanning migrations: %w", err)
	}

	if len(structNames) == 0 {
		return nil, nil, nil, nil
	}

	migrationsImport := project.ModulePath + "/" + project.Layout.MigrationsRel
	var entries []MigrationEntry
	for _, name := range structNames {
		entries = append(entries, MigrationEntry{StructName: name, ImportPath: migrationsImport})
	}

	inspectorSrc, err := GenerateSchemaInspector(entries)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generating inspector: %w", err)
	}

	// Write to a temp directory inside the project so it can resolve local imports
	tmpDir := filepath.Join(project.Dir, ".pickle-tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return nil, nil, nil, fmt.Errorf("creating temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	inspectorPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(inspectorPath, inspectorSrc, 0o644); err != nil {
		return nil, nil, nil, fmt.Errorf("writing inspector: %w", err)
	}

	cmd := exec.Command("go", "run", inspectorPath, "--json")
	cmd.Dir = project.Dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("running inspector: %w\n%s", err, output)
	}

	var result inspectorOutput
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, nil, nil, fmt.Errorf("parsing inspector output: %w\n%s", err, output)
	}

	// Convert to schema.Table
	var tables []*schema.Table
	for _, ti := range result.Tables {
		t := &schema.Table{Name: ti.Name, Connection: ti.Connection, IsImmutable: ti.IsImmutable, IsAppendOnly: ti.IsAppendOnly, HasSoftDelete: ti.HasSoftDelete}
		for _, ci := range ti.Columns {
			colType, ok := typeNameToColumnType[ci.Type]
			if !ok {
				return nil, nil, nil, fmt.Errorf("unknown column type %q for column %s.%s", ci.Type, ti.Name, ci.Name)
			}
			col := &schema.Column{
				Name:             ci.Name,
				Type:             colType,
				IsNullable:       ci.Nullable,
				IsPrimaryKey:     ci.PrimaryKey,
				IsUnique:         ci.Unique,
				ForeignKeyTable:  ci.ForeignKeyTable,
				ForeignKeyColumn: ci.ForeignKeyColumn,
				Length:           ci.Length,
				Precision:        ci.Precision,
				Scale:            ci.Scale,
				IsPublic:         ci.Public,
				IsOwnerSees:      ci.OwnerSees,
				IsOwnerColumn:    ci.OwnerColumn,
				IsEncrypted:      ci.Encrypted,
				IsUnsafePublic:   ci.UnsafePublic,
			}
			if ci.Default != nil {
				col.DefaultValue = ci.Default
			}
			t.Columns = append(t.Columns, col)
		}
		tables = append(tables, t)
	}

	// Convert to schema.View
	var views []*schema.View
	for _, vi := range result.Views {
		v := &schema.View{Name: vi.Name, GroupByCols: vi.GroupBy}
		for _, src := range vi.Sources {
			v.Sources = append(v.Sources, schema.ViewSource{
				Table:         src.Table,
				Alias:         src.Alias,
				JoinType:      src.JoinType,
				JoinCondition: src.JoinCondition,
			})
		}
		for _, ci := range vi.Columns {
			vc := &schema.ViewColumn{
				SourceAlias:  ci.SourceAlias,
				SourceColumn: ci.SourceColumn,
				RawExpr:      ci.RawExpr,
			}
			vc.Name = ci.Name
			vc.Type = typeNameToColumnType[ci.Type]
			vc.IsNullable = ci.Nullable
			vc.Precision = ci.Precision
			vc.Scale = ci.Scale
			v.Columns = append(v.Columns, vc)
		}
		views = append(views, v)
	}

	// Convert relationships
	var rels []SchemaRelationship
	for _, ri := range result.Relationships {
		rels = append(rels, SchemaRelationship{
			Type:        ri.Type,
			ParentTable: ri.ParentTable,
			ChildTable:  ri.ChildTable,
			Collection:  ri.Collection,
			TopLevel:    ri.TopLevel,
		})
	}

	return tables, views, rels, nil
}


// Generate runs all generators for a project and writes output.
//
// Layout (Laravel-style):
//   - {root}/app/http/pickle_gen.go         — HTTP types (Context, Response, Router, etc.)
//   - {root}/app/http/requests/bindings_gen.go — Request deserialization + validation
//   - {root}/app/models/pickle_gen.go       — QueryBuilder[T]
//   - {root}/app/models/*.go                — Model structs and query scopes
//   - {root}/database/migrations/types_gen.go — Schema DSL types (Migration, Table, etc.)
//   - {root}/config/pickle_gen.go           — Config glue
func Generate(project *Project, picklePkgDir string) error {
	layout := project.Layout
	modelsDir := layout.ModelsDir
	migrationsDir := layout.MigrationsDir
	configDir := layout.ConfigDir
	requestsDir := layout.RequestsDir
	httpPkg := layout.HTTPPkg

	// 0. Generate config glue if config/ exists
	if _, err := os.Stat(configDir); err == nil {
		scan, err := ScanConfigs(configDir)
		if err != nil {
			return fmt.Errorf("scanning config: %w", err)
		}

		if len(scan.Configs) > 0 {
			fmt.Println("  generating config/pickle_gen.go")
			configSrc, err := GenerateConfigGlue(scan, "config")
			if err != nil {
				return fmt.Errorf("generating config glue: %w", err)
			}
			if err := writeFile(filepath.Join(configDir, "pickle_gen.go"), configSrc); err != nil {
				return err
			}
		}
	}

	// 1. Write pre-tickled core types
	fmt.Println("  generating pickle_gen.go")
	if err := writeFile(filepath.Join(layout.HTTPDir, "pickle_gen.go"), GenerateCoreHTTP(httpPkg)); err != nil {
		return err
	}

	fmt.Println("  generating models/pickle_gen.go")
	if err := writeFile(filepath.Join(modelsDir, "pickle_gen.go"), GenerateCoreQuery("models")); err != nil {
		return err
	}

	// 1b. Generate all built-in auth drivers (always present, user overrides via driver.go)
	authDir := layout.AuthDir
	for name := range builtinAuthDrivers {
		if err := os.MkdirAll(filepath.Join(authDir, name), 0o755); err != nil {
			return fmt.Errorf("creating auth dir for %s: %w", name, err)
		}
	}
	{
		fmt.Println("  scanning auth drivers")
		drivers, err := ScanAuthDrivers(authDir)
		if err != nil {
			return fmt.Errorf("scanning auth drivers: %w", err)
		}

		httpImport := project.ModulePath + "/app/http"


		// Write driver_gen.go for built-in drivers that haven't been overridden
		for _, d := range drivers {
			if d.NeedsGen {
				fmt.Printf("  generating auth/%s/driver_gen.go\n", d.Name)
				src, err := GenerateBuiltinDriver(d, httpImport)
				if err != nil {
					return fmt.Errorf("generating auth driver %s: %w", d.Name, err)
				}
				if err := writeFile(filepath.Join(d.Dir, "driver_gen.go"), src); err != nil {
					return err
				}
			}
		}

		// Write driver-specific migrations into database/migrations/ as _gen.go files
		for _, d := range drivers {
			if d.IsBuiltin && d.NeedsGen {
				if err := WriteDriverMigrations(d.Name, migrationsDir, "migrations"); err != nil {
					return fmt.Errorf("writing migrations for auth driver %s: %w", d.Name, err)
				}
			}
		}

		// Generate auth/pickle_gen.go with interface + registry
		if len(drivers) > 0 {
			fmt.Println("  generating auth/pickle_gen.go")
			registrySrc, err := GenerateAuthRegistry(drivers, project.ModulePath, httpImport)
			if err != nil {
				return fmt.Errorf("generating auth registry: %w", err)
			}
			if err := writeFile(filepath.Join(authDir, "pickle_gen.go"), registrySrc); err != nil {
				return err
			}
		}
	}

	// 2. Write pre-tickled schema types and migration runner into migrations/
	if _, err := os.Stat(migrationsDir); err == nil {
		fmt.Println("  generating migrations/types_gen.go")
		if err := writeFile(filepath.Join(migrationsDir, "types_gen.go"), GenerateCoreSchema("migrations")); err != nil {
			return err
		}

		fmt.Println("  generating migrations/runner_gen.go")
		if err := writeFile(filepath.Join(migrationsDir, "runner_gen.go"), GenerateCoreMigration("migrations")); err != nil {
			return err
		}

		fmt.Println("  generating migrations/registry_gen.go")
		localMigEntries, err := ScanMigrationFiles(migrationsDir)
		if err != nil {
			return fmt.Errorf("scanning migration files: %w", err)
		}
		registrySrc, err := GenerateRegistry("migrations", localMigEntries)
		if err != nil {
			return fmt.Errorf("generating registry: %w", err)
		}
		if err := writeFile(filepath.Join(migrationsDir, "registry_gen.go"), registrySrc); err != nil {
			return err
		}
	}

	// 3. Run schema inspector to get tables, views, and relationships from migrations
	var tables []*schema.Table
	var views []*schema.View
	var relationships []SchemaRelationship
	if _, err := os.Stat(migrationsDir); err == nil {
		fmt.Println("  inspecting schema from migrations")
		var err error
		tables, views, relationships, err = RunSchemaInspector(project)
		if err != nil {
			return fmt.Errorf("schema inspection: %w", err)
		}
	}

	// Build nesting map: child table name → relationship info
	nestingMap := map[string]SchemaRelationship{}
	for _, rel := range relationships {
		nestingMap[rel.ChildTable] = rel
	}

	// 3b. Write pickle_gen.go (QueryBuilder) into each nested model subdirectory
	if len(nestingMap) > 0 {
		nestedDirs := map[string]string{} // dir → pkgName
		for _, tbl := range tables {
			dir, pkg := resolveModelDir(modelsDir, tbl.Name, nestingMap)
			if dir != modelsDir {
				nestedDirs[dir] = pkg
			}
		}
		for dir, pkg := range nestedDirs {
			fmt.Printf("  generating %s/pickle_gen.go\n", pkg)
			if err := writeFile(filepath.Join(dir, "pickle_gen.go"), GenerateCoreQuery(pkg)); err != nil {
				return err
			}
		}
	}

	// 4. Generate models into models/ (or nested subdirectories)
	if len(tables) > 0 {
		for _, tbl := range tables {
			targetDir, pkgName := resolveModelDir(modelsDir, tbl.Name, nestingMap)
			fmt.Printf("  generating model: %s → %s\n", tbl.Name, pkgName)
			src, err := GenerateModel(tbl, pkgName)
			if err != nil {
				return fmt.Errorf("generating model for %s: %w", tbl.Name, err)
			}
			filename := toLowerFirst(tableToStructName(tbl.Name)) + ".go"
			if err := writeFile(filepath.Join(targetDir, filename), src); err != nil {
				return err
			}
		}
	}

	// 4b. Generate response structs for models with ownership
	if len(tables) > 0 {
		for _, tbl := range tables {
			if HasOwnership(tbl) {
				targetDir, pkgName := resolveModelDir(modelsDir, tbl.Name, nestingMap)
				fmt.Printf("  generating responses: %s\n", tbl.Name)
				src, err := GenerateResponses(tbl, pkgName)
				if err != nil {
					return fmt.Errorf("generating responses for %s: %w", tbl.Name, err)
				}
				filename := toLowerFirst(tableToStructName(tbl.Name)) + "_responses.go"
				if err := writeFile(filepath.Join(targetDir, filename), src); err != nil {
					return err
				}
			}
		}
	}

	// 5. Generate query scopes into models/ (or nested subdirectories)
	if len(tables) > 0 {
		scopesPath := filepath.Join(picklePkgDir, "cooked", "scopes.go")
		if _, err := os.Stat(scopesPath); err == nil {
			blocks, err := tickle.ParseScopeBlocks(scopesPath)
			if err != nil {
				return fmt.Errorf("parsing scope blocks: %w", err)
			}

			for _, tbl := range tables {
				targetDir, pkgName := resolveModelDir(modelsDir, tbl.Name, nestingMap)
				fmt.Printf("  generating queries: %s\n", tbl.Name)
				src, err := GenerateQueryScopes(tbl, blocks, pkgName)
				if err != nil {
					return fmt.Errorf("generating scopes for %s: %w", tbl.Name, err)
				}
				filename := toLowerFirst(tableToStructName(tbl.Name)) + "_query.go"
				if err := writeFile(filepath.Join(targetDir, filename), src); err != nil {
					return err
				}
			}

			// Generate Tx.Query<Model>() methods
			fmt.Println("  generating transaction query methods")
			txSrc, err := GenerateTxMethods(tables, nestingMap, modelsDir, "models")
			if err != nil {
				return fmt.Errorf("generating tx methods: %w", err)
			}
			if err := writeFile(filepath.Join(modelsDir, "tx_gen.go"), txSrc); err != nil {
				return err
			}
		}
	}

	// 5b. Generate view models into models/
	if len(views) > 0 {
		for _, view := range views {
			fmt.Printf("  generating view model: %s\n", view.Name)
			src, err := GenerateViewModel(view, "models")
			if err != nil {
				return fmt.Errorf("generating view model for %s: %w", view.Name, err)
			}
			filename := toLowerFirst(tableToStructName(view.Name)) + ".go"
			if err := writeFile(filepath.Join(modelsDir, filename), src); err != nil {
				return err
			}
		}
	}

	// 5c. Generate view query scopes into models/
	if len(views) > 0 {
		scopesPath := filepath.Join(picklePkgDir, "cooked", "scopes.go")
		if _, err := os.Stat(scopesPath); err == nil {
			blocks, err := tickle.ParseScopeBlocks(scopesPath)
			if err != nil {
				return fmt.Errorf("parsing scope blocks: %w", err)
			}

			for _, view := range views {
				fmt.Printf("  generating view queries: %s\n", view.Name)
				src, err := GenerateViewQueryScopes(view, blocks, "models")
				if err != nil {
					return fmt.Errorf("generating view scopes for %s: %w", view.Name, err)
				}
				filename := toLowerFirst(tableToStructName(view.Name)) + "_query.go"
				if err := writeFile(filepath.Join(modelsDir, filename), src); err != nil {
					return err
				}
			}
		}
	}

	// 6. Generate bindings
	requests, err := ScanRequests(requestsDir)
	if err != nil {
		return fmt.Errorf("scanning requests: %w", err)
	}

	if len(requests) > 0 {
		fmt.Println("  generating bindings")
		bindingSrc, err := GenerateBindings(requests, "requests")
		if err != nil {
			return fmt.Errorf("generating bindings: %w", err)
		}

		if err := writeFile(filepath.Join(requestsDir, "bindings_gen.go"), bindingSrc); err != nil {
			return err
		}
	}

	// 7. Generate GraphQL layer if app/graphql/ exists
	graphqlDir := filepath.Join(project.Dir, "app", "graphql")
	if _, err := os.Stat(graphqlDir); err == nil {
		fmt.Println("  generating graphql layer")
		if err := GenerateGraphQL(project, tables, relationships); err != nil {
			return fmt.Errorf("graphql generation: %w", err)
		}
	}

	// 8. Generate commands glue if app/commands/ exists
	commandsDir := layout.CommandsDir
	if _, err := os.Stat(commandsDir); err == nil {
		fmt.Println("  generating commands/pickle_gen.go")
		userCmds, err := ScanCommands(commandsDir)
		if err != nil {
			return fmt.Errorf("scanning commands: %w", err)
		}

		// Scan routes/ for route vars (e.g. "API")
		routesDir := filepath.Join(project.Dir, "routes")
		var routeVars []string
		if _, err := os.Stat(routesDir); err == nil {
			var scanErr error
			routeVars, scanErr = ScanRouteVars(routesDir)
			if scanErr != nil {
				return fmt.Errorf("scanning route vars: %w", scanErr)
			}
		}

		// Check if auth directory exists
		hasAuth := false
		if _, err := os.Stat(layout.AuthDir); err == nil {
			hasAuth = true
		}

		cmdSrc, err := GenerateCommandsGlue(project.ModulePath, layout.MigrationsRel, userCmds, routeVars, hasAuth)
		if err != nil {
			return fmt.Errorf("generating commands glue: %w", err)
		}
		if err := writeFile(filepath.Join(commandsDir, "pickle_gen.go"), cmdSrc); err != nil {
			return err
		}
	}

	return nil
}

// resolveModelDir determines the output directory and package name for a table,
// based on its position in the relationship nesting hierarchy.
// - Top-level tables → models/ (package "models")
// - Nested tables → models/parent/ (package "parent_singular")
// - .TopLevelModel() → models/ (package "models")
// - Deep nesting → models/parent/child/ etc.
func resolveModelDir(modelsDir, tableName string, nestingMap map[string]SchemaRelationship) (string, string) {
	rel, isNested := nestingMap[tableName]
	if !isNested || rel.TopLevel {
		return modelsDir, "models"
	}

	// Build the path chain from child → parent
	var parents []string
	current := tableName
	for {
		r, ok := nestingMap[current]
		if !ok || r.TopLevel {
			break
		}
		parentSingular := strings.TrimSuffix(r.ParentTable, "s") // simple singularize
		parents = append([]string{parentSingular}, parents...)
		current = r.ParentTable
	}

	dir := filepath.Join(append([]string{modelsDir}, parents...)...)
	pkgName := parents[len(parents)-1] // deepest parent's singular name
	return dir, pkgName
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating directory for %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
