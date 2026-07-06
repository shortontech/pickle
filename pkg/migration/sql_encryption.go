//go:build ignore

package migration

// expandColumns rewrites encrypted/sealed columns into their physical storage
// columns before DDL is emitted. A plaintext column such as `email` is never
// stored directly; the model and scope generators read `email_encrypted` (and
// `email_encrypted_v2` during key rotation), so the migration DDL must create
// exactly those physical columns. Non-encrypted columns pass through unchanged.
func expandColumns(cols []*Column) []*Column {
	out := make([]*Column, 0, len(cols))
	for _, col := range cols {
		out = append(out, expandColumn(col)...)
	}
	return out
}

// expandColumn returns the physical storage columns for a single declared
// column. For an encrypted/sealed column `c` it returns:
//
//	c.Name + "_encrypted"      TEXT, preserving c's nullability
//	c.Name + "_encrypted_v2"   TEXT, always nullable (key-rotation slot)
//
// The bare `c.Name` column is never emitted. Uniqueness is preserved on the
// `_encrypted` column only for deterministic (.Encrypted()) columns, where
// equal plaintext yields equal ciphertext (AES-SIV). Sealed (.Sealed()) columns
// use non-deterministic encryption, so any uniqueness/index constraint is
// meaningless and is dropped.
func expandColumn(col *Column) []*Column {
	if !col.IsEncrypted && !col.IsSealed {
		return []*Column{col}
	}
	primary := encryptedStorageColumn(col, col.Name+"_encrypted", col.IsNullable)
	if col.IsEncrypted && col.IsUnique {
		primary.IsUnique = true
	}
	v2 := encryptedStorageColumn(col, col.Name+"_encrypted_v2", true)
	return []*Column{primary, v2}
}

// encryptedStorageColumn derives a physical TEXT storage column from a declared
// encrypted/sealed column. Ciphertext is stored as opaque TEXT, so the declared
// type (e.g. VARCHAR) is discarded, and encryption-only attributes (primary key,
// foreign key, default, uniqueness) are cleared — callers re-apply uniqueness
// where it is meaningful.
func encryptedStorageColumn(src *Column, name string, nullable bool) *Column {
	col := *src
	col.Name = name
	col.Type = Text
	col.IsEncrypted = false
	col.IsSealed = false
	col.IsNullable = nullable
	col.IsPrimaryKey = false
	col.IsUnique = false
	col.ForeignKeyTable = ""
	col.ForeignKeyColumn = ""
	col.HasDefault = false
	col.DefaultValue = nil
	return &col
}
