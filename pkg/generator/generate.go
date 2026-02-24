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

// Project represents a Pickle project layout rooted at a directory.
type Project struct {
	Dir        string // project root
	ModulePath string // Go module path from go.mod
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

	return &Project{Dir: absDir, ModulePath: modPath}, nil
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
	Name    string                `json:"name"`
	Columns []inspectorColumnInfo `json:"columns"`
	Indexes []inspectorIndexInfo  `json:"indexes,omitempty"`
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
}

type inspectorIndexInfo struct {
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
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

// RunSchemaInspector generates a temp inspector program, compiles and runs it,
// and returns the parsed schema tables.
func RunSchemaInspector(project *Project) ([]*schema.Table, error) {
	migrationsDir := filepath.Join(project.Dir, "migrations")
	structNames, err := ScanMigrationStructs(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("scanning migrations: %w", err)
	}

	if len(structNames) == 0 {
		return nil, nil
	}

	var entries []MigrationEntry
	for _, name := range structNames {
		entries = append(entries, MigrationEntry{StructName: name})
	}

	inspectorSrc, err := GenerateSchemaInspector(project.ModulePath, entries)
	if err != nil {
		return nil, fmt.Errorf("generating inspector: %w", err)
	}

	// Write to a temp directory inside the project so it can resolve local imports
	tmpDir := filepath.Join(project.Dir, ".pickle-tmp")
	os.MkdirAll(tmpDir, 0o755)
	defer os.RemoveAll(tmpDir)

	inspectorPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(inspectorPath, inspectorSrc, 0o644); err != nil {
		return nil, fmt.Errorf("writing inspector: %w", err)
	}

	cmd := exec.Command("go", "run", inspectorPath, "--json")
	cmd.Dir = project.Dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("running inspector: %w\n%s", err, output)
	}

	var tableInfos []inspectorTableInfo
	if err := json.Unmarshal(output, &tableInfos); err != nil {
		return nil, fmt.Errorf("parsing inspector output: %w\n%s", err, output)
	}

	// Convert to schema.Table
	var tables []*schema.Table
	for _, ti := range tableInfos {
		t := &schema.Table{Name: ti.Name}
		for _, ci := range ti.Columns {
			col := &schema.Column{
				Name:             ci.Name,
				Type:             typeNameToColumnType[ci.Type],
				IsNullable:       ci.Nullable,
				IsPrimaryKey:     ci.PrimaryKey,
				IsUnique:         ci.Unique,
				ForeignKeyTable:  ci.ForeignKeyTable,
				ForeignKeyColumn: ci.ForeignKeyColumn,
				Length:           ci.Length,
				Precision:        ci.Precision,
				Scale:            ci.Scale,
			}
			if ci.Default != nil {
				col.DefaultValue = ci.Default
			}
			t.Columns = append(t.Columns, col)
		}
		tables = append(tables, t)
	}

	return tables, nil
}

// detectPackageName reads the package declaration from the first .go file in the directory.
func detectPackageName(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		if strings.HasSuffix(e.Name(), "_gen.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
		if err != nil {
			continue
		}
		return f.Name.Name, nil
	}
	return "", fmt.Errorf("no Go files found in %s", dir)
}

// Generate runs all generators for a project and writes output.
//
// Layout:
//   - {root}/pickle_gen.go            — HTTP types (Context, Response, Router, etc.)
//   - {root}/routes_gen.go            — Route registration
//   - {root}/bindings_gen.go          — Request deserialization + validation
//   - {root}/models/pickle_gen.go     — QueryBuilder[T]
//   - {root}/models/*.go              — Model structs and query scopes
//   - {root}/migrations/types_gen.go  — Schema DSL types (Migration, Table, etc.)
func Generate(project *Project, picklePkgDir string) error {
	rootPkg, err := detectPackageName(project.Dir)
	if err != nil {
		return fmt.Errorf("detecting package name: %w", err)
	}

	modelsDir := filepath.Join(project.Dir, "models")
	migrationsDir := filepath.Join(project.Dir, "migrations")

	configDir := filepath.Join(project.Dir, "config")

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
	if err := writeFile(filepath.Join(project.Dir, "pickle_gen.go"), GenerateCoreHTTP(rootPkg)); err != nil {
		return err
	}

	fmt.Println("  generating models/pickle_gen.go")
	if err := writeFile(filepath.Join(modelsDir, "pickle_gen.go"), GenerateCoreQuery("models")); err != nil {
		return err
	}

	// 2. Write pre-tickled schema types into migrations/
	if _, err := os.Stat(migrationsDir); err == nil {
		fmt.Println("  generating migrations/types_gen.go")
		if err := writeFile(filepath.Join(migrationsDir, "types_gen.go"), GenerateCoreSchema("migrations")); err != nil {
			return err
		}
	}

	// 3. Run schema inspector to get tables from migrations
	var tables []*schema.Table
	if _, err := os.Stat(migrationsDir); err == nil {
		fmt.Println("  inspecting schema from migrations")
		tables, err = RunSchemaInspector(project)
		if err != nil {
			return fmt.Errorf("schema inspection: %w", err)
		}
	}

	// 4. Generate models into models/
	if len(tables) > 0 {
		for _, tbl := range tables {
			fmt.Printf("  generating model: %s\n", tbl.Name)
			src, err := GenerateModel(tbl, "models")
			if err != nil {
				return fmt.Errorf("generating model for %s: %w", tbl.Name, err)
			}
			filename := toLowerFirst(tableToStructName(tbl.Name)) + ".go"
			if err := writeFile(filepath.Join(modelsDir, filename), src); err != nil {
				return err
			}
		}
	}

	// 5. Generate query scopes into models/
	if len(tables) > 0 {
		scopesPath := filepath.Join(picklePkgDir, "cooked", "scopes.go")
		if _, err := os.Stat(scopesPath); err == nil {
			blocks, err := tickle.ParseScopeBlocks(scopesPath)
			if err != nil {
				return fmt.Errorf("parsing scope blocks: %w", err)
			}

			for _, tbl := range tables {
				fmt.Printf("  generating queries: %s\n", tbl.Name)
				src, err := GenerateQueryScopes(tbl, blocks, "models")
				if err != nil {
					return fmt.Errorf("generating scopes for %s: %w", tbl.Name, err)
				}
				filename := toLowerFirst(tableToStructName(tbl.Name)) + "_query.go"
				if err := writeFile(filepath.Join(modelsDir, filename), src); err != nil {
					return err
				}
			}
		}
	}

	// 6. Generate bindings into project root
	requests, err := ScanRequests(project.Dir)
	if err != nil {
		return fmt.Errorf("scanning requests: %w", err)
	}

	if len(requests) > 0 {
		fmt.Println("  generating bindings")
		bindingSrc, err := GenerateBindings(requests, rootPkg)
		if err != nil {
			return fmt.Errorf("generating bindings: %w", err)
		}

		if err := writeFile(filepath.Join(project.Dir, "bindings_gen.go"), bindingSrc); err != nil {
			return err
		}
	}

	return nil
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
