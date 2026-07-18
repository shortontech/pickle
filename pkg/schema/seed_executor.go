package schema

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
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
	AnchorTime         time.Time
}

// SeedPlannedRow is a resolved row in insertion order. Password values are
// always redacted; Values contains the application-supplied insertion values.
type SeedPlannedRow struct {
	NodeID           int
	NodePath         string
	RowOrdinal       int
	Table            string
	Values           map[string]any
	Sensitive        map[string]bool
	UniqueBy         []string
	Updates          []string
	Authored         map[string]bool
	Immutable        bool
	AppendOnly       bool
	IntegrityDerived bool
}

// SeedExecutionResult describes what was planned or inserted.
type SeedExecutionResult struct {
	Scenario   string
	RootSeed   int64
	Rows       []SeedPlannedRow
	DryRun     bool
	AnchorTime time.Time
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
	result := &SeedExecutionResult{Scenario: options.Scenario, RootSeed: options.RootSeed, Rows: rows, DryRun: options.DryRun, AnchorTime: effectiveSeedAnchor(options.AnchorTime)}
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
	tableByName := make(map[string]*Table, len(e.Tables))
	for _, table := range e.Tables {
		tableByName[table.Name] = table
	}
	tails := map[string]seedIntegrityTail{}
	locked := map[string]bool{}
	for _, row := range rows {
		table := tableByName[row.Table]
		if table == nil {
			return nil, fmt.Errorf("seed scenario %s: table metadata missing for %s", options.Scenario, row.Table)
		}
		if row.IntegrityDerived {
			if !locked[row.Table] {
				if err := lockSeedIntegrityTable(ctx, tx, options.Driver, row.Table); err != nil {
					return nil, err
				}
				locked[row.Table] = true
			}
			exists, err := seedExistingRowMatches(ctx, tx, options.Driver, row)
			if err != nil {
				return nil, fmt.Errorf("seed scenario %s path %s row %d table %s: %w", options.Scenario, row.NodePath, row.RowOrdinal, row.Table, err)
			}
			if exists {
				continue
			}
			tail, ok := tails[row.Table]
			if !ok {
				tail, err = readSeedIntegrityTail(ctx, tx, options.Driver, table)
				if err != nil {
					return nil, err
				}
			}
			order := seedIntegrityOrder(row, table)
			if tail.Order != "" && order <= tail.Order {
				return nil, fmt.Errorf("seed scenario %s path %s row %d table %s: generated integrity order %s does not follow existing chain tail %s", options.Scenario, row.NodePath, row.RowOrdinal, row.Table, order, tail.Order)
			}
			row.Values["prev_hash"] = append([]byte(nil), tail.Hash...)
			row.Values["row_hash"] = computeSeedRowHash(tail.Hash, row.Values, table.Columns)
		}
		query, arguments, err := seedInsertSQL(options.Driver, row, options)
		if err != nil {
			return nil, fmt.Errorf("seed scenario %s path %s row %d table %s: %w", options.Scenario, row.NodePath, row.RowOrdinal, row.Table, err)
		}
		execResult, err := tx.ExecContext(ctx, query, arguments...)
		if err != nil {
			return nil, fmt.Errorf("seed scenario %s path %s row %d table %s: insert failed: %w", options.Scenario, row.NodePath, row.RowOrdinal, row.Table, err)
		}
		if row.IntegrityDerived {
			inserted, _ := execResult.RowsAffected()
			if inserted > 0 {
				tails[row.Table] = seedIntegrityTail{Hash: append([]byte(nil), row.Values["row_hash"].([]byte)...), Order: seedIntegrityOrder(row, table)}
			}
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
	if err := deriveSeedFrameworkIdentities(planned, options); err != nil {
		return nil, err
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
			base := SeedValueContext{RootSeed: options.RootSeed, Scenario: options.Scenario, NodePath: path, RowOrdinal: ordinal, AnchorTime: effectiveSeedAnchor(options.AnchorTime)}
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
			authored := map[string]bool{}
			for column := range overrides {
				authored[column] = true
			}
			rows = append(rows, SeedPlannedRow{NodeID: node.ID, NodePath: path, RowOrdinal: ordinal, Table: table.Name, Values: values, Sensitive: sensitive, UniqueBy: append([]string(nil), node.UniqueColumns...), Updates: append([]string(nil), node.UpdateColumns...), Authored: authored, Immutable: table.IsImmutable, AppendOnly: table.IsAppendOnly, IntegrityDerived: table.IsImmutable || table.IsAppendOnly})
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
	ctx := SeedValueContext{RootSeed: options.RootSeed, Scenario: options.Scenario, NodePath: node.Seeder.Name, RowOrdinal: parentOrdinal, Column: "__count", AnchorTime: effectiveSeedAnchor(options.AnchorTime)}
	stream := newSeedStream(ctx)
	return node.Count.Min + stream.index(node.Count.Max-node.Count.Min+1)
}

func effectiveSeedAnchor(anchor time.Time) time.Time {
	if anchor.IsZero() {
		return DefaultSeedAnchor
	}
	return anchor.UTC()
}

func ParseSeedAnchor(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("seed anchor is empty")
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid --as-of %q: expected RFC3339 timestamp with explicit offset", value)
	}
	return parsed.UTC(), nil
}

// SeedAnchorFlag is a repeat-aware flag.Value for --as-of. Repeating the same
// normalized instant is harmless; conflicting anchors are rejected.
type SeedAnchorFlag struct {
	anchor time.Time
	set    bool
}

func (f *SeedAnchorFlag) String() string {
	if f == nil || !f.set {
		return ""
	}
	return f.anchor.Format(time.RFC3339)
}

func (f *SeedAnchorFlag) Set(value string) error {
	anchor, err := ParseSeedAnchor(value)
	if err != nil {
		return err
	}
	if f.set && !f.anchor.Equal(anchor) {
		return fmt.Errorf("conflicting --as-of values %s and %s", f.anchor.Format(time.RFC3339), anchor.Format(time.RFC3339))
	}
	f.anchor = anchor
	f.set = true
	return nil
}

func (f *SeedAnchorFlag) Anchor() time.Time {
	if f == nil || !f.set {
		return DefaultSeedAnchor
	}
	return f.anchor
}

// ValidateSeedAnchorArguments validates --as-of before commands such as
// migrate:fresh perform any database mutation.
func ValidateSeedAnchorArguments(args []string) error {
	var anchor SeedAnchorFlag
	for i := 0; i < len(args); i++ {
		argument := args[i]
		if argument == "--as-of" {
			if i+1 >= len(args) {
				return errors.New("--as-of requires an RFC3339 value")
			}
			i++
			if err := anchor.Set(args[i]); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(argument, "--as-of=") {
			if err := anchor.Set(strings.TrimPrefix(argument, "--as-of=")); err != nil {
				return err
			}
		}
	}
	return nil
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
		if policy == Upsert && (row.Immutable || row.AppendOnly) {
			return fmt.Errorf("upsert is unavailable for immutable or append-only table %s", row.Table)
		}
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

func deriveSeedFrameworkIdentities(rows []SeedPlannedRow, options SeedExecutionOptions) error {
	ordinals := map[string]int{}
	lastOrder := map[string]string{}
	for index := range rows {
		row := &rows[index]
		if !row.IntegrityDerived {
			continue
		}
		tableOrdinal := ordinals[row.Table]
		ordinals[row.Table]++
		if !row.Authored["id"] {
			row.Values["id"] = deterministicSeedUUIDv7(options, row.Table, row.NodePath, "id", tableOrdinal)
		}
		if row.Immutable && !row.Authored["version_id"] {
			row.Values["version_id"] = deterministicSeedUUIDv7(options, row.Table, row.NodePath, "version_id", tableOrdinal)
		}
		delete(row.Values, "row_hash")
		delete(row.Values, "prev_hash")
		order := fmt.Sprint(row.Values["id"])
		if row.Immutable {
			order += "\x00" + fmt.Sprint(row.Values["version_id"])
		}
		if previous := lastOrder[row.Table]; previous != "" && order <= previous {
			return fmt.Errorf("seed table %s has non-monotonic explicit integrity identity; declare rows in id/version order", row.Table)
		}
		lastOrder[row.Table] = order
	}
	return nil
}

func deterministicSeedUUIDv7(options SeedExecutionOptions, table, path, column string, ordinal int) string {
	identity := fmt.Sprintf("pickle-integrity-id-v1\x00%d\x00%s\x00%s\x00%s", options.RootSeed, table, path, column)
	digest := sha256.Sum256([]byte(identity))
	value := digest[:16]
	ms := DefaultSeedAnchor.Add(time.Duration(ordinal) * time.Millisecond).UnixMilli()
	for i := 5; i >= 0; i-- {
		value[i] = byte(ms)
		ms >>= 8
	}
	value[6] = (value[6] & 0x0f) | 0x70
	value[8] = (value[8] & 0x3f) | 0x80
	return formatSeedUUID(value)
}

func formatSeedUUID(value []byte) string {
	if len(value) != 16 {
		return ""
	}
	encoded := hex.EncodeToString(value)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
}

func parseSeedUUID(value string) ([]byte, bool) {
	compact := strings.ReplaceAll(value, "-", "")
	if len(compact) != 32 {
		return nil, false
	}
	decoded, err := hex.DecodeString(compact)
	return decoded, err == nil
}

func lockSeedIntegrityTable(ctx context.Context, tx *sql.Tx, driver, table string) error {
	if driver == "pgsql" || driver == "postgres" {
		digest := sha256.Sum256([]byte("pickle-integrity-chain:" + table))
		key := int64(binary.BigEndian.Uint64(digest[:8]))
		if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", key); err != nil {
			return fmt.Errorf("lock integrity chain %s: %w", table, err)
		}
	}
	return nil
}

type seedIntegrityTail struct {
	Hash  []byte
	Order string
}

func readSeedIntegrityTail(ctx context.Context, tx *sql.Tx, driver string, table *Table) (seedIntegrityTail, error) {
	order := "id DESC"
	selectColumns := "row_hash, id"
	if table.IsImmutable {
		order += ", version_id DESC"
		selectColumns += ", version_id"
	}
	query := "SELECT " + selectColumns + " FROM " + seedQuoteIdentifier(driver, table.Name) + " ORDER BY " + order + " LIMIT 1"
	var tail []byte
	var id any
	var version any
	scan := []any{&tail, &id}
	if table.IsImmutable {
		scan = append(scan, &version)
	}
	err := tx.QueryRowContext(ctx, query).Scan(scan...)
	if errors.Is(err, sql.ErrNoRows) {
		return seedIntegrityTail{Hash: make([]byte, 32)}, nil
	}
	if err != nil {
		return seedIntegrityTail{}, fmt.Errorf("read integrity chain tail for %s: %w", table.Name, err)
	}
	orderValue := seedComparableIdentity(id)
	if table.IsImmutable {
		orderValue += "\x00" + seedComparableIdentity(version)
	}
	return seedIntegrityTail{Hash: tail, Order: orderValue}, nil
}

func seedIntegrityOrder(row SeedPlannedRow, table *Table) string {
	order := seedComparableIdentity(row.Values["id"])
	if table.IsImmutable {
		order += "\x00" + seedComparableIdentity(row.Values["version_id"])
	}
	return order
}

func seedComparableIdentity(value any) string {
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	case [16]byte:
		return string(typed[:])
	default:
		return fmt.Sprint(value)
	}
}

func seedExistingRowMatches(ctx context.Context, tx *sql.Tx, driver string, row SeedPlannedRow) (bool, error) {
	identity := row.UniqueBy
	if len(identity) == 0 {
		return false, nil
	}
	columns := make([]string, 0, len(row.Values))
	for column := range row.Values {
		if column != "row_hash" && column != "prev_hash" {
			columns = append(columns, column)
		}
	}
	sort.Strings(columns)
	where := make([]string, len(identity))
	args := make([]any, len(identity))
	for i, column := range identity {
		placeholder := "?"
		if driver == "pgsql" || driver == "postgres" {
			placeholder = fmt.Sprintf("$%d", i+1)
		}
		where[i] = seedQuoteIdentifier(driver, column) + " = " + placeholder
		args[i] = row.Values[column]
	}
	query := "SELECT " + seedQuotedColumns(driver, columns) + " FROM " + seedQuoteIdentifier(driver, row.Table) + " WHERE " + strings.Join(where, " AND ") + " LIMIT 1"
	dest := make([]any, len(columns))
	holders := make([]any, len(columns))
	for i := range dest {
		dest[i] = &holders[i]
	}
	err := tx.QueryRowContext(ctx, query, args...).Scan(dest...)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for i, column := range columns {
		if comparableSeedValue(holders[i]) != comparableSeedValue(row.Values[column]) {
			return false, fmt.Errorf("existing repeat identity for %s differs at column %s", row.Table, column)
		}
	}
	return true, nil
}

func comparableSeedValue(value any) string {
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	case [16]byte:
		return formatSeedUUID(typed[:])
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(value)
	}
}

func computeSeedRowHash(prev []byte, values map[string]any, columns []*Column) []byte {
	h := sha256.New()
	h.Write(prev)
	h.Write(canonicalSeedRow(values, columns))
	return h.Sum(nil)
}

// ComputeSeedIntegrityHash exposes the canonical seeder hash for compatibility
// checks and tooling that validates a planned immutable or append-only row.
func ComputeSeedIntegrityHash(prev []byte, values map[string]any, columns []*Column) []byte {
	return computeSeedRowHash(prev, values, columns)
}

func canonicalSeedRow(values map[string]any, columns []*Column) []byte {
	var out bytes.Buffer
	for _, column := range columns {
		if column.Name == "row_hash" || column.Name == "prev_hash" {
			continue
		}
		out.WriteString(column.Name)
		out.WriteByte(0)
		value, ok := values[column.Name]
		if !ok || value == nil {
			out.WriteByte(0)
			out.WriteByte(0)
			continue
		}
		tag := seedIntegrityTypeTag(column.Type)
		out.WriteByte(tag)
		writeCanonicalSeedValue(&out, tag, value)
		out.WriteByte(0)
	}
	return out.Bytes()
}

func seedIntegrityTypeTag(kind ColumnType) byte {
	switch kind {
	case UUID:
		return 1
	case String, Text:
		return 2
	case Integer:
		return 3
	case BigInteger:
		return 4
	case Decimal:
		return 5
	case Boolean:
		return 6
	case Timestamp:
		return 7
	case JSONB:
		return 8
	case Binary:
		return 9
	case Date:
		return 10
	case Time:
		return 11
	case Float:
		return 12
	case Double:
		return 13
	}
	return 2
}

func writeCanonicalSeedValue(out *bytes.Buffer, tag byte, value any) {
	switch tag {
	case 1:
		switch typed := value.(type) {
		case [16]byte:
			out.Write(typed[:])
		case []byte:
			if len(typed) == 16 {
				out.Write(typed)
			} else if parsed, ok := parseSeedUUID(string(typed)); ok {
				out.Write(parsed)
			}
		default:
			if parsed, ok := parseSeedUUID(fmt.Sprint(value)); ok {
				out.Write(parsed)
			}
		}
	case 2, 5:
		out.WriteString(fmt.Sprint(value))
	case 3, 4:
		parsed, _ := strconv.ParseInt(fmt.Sprint(value), 10, 64)
		var data [8]byte
		binary.BigEndian.PutUint64(data[:], uint64(parsed))
		out.Write(data[:])
	case 6:
		if parsed, _ := strconv.ParseBool(fmt.Sprint(value)); parsed {
			out.WriteByte(1)
		} else {
			out.WriteByte(0)
		}
	case 7:
		if parsed, ok := value.(time.Time); ok {
			parsed = parsed.UTC().Truncate(time.Microsecond)
			var data [8]byte
			binary.BigEndian.PutUint64(data[:], uint64(parsed.UnixNano()))
			out.Write(data[:])
		}
	case 8:
		var raw []byte
		switch typed := value.(type) {
		case []byte:
			raw = typed
		case json.RawMessage:
			raw = typed
		default:
			raw, _ = json.Marshal(value)
		}
		var compact bytes.Buffer
		if json.Compact(&compact, raw) == nil {
			out.Write(compact.Bytes())
		}
	case 9:
		if raw, ok := value.([]byte); ok {
			out.Write(raw)
		}
	case 10:
		if parsed, ok := value.(time.Time); ok {
			out.WriteString(parsed.UTC().Format("2006-01-02"))
		}
	case 11:
		if parsed, ok := value.(time.Time); ok {
			out.WriteString(parsed.UTC().Format("15:04:05"))
		}
	}
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
