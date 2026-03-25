package generator

import (
	"os"
	"path/filepath"
	"strings"
)

// builtinRBACMigrations lists the RBAC migration embeds.
var builtinRBACMigrations = []struct {
	Filename string
	Embed    *string // pointer to constant — set in init
}{}

// builtinGraphQLMigrations lists the GraphQL migration embeds.
var builtinGraphQLMigrations = []struct {
	Filename string
	Embed    *string
}{}

// initRBACMigrations populates the migration lists from embed constants.
// Called lazily on first use because Go const addresses aren't available at init time.
func initRBACMigrations() []struct {
	Filename string
	Embed    string
} {
	return []struct {
		Filename string
		Embed    string
	}{
		{Filename: "2026_03_23_000001_create_roles_table", Embed: embed_2026_03_23_000001_create_roles_table},
		{Filename: "2026_03_23_000002_create_role_actions_table", Embed: embed_2026_03_23_000002_create_role_actions_table},
		{Filename: "2026_03_23_000003_create_role_user_table", Embed: embed_2026_03_23_000003_create_role_user_table},
		{Filename: "2026_03_23_000004_create_rbac_changelog_table", Embed: embed_2026_03_23_000004_create_rbac_changelog_table},
	}
}

func initGraphQLMigrations() []struct {
	Filename string
	Embed    string
} {
	return []struct {
		Filename string
		Embed    string
	}{
		{Filename: "2026_03_25_000001_create_graphql_changelog_table", Embed: embed_2026_03_25_000001_create_graphql_changelog_table},
		{Filename: "2026_03_25_000002_create_graphql_exposures_table", Embed: embed_2026_03_25_000002_create_graphql_exposures_table},
		{Filename: "2026_03_25_000003_create_graphql_actions_table", Embed: embed_2026_03_25_000003_create_graphql_actions_table},
	}
}

// WriteRBACMigrations writes RBAC migration files into the project's
// database/migrations/rbac/ directory. Override pattern applies.
func WriteRBACMigrations(migrationsDir, migrationsPkg string) error {
	rbacDir := filepath.Join(migrationsDir, "rbac")
	if err := os.MkdirAll(rbacDir, 0o755); err != nil {
		return err
	}

	for _, m := range initRBACMigrations() {
		genFilename := m.Filename + "_gen.go"
		userFilename := m.Filename + ".go"

		if _, exists := statFile(filepath.Join(rbacDir, userFilename)); exists {
			continue
		}

		src := strings.ReplaceAll(m.Embed, packagePlaceholder, migrationsPkg)
		if err := os.WriteFile(filepath.Join(rbacDir, genFilename), []byte(src), 0o644); err != nil {
			return err
		}
	}

	return nil
}

// WriteGraphQLMigrations writes GraphQL migration files into the project's
// database/migrations/graphql/ directory. Override pattern applies.
func WriteGraphQLMigrations(migrationsDir, migrationsPkg string) error {
	graphqlDir := filepath.Join(migrationsDir, "graphql")
	if err := os.MkdirAll(graphqlDir, 0o755); err != nil {
		return err
	}

	for _, m := range initGraphQLMigrations() {
		genFilename := m.Filename + "_gen.go"
		userFilename := m.Filename + ".go"

		if _, exists := statFile(filepath.Join(graphqlDir, userFilename)); exists {
			continue
		}

		src := strings.ReplaceAll(m.Embed, packagePlaceholder, migrationsPkg)
		if err := os.WriteFile(filepath.Join(graphqlDir, genFilename), []byte(src), 0o644); err != nil {
			return err
		}
	}

	return nil
}
