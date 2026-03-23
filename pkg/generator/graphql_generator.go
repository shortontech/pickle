package generator

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shortontech/pickle/pkg/schema"
)

const graphqlPackageName = "graphql"

// GenerateGraphQL orchestrates the complete GraphQL layer generation.
func GenerateGraphQL(project *Project, tables []*schema.Table, relationships []SchemaRelationship, requests []RequestDef) error {
	graphqlDir := filepath.Join(project.Dir, "app", "graphql")
	modelsImport := project.ModulePath + "/app/models"

	if err := os.MkdirAll(graphqlDir, 0o755); err != nil {
		return fmt.Errorf("creating graphql dir: %w", err)
	}

	// Filter to only tables that should be exposed in GraphQL
	var exposedTables []*schema.Table
	for _, tbl := range tables {
		if isGraphQLExposed(tbl) {
			exposedTables = append(exposedTables, tbl)
		}
	}
	tables = exposedTables

	// 1. Write tickled core (executor, batch loader, ResolveContext)
	if !hasOverride(graphqlDir, "pickle.go") {
		fmt.Println("  generating graphql/pickle_gen.go")
		if err := writeFile(filepath.Join(graphqlDir, "pickle_gen.go"), GenerateCoreGraphQL(graphqlPackageName)); err != nil {
			return err
		}
	}

	// 2. Schema SDL
	if !hasOverride(graphqlDir, "schema.go") {
		fmt.Println("  generating graphql/schema_gen.go")
		src, err := GenerateGraphQLSchema(tables, relationships, requests, graphqlPackageName)
		if err != nil {
			return fmt.Errorf("schema generation: %w", err)
		}
		if err := writeFile(filepath.Join(graphqlDir, "schema_gen.go"), src); err != nil {
			return err
		}
	}

	// 3. GQL types
	if !hasOverride(graphqlDir, "types.go") {
		fmt.Println("  generating graphql/types_gen.go")
		src, err := GenerateGraphQLTypes(tables, requests, graphqlPackageName)
		if err != nil {
			return fmt.Errorf("types generation: %w", err)
		}
		if err := writeFile(filepath.Join(graphqlDir, "types_gen.go"), src); err != nil {
			return err
		}
	}

	// 4. Resolvers
	if !hasOverride(graphqlDir, "resolver.go") {
		fmt.Println("  generating graphql/resolver_gen.go")
		src, err := GenerateGraphQLResolvers(tables, relationships, modelsImport, graphqlPackageName)
		if err != nil {
			return fmt.Errorf("resolver generation: %w", err)
		}
		if err := writeFile(filepath.Join(graphqlDir, "resolver_gen.go"), src); err != nil {
			return err
		}
	}

	// 5. Mutations
	if !hasOverride(graphqlDir, "mutation.go") {
		fmt.Println("  generating graphql/mutation_gen.go")
		src, err := GenerateGraphQLMutations(tables, modelsImport, graphqlPackageName, relationships)
		if err != nil {
			return fmt.Errorf("mutation generation: %w", err)
		}
		if err := writeFile(filepath.Join(graphqlDir, "mutation_gen.go"), src); err != nil {
			return err
		}
	}

	// 6. Dataloaders
	if !hasOverride(graphqlDir, "dataloader.go") {
		fmt.Println("  generating graphql/dataloader_gen.go")
		src, err := GenerateGraphQLDataloaders(tables, relationships, modelsImport, graphqlPackageName)
		if err != nil {
			return fmt.Errorf("dataloader generation: %w", err)
		}
		if err := writeFile(filepath.Join(graphqlDir, "dataloader_gen.go"), src); err != nil {
			return err
		}
	}

	// 7. Handler
	if !hasOverride(graphqlDir, "handler.go") {
		fmt.Println("  generating graphql/handler_gen.go")
		src, err := GenerateGraphQLHandler(graphqlPackageName)
		if err != nil {
			return fmt.Errorf("handler generation: %w", err)
		}
		if err := writeFile(filepath.Join(graphqlDir, "handler_gen.go"), src); err != nil {
			return err
		}
	}

	// 8. Zero-controller CRUD resolvers (spec 018)
	// Generate CRUD resolvers for tables that don't have user-written resolver overrides.
	var crudTables []*schema.Table
	for _, tbl := range tables {
		if !HasCRUDOverride(graphqlDir, tbl.Name) {
			crudTables = append(crudTables, tbl)
		}
	}
	if len(crudTables) > 0 && !hasOverride(graphqlDir, "crud_resolver.go") {
		fmt.Println("  generating graphql/crud_resolver_gen.go")
		src, err := GenerateGraphQLCRUDResolvers(CRUDConfig{
			Tables:        crudTables,
			Relationships: relationships,
			ModelsImport:  modelsImport,
			PackageName:   graphqlPackageName,
		})
		if err != nil {
			return fmt.Errorf("crud resolver generation: %w", err)
		}
		if err := writeFile(filepath.Join(graphqlDir, "crud_resolver_gen.go"), src); err != nil {
			return err
		}
	}

	return nil
}

// hasOverride checks if a user-written override file exists.
func hasOverride(dir, filename string) bool {
	_, err := os.Stat(filepath.Join(dir, filename))
	return err == nil
}
