package generator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// initAuditMigrations populates the audit migration list from embed constants.
func initAuditMigrations() []struct {
	Filename string
	Embed    string
} {
	return []struct {
		Filename string
		Embed    string
	}{
		{Filename: "2026_03_25_000001_create_model_types_table", Embed: embed_2026_03_25_000001_create_model_types_table},
		{Filename: "2026_03_25_000002_create_action_types_table", Embed: embed_2026_03_25_000002_create_action_types_table},
		{Filename: "2026_03_25_000003_create_user_actions_table", Embed: embed_2026_03_25_000003_create_user_actions_table},
	}
}

// initAuditModels populates the audit model list from embed constants.
func initAuditModels() []struct {
	Filename string
	Embed    string
} {
	return []struct {
		Filename string
		Embed    string
	}{
		{Filename: "model_type", Embed: embed_model_type},
		{Filename: "model_type_query", Embed: embed_model_type_query},
		{Filename: "action_type", Embed: embed_action_type},
		{Filename: "action_type_query", Embed: embed_action_type_query},
		{Filename: "user_action", Embed: embed_user_action},
		{Filename: "user_action_query", Embed: embed_user_action_query},
		{Filename: "performed", Embed: embed_performed},
	}
}

// WriteAuditMigrations writes audit trail migration files into the project's
// database/migrations/audit/ directory. Override pattern applies.
func WriteAuditMigrations(migrationsDir, migrationsPkg string) error {
	auditDir := filepath.Join(migrationsDir, "audit")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		return err
	}

	// Write schema types so migration files can reference Migration, Table, etc.
	schemaTypes := GenerateCoreSchema(migrationsPkg)
	if err := os.WriteFile(filepath.Join(auditDir, "types_gen.go"), schemaTypes, 0o644); err != nil {
		return err
	}

	for _, m := range initAuditMigrations() {
		genFilename := m.Filename + "_gen.go"
		userFilename := m.Filename + ".go"

		if _, exists := statFile(filepath.Join(auditDir, userFilename)); exists {
			continue
		}

		src := strings.ReplaceAll(m.Embed, packagePlaceholder, migrationsPkg)
		if err := os.WriteFile(filepath.Join(auditDir, genFilename), []byte(src), 0o644); err != nil {
			return err
		}
	}

	return nil
}

// WriteAuditModels writes audit model files into the project's
// app/models/audit/ directory. Includes QueryBuilder and Context types.
func WriteAuditModels(modelsDir string) error {
	auditDir := filepath.Join(modelsDir, "audit")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		return err
	}

	auditPkg := "audit"

	// Write QueryBuilder so model query types compile.
	queryTypes := GenerateCoreQuery(auditPkg)
	if err := os.WriteFile(filepath.Join(auditDir, "pickle_gen.go"), queryTypes, 0o644); err != nil {
		return err
	}

	for _, m := range initAuditModels() {
		genFilename := m.Filename + "_gen.go"
		userFilename := m.Filename + ".go"

		if _, exists := statFile(filepath.Join(auditDir, userFilename)); exists {
			continue
		}

		src := strings.ReplaceAll(m.Embed, packagePlaceholder, auditPkg)
		if err := os.WriteFile(filepath.Join(auditDir, genFilename), []byte(src), 0o644); err != nil {
			return err
		}
	}

	return nil
}

// IDRegistry tracks permanently assigned integer IDs for model_types and
// action_types. Once an ID is assigned, it is never reused, even if the
// corresponding model or action is removed from the codebase.
type IDRegistry struct {
	NextModelTypeID  int                  `json:"next_model_type_id"`
	NextActionTypeID int                  `json:"next_action_type_id"`
	ModelTypes       map[string]int       `json:"model_types"`  // model name → ID
	ActionTypes      map[string]ActionReg `json:"action_types"` // "Model.Action" → registration
}

// ActionReg stores the registered ID and model type link for an action type.
type ActionReg struct {
	ID          int    `json:"id"`
	ModelTypeID int    `json:"model_type_id"`
	Model       string `json:"model"`
	Action      string `json:"action"`
}

// LoadIDRegistry reads the registry from disk, or returns a fresh one.
func LoadIDRegistry(path string) (*IDRegistry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &IDRegistry{
				NextModelTypeID:  1,
				NextActionTypeID: 1,
				ModelTypes:       map[string]int{},
				ActionTypes:      map[string]ActionReg{},
			}, nil
		}
		return nil, err
	}
	var reg IDRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parsing id_registry.json: %w", err)
	}
	if reg.ModelTypes == nil {
		reg.ModelTypes = map[string]int{}
	}
	if reg.ActionTypes == nil {
		reg.ActionTypes = map[string]ActionReg{}
	}
	return &reg, nil
}

// SaveIDRegistry writes the registry to disk.
func SaveIDRegistry(path string, reg *IDRegistry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// EnsureModelType assigns a stable ID for a model name.
func (r *IDRegistry) EnsureModelType(name string) int {
	if id, ok := r.ModelTypes[name]; ok {
		return id
	}
	id := r.NextModelTypeID
	r.NextModelTypeID++
	r.ModelTypes[name] = id
	return id
}

// EnsureActionType assigns a stable ID for a model+action pair.
func (r *IDRegistry) EnsureActionType(model, action string) int {
	key := model + "." + action
	if reg, ok := r.ActionTypes[key]; ok {
		return reg.ID
	}
	modelTypeID := r.EnsureModelType(model)
	id := r.NextActionTypeID
	r.NextActionTypeID++
	r.ActionTypes[key] = ActionReg{
		ID:          id,
		ModelTypeID: modelTypeID,
		Model:       model,
		Action:      action,
	}
	return id
}

// GenerateAuditSeed produces the seed migration Go source that inserts model_types
// and action_types rows via idempotent INSERT ON CONFLICT DO NOTHING statements.
func GenerateAuditSeed(reg *IDRegistry, packageName string) ([]byte, error) {
	// Collect and sort model types
	type modelEntry struct {
		Name string
		ID   int
	}
	var models []modelEntry
	for name, id := range reg.ModelTypes {
		models = append(models, modelEntry{Name: name, ID: id})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })

	// Collect and sort action types
	type actionEntry struct {
		Key string
		Reg ActionReg
	}
	var actions []actionEntry
	for key, ar := range reg.ActionTypes {
		actions = append(actions, actionEntry{Key: key, Reg: ar})
	}
	sort.Slice(actions, func(i, j int) bool { return actions[i].Reg.ID < actions[j].Reg.ID })

	var buf bytes.Buffer
	buf.WriteString("// Code generated by Pickle. DO NOT EDIT.\n\n")
	buf.WriteString(fmt.Sprintf("package %s\n\n", packageName))
	buf.WriteString("// SeedActionTypes_2026_03_25_000004 seeds model_types and action_types.\n")
	buf.WriteString("type SeedActionTypes_2026_03_25_000004 struct {\n\tMigration\n}\n\n")
	buf.WriteString("func (m *SeedActionTypes_2026_03_25_000004) Up() {\n")

	for _, mt := range models {
		buf.WriteString(fmt.Sprintf("\tm.RawSQL(`INSERT INTO model_types (id, name) VALUES (%d, '%s') ON CONFLICT (id) DO NOTHING`)\n", mt.ID, mt.Name))
	}
	if len(models) > 0 && len(actions) > 0 {
		buf.WriteString("\n")
	}
	for _, at := range actions {
		buf.WriteString(fmt.Sprintf("\tm.RawSQL(`INSERT INTO action_types (id, model_type_id, name) VALUES (%d, %d, '%s') ON CONFLICT (id) DO NOTHING`)\n",
			at.Reg.ID, at.Reg.ModelTypeID, at.Reg.Action))
	}

	buf.WriteString("}\n\n")
	buf.WriteString("func (m *SeedActionTypes_2026_03_25_000004) Down() {\n")
	buf.WriteString("\t// Append-only: seed data is never removed.\n")
	buf.WriteString("}\n")

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return buf.Bytes(), fmt.Errorf("formatting audit seed: %w\n%s", err, buf.String())
	}
	return formatted, nil
}

// GenerateAuditConstants produces a Go source file with constants for each
// action type ID, e.g. ActionTypeUserBan = 1.
func GenerateAuditConstants(reg *IDRegistry, packageName string) ([]byte, error) {
	type entry struct {
		Key string
		Reg ActionReg
	}
	var entries []entry
	for key, ar := range reg.ActionTypes {
		entries = append(entries, entry{Key: key, Reg: ar})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Reg.ID < entries[j].Reg.ID })

	var buf bytes.Buffer
	buf.WriteString("// Code generated by Pickle. DO NOT EDIT.\n\n")
	buf.WriteString(fmt.Sprintf("package %s\n\n", packageName))

	if len(entries) > 0 {
		buf.WriteString("const (\n")
		for _, e := range entries {
			constName := "ActionType" + tableToStructName(e.Reg.Model) + e.Reg.Action
			buf.WriteString(fmt.Sprintf("\t%s = %d\n", constName, e.Reg.ID))
		}
		buf.WriteString(")\n")
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return buf.Bytes(), fmt.Errorf("formatting audit constants: %w\n%s", err, buf.String())
	}
	return formatted, nil
}

// WriteAuditSeedAndConstants scans actions, updates the ID registry, and writes
// the seed migration and constants files.
func WriteAuditSeedAndConstants(project *Project, actionSets map[string]*ActionSet) error {
	reg := &IDRegistry{
		NextModelTypeID:  1,
		NextActionTypeID: 1,
		ModelTypes:       map[string]int{},
		ActionTypes:      map[string]ActionReg{},
	}

	// Register all model+action pairs deterministically from the current source.
	// This keeps generation/export self-contained without requiring a sidecar JSON file.
	var models []string
	setsByModel := map[string]*ActionSet{}
	for model, set := range actionSets {
		models = append(models, model)
		setsByModel[model] = set
	}
	sort.Strings(models)
	for _, model := range models {
		set := setsByModel[model]
		modelName := tableToStructName(set.Model)
		reg.EnsureModelType(modelName)
		sort.Slice(set.Actions, func(i, j int) bool { return set.Actions[i].Name < set.Actions[j].Name })
		for _, action := range set.Actions {
			reg.EnsureActionType(modelName, action.Name)
		}
	}

	// Write seed migration
	migrationsDir := filepath.Join(project.Dir, "database", "migrations", "audit")
	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		return err
	}

	seedFilename := "2026_03_25_000004_seed_action_types_gen.go"
	userFilename := "2026_03_25_000004_seed_action_types.go"
	if _, exists := statFile(filepath.Join(migrationsDir, userFilename)); !exists {
		seedSrc, err := GenerateAuditSeed(reg, "audit")
		if err != nil {
			return fmt.Errorf("generating audit seed: %w", err)
		}
		if err := os.WriteFile(filepath.Join(migrationsDir, seedFilename), seedSrc, 0o644); err != nil {
			return err
		}
	}

	// Write constants
	modelsDir := filepath.Join(project.Dir, "app", "models", "audit")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		return err
	}

	constSrc, err := GenerateAuditConstants(reg, "audit")
	if err != nil {
		return fmt.Errorf("generating audit constants: %w", err)
	}
	return os.WriteFile(filepath.Join(modelsDir, "constants_gen.go"), constSrc, 0o644)
}
