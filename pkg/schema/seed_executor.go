package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// SeedPolicy controls how a root scenario behaves when it is repeated.
type SeedPolicy string

const (
	InsertOnly      SeedPolicy = "insert_only"
	InsertOrIgnore  SeedPolicy = "insert_or_ignore"
	Upsert          SeedPolicy = "upsert"
	ReplaceScenario SeedPolicy = "replace_scenario"
)

// SeedExecutionOptions controls one root scenario execution.
type SeedExecutionOptions struct {
	Scenario           string
	RootSeed           int64
	Environment        string
	Force              bool
	ConfirmEnvironment string
	DryRun             bool
	Driver             string
	Policy             SeedPolicy
	UniqueBy           []string
	UpdateColumns      []string
	ProvenanceEnabled  bool
	PasswordHasher     func(string) (string, error)
	SeederResolver     func(string, SeedValueContext) (value any, found bool, err error)
}

// SeedPlannedRow is a resolved row in insertion order. Password values are
// always redacted; Values contains the application-supplied insertion values.
type SeedPlannedRow struct {
	NodeID     int
	NodePath   string
	RowOrdinal int
	Table      string
	Values     map[string]any
	Sensitive  map[string]bool
	UniqueBy   []string
	Updates    []string
}

// SeedExecutionResult describes what was planned or inserted.
type SeedExecutionResult struct {
	Scenario string
	RootSeed int64
	Rows     []SeedPlannedRow
	DryRun   bool
}

// SeedExecutor expands and inserts one validated scenario graph.
type SeedExecutor struct {
	DB     *sql.DB
	Tables []*Table
}

// Run validates safety before planning. Mutating runs use one transaction and
// roll back the entire root scenario after any insertion failure.
func (e SeedExecutor) Run(ctx context.Context, graph *SeedGraph, options SeedExecutionOptions) (*SeedExecutionResult, error) {
	if graph == nil {
		return nil, errors.New("seed graph is nil")
	}
	if strings.TrimSpace(options.Scenario) == "" {
		return nil, errors.New("seed scenario name is required")
	}
	if err := ValidateSeedEnvironment(options.Environment, options.Force, options.ConfirmEnvironment, options.DryRun); err != nil {
		return nil, err
	}
	rows, err := PlanSeedGraph(graph, e.Tables, options)
	if err != nil {
		return nil, fmt.Errorf("seed scenario %s: %w", options.Scenario, err)
	}
	if err := validateSeedRepeatPolicy(rows, options); err != nil {
		return nil, fmt.Errorf("seed scenario %s: %w", options.Scenario, err)
	}
	result := &SeedExecutionResult{Scenario: options.Scenario, RootSeed: options.RootSeed, Rows: rows, DryRun: options.DryRun}
	if options.DryRun {
		return result, nil
	}
	if e.DB == nil {
		return nil, errors.New("seed database is not configured")
	}
	tx, err := e.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("seed scenario %s: begin transaction: %w", options.Scenario, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for _, row := range rows {
		query, arguments, err := seedInsertSQL(options.Driver, row, options)
		if err != nil {
			return nil, fmt.Errorf("seed scenario %s path %s row %d table %s: %w", options.Scenario, row.NodePath, row.RowOrdinal, row.Table, err)
		}
		if _, err := tx.ExecContext(ctx, query, arguments...); err != nil {
			return nil, fmt.Errorf("seed scenario %s path %s row %d table %s: insert failed: %w", options.Scenario, row.NodePath, row.RowOrdinal, row.Table, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("seed scenario %s: commit: %w", options.Scenario, err)
	}
	committed = true
	return result, nil
}

// ValidateSeedEnvironment permits mutation in development-like environments.
// Every other environment requires both explicit confirmation flags.
func ValidateSeedEnvironment(environment string, force bool, confirmation string, dryRun bool) error {
	if dryRun {
		return nil
	}
	environment = strings.ToLower(strings.TrimSpace(environment))
	switch environment {
	case "local", "development", "test":
		return nil
	}
	if environment == "" {
		environment = "unknown"
	}
	if !force || confirmation != environment {
		return fmt.Errorf("seeding environment %q requires --force --confirm-environment %s", environment, environment)
	}
	return nil
}

// PlanSeedGraph resolves counts, parent edges, field providers, composites, and
// hashing without touching the database.
func PlanSeedGraph(graph *SeedGraph, tables []*Table, options SeedExecutionOptions) ([]SeedPlannedRow, error) {
	tableByName := make(map[string]*Table, len(tables))
	for _, table := range tables {
		tableByName[table.Name] = table
	}
	nodeByID := make(map[int]SeedNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if node.ID < 1 || nodeByID[node.ID].ID != 0 {
			return nil, fmt.Errorf("invalid or duplicate seed node id %d", node.ID)
		}
		if tableByName[node.Seeder.Table] == nil {
			return nil, fmt.Errorf("seeder %s targets unknown table %q", node.Seeder.Name, node.Seeder.Table)
		}
		if node.Count.Min < 0 || node.Count.Max < node.Count.Min {
			return nil, fmt.Errorf("seeder %s has invalid count range", node.Seeder.Name)
		}
		nodeByID[node.ID] = node
	}
	if err := validateExecutionCycles(graph.Nodes, nodeByID); err != nil {
		return nil, err
	}

	hasher := options.PasswordHasher
	if hasher == nil {
		hasher = func(string) (string, error) {
			return "", errors.New("password hasher is not configured")
		}
	}
	rowsByNode := map[int][]SeedPlannedRow{}
	var planned []SeedPlannedRow
	remaining := append([]SeedNode(nil), graph.Nodes...)
	for len(remaining) > 0 {
		progress := false
		next := remaining[:0]
		for _, node := range remaining {
			if node.ParentNodeID != 0 {
				if _, ready := rowsByNode[node.ParentNodeID]; !ready {
					next = append(next, node)
					continue
				}
			}
			produced, err := planSeedNode(node, tableByName, rowsByNode, options, hasher)
			if err != nil {
				return nil, err
			}
			rowsByNode[node.ID] = produced
			planned = append(planned, produced...)
			progress = true
		}
		if !progress {
			return nil, errors.New("seed graph contains unresolved parent nodes")
		}
		remaining = next
	}
	return planned, nil
}

func planSeedNode(node SeedNode, tables map[string]*Table, rowsByNode map[int][]SeedPlannedRow, options SeedExecutionOptions, hasher func(string) (string, error)) ([]SeedPlannedRow, error) {
	table := tables[node.Seeder.Table]
	parents := []SeedPlannedRow{{}}
	var relationship *ForeignKey
	if node.ParentNodeID != 0 {
		parents = rowsByNode[node.ParentNodeID]
		if len(parents) > 0 {
			var err error
			relationship, err = resolveExecutionRelationship(table, parents[0].Table, node.Through)
			if err != nil {
				return nil, fmt.Errorf("seeder %s: %w", node.Seeder.Name, err)
			}
		}
	}
	var rows []SeedPlannedRow
	ordinal := 0
	for parentIndex, parent := range parents {
		count := seedNodeCount(node, options, parentIndex)
		for index := 0; index < count; index++ {
			path := fmt.Sprintf("%s/%d", node.Seeder.Name, ordinal)
			overrides := cloneSeedValues(node.Values)
			base := SeedValueContext{RootSeed: options.RootSeed, Scenario: options.Scenario, NodePath: path, RowOrdinal: ordinal}
			if options.SeederResolver != nil {
				seeded, found, err := options.SeederResolver(node.Seeder.Name, base)
				if err != nil {
					return nil, fmt.Errorf("seeder %s path %s row %d: custom row seeder failed: %w", node.Seeder.Name, path, ordinal, err)
				}
				if found {
					values, err := seedStructValues(seeded)
					if err != nil {
						return nil, fmt.Errorf("seeder %s path %s row %d: %w", node.Seeder.Name, path, ordinal, err)
					}
					for column, value := range values {
						overrides[column] = value
					}
				}
			}
			if relationship != nil {
				for position, column := range relationship.Columns {
					value, exists := parent.Values[relationship.ReferencedColumns[position]]
					if !exists {
						return nil, fmt.Errorf("seeder %s path %s: parent %s has no value for relationship column %s", node.Seeder.Name, path, parent.Table, relationship.ReferencedColumns[position])
					}
					overrides[column] = value
				}
			}
			values, err := GenerateSeedRowWith(table, overrides, base, options.SeederResolver)
			if err != nil {
				return nil, fmt.Errorf("seeder %s path %s row %d: %w", node.Seeder.Name, path, ordinal, err)
			}
			sensitive := map[string]bool{}
			for _, column := range table.Columns {
				if column.Seeder == nil || column.Seeder.Kind != "password" {
					continue
				}
				plain, ok := values[column.Name].(string)
				if !ok {
					return nil, fmt.Errorf("seeder %s path %s row %d column %s: password composite is not text", node.Seeder.Name, path, ordinal, column.Name)
				}
				hash, err := hasher(plain)
				if err != nil {
					return nil, fmt.Errorf("seeder %s path %s row %d column %s: password hashing failed", node.Seeder.Name, path, ordinal, column.Name)
				}
				values[column.Name] = hash
				sensitive[column.Name] = true
			}
			rows = append(rows, SeedPlannedRow{NodeID: node.ID, NodePath: path, RowOrdinal: ordinal, Table: table.Name, Values: values, Sensitive: sensitive, UniqueBy: append([]string(nil), node.UniqueColumns...), Updates: append([]string(nil), node.UpdateColumns...)})
			ordinal++
		}
	}
	return rows, nil
}

func seedStructValues(value any) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	if values, ok := value.(map[string]any); ok {
		return cloneSeedValues(values), nil
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.Pointer {
		if reflected.IsNil() {
			return nil, nil
		}
		reflected = reflected.Elem()
	}
	if reflected.Kind() != reflect.Struct {
		return nil, fmt.Errorf("custom row seeder must return a struct or map[string]any")
	}
	typeInfo := reflected.Type()
	values := map[string]any{}
	for index := 0; index < reflected.NumField(); index++ {
		field := typeInfo.Field(index)
		if !field.IsExported() {
			continue
		}
		name := strings.Split(field.Tag.Get("db"), ",")[0]
		if name == "" {
			name = strings.Split(field.Tag.Get("json"), ",")[0]
		}
		if name == "-" {
			continue
		}
		if name == "" {
			name = seedSnakeCase(field.Name)
		}
		values[name] = reflected.Field(index).Interface()
	}
	return values, nil
}

func seedSnakeCase(value string) string {
	var out strings.Builder
	for index, char := range value {
		if char >= 'A' && char <= 'Z' {
			if index > 0 {
				out.WriteByte('_')
			}
			out.WriteByte(byte(char - 'A' + 'a'))
		} else {
			out.WriteRune(char)
		}
	}
	return out.String()
}

func seedNodeCount(node SeedNode, options SeedExecutionOptions, parentOrdinal int) int {
	if node.Count.Min == node.Count.Max {
		return node.Count.Min
	}
	ctx := SeedValueContext{RootSeed: options.RootSeed, Scenario: options.Scenario, NodePath: node.Seeder.Name, RowOrdinal: parentOrdinal, Column: "__count"}
	stream := newSeedStream(ctx)
	return node.Count.Min + stream.index(node.Count.Max-node.Count.Min+1)
}

func resolveExecutionRelationship(child *Table, parentTable, through string) (*ForeignKey, error) {
	var matches []*ForeignKey
	for _, fk := range child.ForeignKeys {
		if fk.ReferencedTable == parentTable && (through == "" || through == strings.Join(fk.Columns, ",") || seedContains(fk.Columns, through)) {
			matches = append(matches, fk)
		}
	}
	for _, column := range child.Columns {
		if column.ForeignKeyTable == parentTable && (through == "" || through == column.Name) {
			matches = append(matches, &ForeignKey{Columns: []string{column.Name}, ReferencedTable: parentTable, ReferencedColumns: []string{column.ForeignKeyColumn}})
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no relationship from %s to %s", child.Name, parentTable)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("ambiguous relationship from %s to %s; specify Through", child.Name, parentTable)
	}
	if len(matches[0].Columns) != len(matches[0].ReferencedColumns) {
		return nil, errors.New("incomplete composite relationship")
	}
	return matches[0], nil
}

func seedInsertSQL(driver string, row SeedPlannedRow, options SeedExecutionOptions) (string, []any, error) {
	columns := make([]string, 0, len(row.Values))
	for column := range row.Values {
		columns = append(columns, column)
	}
	sort.Strings(columns)
	arguments := make([]any, len(columns))
	placeholders := make([]string, len(columns))
	for index, column := range columns {
		arguments[index] = row.Values[column]
		if driver == "pgsql" || driver == "postgres" {
			placeholders[index] = fmt.Sprintf("$%d", index+1)
		} else {
			placeholders[index] = "?"
		}
	}
	verb := "INSERT INTO"
	policy := options.Policy
	if policy == "" {
		policy = InsertOnly
	}
	if policy == InsertOrIgnore && driver == "mysql" {
		verb = "INSERT IGNORE INTO"
	}
	query := fmt.Sprintf("%s %s (%s) VALUES (%s)", verb, seedQuoteIdentifier(driver, row.Table), seedQuotedColumns(driver, columns), strings.Join(placeholders, ", "))
	uniqueBy := row.UniqueBy
	if len(uniqueBy) == 0 {
		uniqueBy = options.UniqueBy
	}
	updateColumns := row.Updates
	if len(updateColumns) == 0 {
		updateColumns = options.UpdateColumns
	}
	identity := seedQuotedColumns(driver, uniqueBy)
	switch policy {
	case InsertOnly:
	case InsertOrIgnore:
		if driver != "mysql" {
			query += " ON CONFLICT (" + identity + ") DO NOTHING"
		}
	case Upsert:
		updates := make([]string, len(updateColumns))
		for index, column := range updateColumns {
			quoted := seedQuoteIdentifier(driver, column)
			if driver == "mysql" {
				updates[index] = quoted + " = VALUES(" + quoted + ")"
			} else {
				updates[index] = quoted + " = excluded." + quoted
			}
		}
		if driver == "mysql" {
			query += " ON DUPLICATE KEY UPDATE " + strings.Join(updates, ", ")
		} else {
			query += " ON CONFLICT (" + identity + ") DO UPDATE SET " + strings.Join(updates, ", ")
		}
	case ReplaceScenario:
		return "", nil, errors.New("replace-scenario execution requires generated provenance support")
	default:
		return "", nil, fmt.Errorf("unknown seed repeat policy %q", policy)
	}
	return query, arguments, nil
}

func validateSeedRepeatPolicy(rows []SeedPlannedRow, options SeedExecutionOptions) error {
	policy := options.Policy
	if policy == "" || policy == InsertOnly {
		return nil
	}
	if policy == ReplaceScenario {
		if !options.ProvenanceEnabled {
			return errors.New("replace-scenario policy requires explicitly enabled seed provenance")
		}
		return errors.New("replace-scenario policy is not available until provenance tables are generated")
	}
	if policy != InsertOrIgnore && policy != Upsert {
		return fmt.Errorf("unknown seed repeat policy %q", policy)
	}
	for _, row := range rows {
		uniqueBy := row.UniqueBy
		if len(uniqueBy) == 0 {
			uniqueBy = options.UniqueBy
		}
		updates := row.Updates
		if len(updates) == 0 {
			updates = options.UpdateColumns
		}
		if len(uniqueBy) == 0 {
			return fmt.Errorf("%s policy requires an explicit UniqueBy identity for table %s", policy, row.Table)
		}
		if policy == Upsert && len(updates) == 0 {
			return fmt.Errorf("upsert policy requires an explicit update column allowlist for table %s", row.Table)
		}
		for _, column := range uniqueBy {
			if _, exists := row.Values[column]; !exists {
				return fmt.Errorf("repeat identity column %s is missing from table %s row", column, row.Table)
			}
		}
		for _, column := range updates {
			if _, exists := row.Values[column]; !exists {
				return fmt.Errorf("upsert column %s is missing from table %s row", column, row.Table)
			}
			if seedContains(uniqueBy, column) {
				return fmt.Errorf("upsert column %s cannot also be part of UniqueBy", column)
			}
		}
	}
	return nil
}

func seedQuoteIdentifier(driver, value string) string {
	if driver == "mysql" {
		return "`" + strings.ReplaceAll(value, "`", "``") + "`"
	}
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func seedQuotedColumns(driver string, columns []string) string {
	quoted := make([]string, len(columns))
	for index, column := range columns {
		quoted[index] = seedQuoteIdentifier(driver, column)
	}
	return strings.Join(quoted, ", ")
}

func cloneSeedValues(values map[string]any) map[string]any {
	clone := make(map[string]any, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func seedContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validateExecutionCycles(nodes []SeedNode, byID map[int]SeedNode) error {
	for _, node := range nodes {
		seen := map[int]bool{}
		for id := node.ID; id != 0; id = byID[id].ParentNodeID {
			if seen[id] {
				return fmt.Errorf("seed graph contains a relationship cycle at node %d", id)
			}
			seen[id] = true
			if byID[id].ID == 0 {
				return fmt.Errorf("seed node %d references missing parent", id)
			}
		}
	}
	return nil
}
