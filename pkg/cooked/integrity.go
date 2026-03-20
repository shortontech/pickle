package cooked

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"reflect"
	"time"
)

// Type tags for canonical serialization. Each column type gets a unique tag
// to prevent type confusion in the hash (e.g., string "42" vs integer 42).
const (
	typeTagNull      byte = 0x00
	typeTagUUID      byte = 0x01
	typeTagString    byte = 0x02
	typeTagInteger   byte = 0x03
	typeTagBigInt    byte = 0x04
	typeTagDecimal   byte = 0x05
	typeTagBoolean   byte = 0x06
	typeTagTimestamp  byte = 0x07
	typeTagJSONB     byte = 0x08
	typeTagBinary    byte = 0x09
	typeTagDate      byte = 0x0A
	typeTagTime      byte = 0x0B
)

// GenesisHash is the prev_hash for the first row in a chain: 32 zero bytes.
var GenesisHash = make([]byte, 32)

// ColumnMeta carries column name and type tag for canonical serialization.
// The generator emits a slice of these for each integrity-enabled table.
type ColumnMeta struct {
	Name    string
	TypeTag byte
}

// computeRowHash computes SHA-256(prevHash || canonical(record)) for a record,
// using the provided column metadata to serialize fields deterministically.
// Columns named "row_hash" and "prev_hash" are excluded from the serialization.
func computeRowHash(prevHash []byte, record any, columns []ColumnMeta) []byte {
	canonical := canonicalSerialize(record, columns)
	h := sha256.New()
	h.Write(prevHash)
	h.Write(canonical)
	return h.Sum(nil)
}

// canonicalSerialize produces a deterministic byte sequence from a record's
// fields, using the column metadata for type-tagged encoding.
//
// Format per column: column_name_bytes || 0x00 || type_tag (1 byte) || value_bytes || 0x00
// NULL values: column_name_bytes || 0x00 || 0x00 (typeTagNull, no value bytes) || 0x00
func canonicalSerialize(record any, columns []ColumnMeta) []byte {
	rv := reflect.ValueOf(record)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	rt := rv.Type()

	// Build a map from db tag to field index for fast lookup
	dbFieldIndex := map[string]int{}
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("db")
		if tag != "" && tag != "-" {
			dbFieldIndex[tag] = i
		}
	}

	var buf bytes.Buffer
	for _, col := range columns {
		// Skip integrity columns themselves
		if col.Name == "row_hash" || col.Name == "prev_hash" {
			continue
		}

		// Write column name + null delimiter
		buf.WriteString(col.Name)
		buf.WriteByte(0x00)

		idx, ok := dbFieldIndex[col.Name]
		if !ok {
			// Column not in struct — treat as NULL
			buf.WriteByte(typeTagNull)
			buf.WriteByte(0x00)
			continue
		}

		field := rv.Field(idx)
		serializeField(&buf, field, col.TypeTag)
	}

	return buf.Bytes()
}

// serializeField writes a single field's type tag and value bytes.
func serializeField(buf *bytes.Buffer, field reflect.Value, typeTag byte) {
	// Check for nil pointer (nullable field)
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			buf.WriteByte(typeTagNull)
			buf.WriteByte(0x00)
			return
		}
		field = field.Elem()
	}

	buf.WriteByte(typeTag)

	switch typeTag {
	case typeTagUUID:
		// UUID is [16]byte — write raw bytes
		if field.Kind() == reflect.Array && field.Len() == 16 {
			for i := 0; i < 16; i++ {
				buf.WriteByte(byte(field.Index(i).Uint()))
			}
		}

	case typeTagString:
		buf.WriteString(field.String())

	case typeTagInteger, typeTagBigInt:
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(field.Int()))
		buf.Write(b[:])

	case typeTagDecimal:
		// Decimal is serialized via its String() method for determinism
		if stringer, ok := field.Interface().(fmt.Stringer); ok {
			buf.WriteString(stringer.String())
		}

	case typeTagBoolean:
		if field.Bool() {
			buf.WriteByte(0x01)
		} else {
			buf.WriteByte(0x00)
		}

	case typeTagTimestamp:
		// time.Time → Unix nanoseconds UTC
		if t, ok := field.Interface().(time.Time); ok {
			var b [8]byte
			binary.BigEndian.PutUint64(b[:], uint64(t.UTC().UnixNano()))
			buf.Write(b[:])
		}

	case typeTagJSONB:
		// json.RawMessage or []byte — compact the JSON
		var jsonBytes []byte
		switch v := field.Interface().(type) {
		case json.RawMessage:
			jsonBytes = v
		case []byte:
			jsonBytes = v
		}
		if jsonBytes != nil {
			var compacted bytes.Buffer
			if err := json.Compact(&compacted, jsonBytes); err == nil {
				buf.Write(compacted.Bytes())
			} else {
				buf.Write(jsonBytes) // fallback: use as-is
			}
		}

	case typeTagBinary:
		if b, ok := field.Interface().([]byte); ok {
			buf.Write(b)
		}

	case typeTagDate:
		// time.Time → "YYYY-MM-DD"
		if t, ok := field.Interface().(time.Time); ok {
			buf.WriteString(t.UTC().Format("2006-01-02"))
		}

	case typeTagTime:
		// time.Time → "HH:MM:SS"
		if t, ok := field.Interface().(time.Time); ok {
			buf.WriteString(t.UTC().Format("15:04:05"))
		}
	}

	buf.WriteByte(0x00)
}

// ChainError is returned when VerifyChain detects a broken hash chain link.
type ChainError struct {
	Table    string
	RowID    string
	Expected []byte // recomputed hash
	Actual   []byte // stored row_hash
	Position int    // row position in chain (0-based)
}

func (e *ChainError) Error() string {
	return fmt.Sprintf(
		"chain integrity error on %s at position %d (id=%s): expected hash %x but found %x",
		e.Table, e.Position, e.RowID, e.Expected, e.Actual,
	)
}

// verifyChain walks all rows in chain order for a table, recomputing each
// row_hash and checking it matches the stored value. Returns nil if the chain
// is intact. Returns a ChainError identifying the first broken link.
//
// This function operates on raw database rows via the provided dbExecutor.
// The caller passes the table name, chain order SQL, and column metadata.
func verifyChain(db dbExecutor, table string, chainOrderSQL string, columns []ColumnMeta) error {
	rows, err := db.Query(chainOrderSQL)
	if err != nil {
		return fmt.Errorf("verifying chain for %s: %w", table, err)
	}
	defer rows.Close()

	prevHash := GenesisHash
	position := 0

	for rows.Next() {
		// Scan all columns into a map
		colNames, err := rows.Columns()
		if err != nil {
			return err
		}
		values := make([]any, len(colNames))
		valuePtrs := make([]any, len(colNames))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return err
		}

		// Find row_hash, prev_hash, and id columns
		colMap := map[string]any{}
		for i, name := range colNames {
			colMap[name] = values[i]
		}

		storedRowHash, _ := colMap["row_hash"].([]byte)
		storedPrevHash, _ := colMap["prev_hash"].([]byte)

		// Verify prev_hash matches our running chain
		if !bytes.Equal(storedPrevHash, prevHash) {
			rowID := fmt.Sprintf("%v", colMap["id"])
			return &ChainError{
				Table:    table,
				RowID:    rowID,
				Expected: prevHash,
				Actual:   storedPrevHash,
				Position: position,
			}
		}

		// Recompute the hash from the row data
		// For verification, we build a canonical representation from the raw values
		recomputed := computeRowHashFromRaw(prevHash, colMap, columns)

		if !bytes.Equal(recomputed, storedRowHash) {
			rowID := fmt.Sprintf("%v", colMap["id"])
			return &ChainError{
				Table:    table,
				RowID:    rowID,
				Expected: recomputed,
				Actual:   storedRowHash,
				Position: position,
			}
		}

		prevHash = storedRowHash
		position++
	}

	return rows.Err()
}

// computeRowHashFromRaw computes the row hash from raw column values (as returned
// by database/sql Scan). This is used by VerifyChain for recomputation.
func computeRowHashFromRaw(prevHash []byte, colMap map[string]any, columns []ColumnMeta) []byte {
	var buf bytes.Buffer
	for _, col := range columns {
		if col.Name == "row_hash" || col.Name == "prev_hash" {
			continue
		}

		buf.WriteString(col.Name)
		buf.WriteByte(0x00)

		val, ok := colMap[col.Name]
		if !ok || val == nil {
			buf.WriteByte(typeTagNull)
			buf.WriteByte(0x00)
			continue
		}

		buf.WriteByte(col.TypeTag)
		serializeRawValue(&buf, val, col.TypeTag)
		buf.WriteByte(0x00)
	}

	h := sha256.New()
	h.Write(prevHash)
	h.Write(buf.Bytes())
	return h.Sum(nil)
}

// serializeRawValue writes a raw database value (from sql.Scan) into the canonical format.
func serializeRawValue(buf *bytes.Buffer, val any, typeTag byte) {
	switch typeTag {
	case typeTagUUID:
		switch v := val.(type) {
		case []byte:
			if len(v) == 16 {
				buf.Write(v)
			}
		case [16]byte:
			buf.Write(v[:])
		case string:
			// UUID as string — parse the 16 bytes
			// This handles Postgres returning UUID as string
			parsed := parseUUIDBytes(v)
			buf.Write(parsed)
		}

	case typeTagString:
		switch v := val.(type) {
		case string:
			buf.WriteString(v)
		case []byte:
			buf.Write(v)
		}

	case typeTagInteger, typeTagBigInt:
		var b [8]byte
		switch v := val.(type) {
		case int64:
			binary.BigEndian.PutUint64(b[:], uint64(v))
		case int32:
			binary.BigEndian.PutUint64(b[:], uint64(v))
		case int:
			binary.BigEndian.PutUint64(b[:], uint64(v))
		}
		buf.Write(b[:])

	case typeTagDecimal:
		// Normalize decimal representation to match shopspring/decimal's String() output
		s := fmt.Sprintf("%v", val)
		// Parse through the decimal library if available, otherwise trim trailing zeros
		// Remove trailing zeros after decimal point to match decimal.String() behavior
		if dotIdx := stringIndex(s, '.'); dotIdx >= 0 {
			s = trimTrailingZeros(s)
		}
		buf.WriteString(s)

	case typeTagBoolean:
		switch v := val.(type) {
		case bool:
			if v {
				buf.WriteByte(0x01)
			} else {
				buf.WriteByte(0x00)
			}
		}

	case typeTagTimestamp:
		switch v := val.(type) {
		case time.Time:
			var b [8]byte
			binary.BigEndian.PutUint64(b[:], uint64(v.UTC().UnixNano()))
			buf.Write(b[:])
		}

	case typeTagJSONB:
		switch v := val.(type) {
		case []byte:
			var compacted bytes.Buffer
			if err := json.Compact(&compacted, v); err == nil {
				buf.Write(compacted.Bytes())
			} else {
				buf.Write(v)
			}
		case string:
			var compacted bytes.Buffer
			if err := json.Compact(&compacted, []byte(v)); err == nil {
				buf.Write(compacted.Bytes())
			} else {
				buf.WriteString(v)
			}
		}

	case typeTagBinary:
		if v, ok := val.([]byte); ok {
			buf.Write(v)
		}

	case typeTagDate:
		switch v := val.(type) {
		case time.Time:
			buf.WriteString(v.UTC().Format("2006-01-02"))
		case string:
			buf.WriteString(v)
		}

	case typeTagTime:
		switch v := val.(type) {
		case time.Time:
			buf.WriteString(v.UTC().Format("15:04:05"))
		case string:
			buf.WriteString(v)
		}
	}
}

// parseUUIDBytes parses a UUID string (with or without hyphens) into 16 bytes.
func parseUUIDBytes(s string) []byte {
	// Remove hyphens
	clean := make([]byte, 0, 32)
	for _, c := range s {
		if c != '-' {
			clean = append(clean, byte(c))
		}
	}
	if len(clean) != 32 {
		return make([]byte, 16) // zero UUID on parse failure
	}
	out := make([]byte, 16)
	for i := 0; i < 16; i++ {
		out[i] = hexByte(clean[2*i], clean[2*i+1])
	}
	return out
}

func hexByte(hi, lo byte) byte {
	return (hexNibble(hi) << 4) | hexNibble(lo)
}

func stringIndex(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// trimTrailingZeros removes trailing zeros and unnecessary decimal point
// from a numeric string to match shopspring/decimal's String() output.
// "100.00000000" → "100", "1.50" → "1.5", "0.10" → "0.1"
func trimTrailingZeros(s string) string {
	if stringIndex(s, '.') < 0 {
		return s
	}
	// Trim trailing zeros
	i := len(s) - 1
	for i > 0 && s[i] == '0' {
		i--
	}
	// Trim trailing decimal point
	if s[i] == '.' {
		i--
	}
	return s[:i+1]
}

func hexNibble(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	default:
		return 0
	}
}
