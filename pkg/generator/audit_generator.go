package generator

import (
	"os"
	"path/filepath"
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

// WriteAuditMigrations writes audit trail migration files into the project's
// database/migrations/audit/ directory. Override pattern applies.
func WriteAuditMigrations(migrationsDir, migrationsPkg string) error {
	auditDir := filepath.Join(migrationsDir, "audit")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
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
