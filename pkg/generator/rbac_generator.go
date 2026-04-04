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
func WriteRBACMigrations(migrationsDir, _ string) error {
	rbacDir := filepath.Join(migrationsDir, "rbac")
	if err := os.MkdirAll(rbacDir, 0o755); err != nil {
		return err
	}

	// Subdirectory package must match directory name for valid Go.
	rbacPkg := "rbac"

	// Write schema types so migration files can reference Migration, Table, etc.
	schemaTypes := GenerateCoreSchema(rbacPkg)
	if err := os.WriteFile(filepath.Join(rbacDir, "types_gen.go"), schemaTypes, 0o644); err != nil {
		return err
	}

	for _, m := range initRBACMigrations() {
		genFilename := m.Filename + "_gen.go"
		userFilename := m.Filename + ".go"

		if _, exists := statFile(filepath.Join(rbacDir, userFilename)); exists {
			continue
		}

		src := strings.ReplaceAll(m.Embed, packagePlaceholder, rbacPkg)
		if err := os.WriteFile(filepath.Join(rbacDir, genFilename), []byte(src), 0o644); err != nil {
			return err
		}
	}

	return nil
}

// initRBACModels returns the list of RBAC model embeds for app/models/auth/.
func initRBACModels() []struct {
	Filename string
	Embed    string
} {
	return []struct {
		Filename string
		Embed    string
	}{
		{Filename: "role", Embed: embed_role},
		{Filename: "role_query", Embed: embed_role_query},
		{Filename: "role_user", Embed: embed_role_user},
		{Filename: "role_user_query", Embed: embed_role_user_query},
	}
}

// WriteRBACModels writes Role and RoleUser model structs + query scopes
// into app/models/auth/. Override pattern applies.
func WriteRBACModels(modelsDir string) error {
	authDir := filepath.Join(modelsDir, "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		return err
	}

	authPkg := "auth"

	// Write QueryBuilder so model query types compile.
	queryTypes := GenerateCoreQuery(authPkg)
	if err := os.WriteFile(filepath.Join(authDir, "pickle_gen.go"), queryTypes, 0o644); err != nil {
		return err
	}

	for _, m := range initRBACModels() {
		genFilename := m.Filename + "_gen.go"
		userFilename := m.Filename + ".go"

		if _, exists := statFile(filepath.Join(authDir, userFilename)); exists {
			continue
		}

		src := strings.ReplaceAll(m.Embed, packagePlaceholder, authPkg)
		if err := os.WriteFile(filepath.Join(authDir, genFilename), []byte(src), 0o644); err != nil {
			return err
		}
	}

	return nil
}

// WriteGraphQLMigrations writes GraphQL migration files into the project's
// database/migrations/graphql/ directory. Override pattern applies.
func WriteGraphQLMigrations(migrationsDir, _ string) error {
	graphqlDir := filepath.Join(migrationsDir, "graphql")
	if err := os.MkdirAll(graphqlDir, 0o755); err != nil {
		return err
	}

	// Subdirectory package must match directory name for valid Go.
	gqlMigPkg := "graphql"

	// Write schema types so migration files can reference Migration, Table, etc.
	schemaTypes := GenerateCoreSchema(gqlMigPkg)
	if err := os.WriteFile(filepath.Join(graphqlDir, "types_gen.go"), schemaTypes, 0o644); err != nil {
		return err
	}

	for _, m := range initGraphQLMigrations() {
		genFilename := m.Filename + "_gen.go"
		userFilename := m.Filename + ".go"

		if _, exists := statFile(filepath.Join(graphqlDir, userFilename)); exists {
			continue
		}

		src := strings.ReplaceAll(m.Embed, packagePlaceholder, gqlMigPkg)
		if err := os.WriteFile(filepath.Join(graphqlDir, genFilename), []byte(src), 0o644); err != nil {
			return err
		}
	}

	return nil
}
