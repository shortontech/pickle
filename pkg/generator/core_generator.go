package generator

import "strings"

const packagePlaceholder = "__PACKAGE__"

// GenerateCoreHTTP returns the HTTP core types (Context, Response, Router, etc.)
// with the package name set to the target package.
func GenerateCoreHTTP(packageName string) []byte {
	return []byte(strings.ReplaceAll(embedHTTP, packagePlaceholder, packageName))
}

// GenerateCoreQuery returns the QueryBuilder[T] and related query types
// with the package name set to the target package.
func GenerateCoreQuery(packageName string) []byte {
	return []byte(strings.ReplaceAll(embedQUERY, packagePlaceholder, packageName))
}

// GenerateCoreSchema returns the migration schema types (Migration, Table, Column, etc.)
// with the package name set to the target package.
func GenerateCoreSchema(packageName string) []byte {
	return []byte(strings.ReplaceAll(embedSCHEMA, packagePlaceholder, packageName))
}

// GenerateCoreConfig returns the config helpers (Env, .env loader, DatabaseConfig, etc.)
// with the package name set to the target package.
func GenerateCoreConfig(packageName string) []byte {
	return []byte(strings.ReplaceAll(embedCONFIG, packagePlaceholder, packageName))
}

// GenerateCoreMigration returns the migration runner and SQL generators
// with the package name set to the target package.
func GenerateCoreMigration(packageName string) []byte {
	return []byte(strings.ReplaceAll(embedMIGRATION, packagePlaceholder, packageName))
}
