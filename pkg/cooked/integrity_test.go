package cooked

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"reflect"
	"testing"
	"time"
)

// testIntegrityModel simulates a generated model with db tags.
type testIntegrityModel struct {
	ID       [16]byte `db:"id"`
	RowHash  []byte   `db:"row_hash"`
	PrevHash []byte   `db:"prev_hash"`
	Name     string   `db:"name"`
	Amount   int64    `db:"amount"`
	Active   bool     `db:"active"`
}

var testColumns = []ColumnMeta{
	{Name: "id", TypeTag: typeTagUUID},
	{Name: "row_hash", TypeTag: typeTagBinary},
	{Name: "prev_hash", TypeTag: typeTagBinary},
	{Name: "name", TypeTag: typeTagString},
	{Name: "amount", TypeTag: typeTagInteger},
	{Name: "active", TypeTag: typeTagBoolean},
}

func TestCanonicalSerializeExcludesHashColumns(t *testing.T) {
	model := &testIntegrityModel{
		ID:     [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Name:   "test",
		Amount: 42,
		Active: true,
	}
	data := canonicalSerialize(model, testColumns)
	if len(data) == 0 {
		t.Fatal("expected non-empty canonical serialization")
	}
	// Verify row_hash and prev_hash are NOT in the output
	if bytes.Contains(data, []byte("row_hash")) {
		t.Error("canonical serialization should exclude row_hash column")
	}
	if bytes.Contains(data, []byte("prev_hash")) {
		t.Error("canonical serialization should exclude prev_hash column")
	}
	// Verify id IS in the output
	if !bytes.Contains(data, []byte("id")) {
		t.Error("canonical serialization should include id column")
	}
}

func TestCanonicalSerializeDeterministic(t *testing.T) {
	model := &testIntegrityModel{
		ID:     [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Name:   "test",
		Amount: 100,
		Active: false,
	}
	data1 := canonicalSerialize(model, testColumns)
	data2 := canonicalSerialize(model, testColumns)
	if !bytes.Equal(data1, data2) {
		t.Error("canonical serialization is not deterministic")
	}
}

func TestCanonicalSerializeDifferentValuesDifferentHash(t *testing.T) {
	model1 := &testIntegrityModel{Name: "alice", Amount: 100}
	model2 := &testIntegrityModel{Name: "bob", Amount: 100}
	data1 := canonicalSerialize(model1, testColumns)
	data2 := canonicalSerialize(model2, testColumns)
	if bytes.Equal(data1, data2) {
		t.Error("different values should produce different serializations")
	}
}

func TestComputeRowHashUsesGenesis(t *testing.T) {
	model := &testIntegrityModel{
		ID:     [16]byte{1, 2, 3},
		Name:   "test",
		Amount: 42,
	}
	hash := computeRowHash(GenesisHash, model, testColumns)
	if len(hash) != 32 {
		t.Fatalf("expected 32 byte SHA-256 hash, got %d bytes", len(hash))
	}
	// Recompute manually
	canonical := canonicalSerialize(model, testColumns)
	h := sha256.New()
	h.Write(GenesisHash)
	h.Write(canonical)
	expected := h.Sum(nil)
	if !bytes.Equal(hash, expected) {
		t.Error("computeRowHash doesn't match manual SHA-256 computation")
	}
}

func TestComputeRowHashChaining(t *testing.T) {
	model1 := &testIntegrityModel{Name: "first", Amount: 1}
	model2 := &testIntegrityModel{Name: "second", Amount: 2}

	hash1 := computeRowHash(GenesisHash, model1, testColumns)
	hash2 := computeRowHash(hash1, model2, testColumns)

	// Hashes should be different
	if bytes.Equal(hash1, hash2) {
		t.Error("chained hashes should be different")
	}

	// hash2 should depend on hash1
	hash2Alt := computeRowHash(GenesisHash, model2, testColumns)
	if bytes.Equal(hash2, hash2Alt) {
		t.Error("chained hash should differ from unchained hash")
	}
}

func TestSerializeFieldUUID(t *testing.T) {
	var buf bytes.Buffer
	id := [16]byte{0xDE, 0xAD, 0xBE, 0xEF, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xFF}
	field := reflectField(id)
	serializeField(&buf, field, typeTagUUID)
	// type tag + 16 bytes + delimiter
	if buf.Len() != 1+16+1 {
		t.Errorf("UUID serialization: expected 18 bytes, got %d", buf.Len())
	}
	if buf.Bytes()[0] != typeTagUUID {
		t.Error("first byte should be UUID type tag")
	}
}

func TestSerializeFieldString(t *testing.T) {
	var buf bytes.Buffer
	field := reflectField("hello")
	serializeField(&buf, field, typeTagString)
	// type tag + "hello" + delimiter
	if buf.Len() != 1+5+1 {
		t.Errorf("string serialization: expected 7 bytes, got %d", buf.Len())
	}
}

func TestSerializeFieldInteger(t *testing.T) {
	var buf bytes.Buffer
	field := reflectField(int64(42))
	serializeField(&buf, field, typeTagInteger)
	// type tag + 8 bytes + delimiter
	if buf.Len() != 1+8+1 {
		t.Errorf("integer serialization: expected 10 bytes, got %d", buf.Len())
	}
	// Verify big-endian encoding
	var expected [8]byte
	binary.BigEndian.PutUint64(expected[:], 42)
	if !bytes.Equal(buf.Bytes()[1:9], expected[:]) {
		t.Error("integer not encoded as big-endian int64")
	}
}

func TestSerializeFieldBoolean(t *testing.T) {
	var bufT, bufF bytes.Buffer
	serializeField(&bufT, reflectField(true), typeTagBoolean)
	serializeField(&bufF, reflectField(false), typeTagBoolean)
	if bufT.Bytes()[1] != 0x01 {
		t.Error("true should serialize as 0x01")
	}
	if bufF.Bytes()[1] != 0x00 {
		t.Error("false should serialize as 0x00")
	}
}

func TestSerializeFieldTimestamp(t *testing.T) {
	var buf bytes.Buffer
	ts := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	serializeField(&buf, reflectField(ts), typeTagTimestamp)
	// type tag + 8 bytes + delimiter
	if buf.Len() != 1+8+1 {
		t.Errorf("timestamp serialization: expected 10 bytes, got %d", buf.Len())
	}
}

func TestChainErrorMessage(t *testing.T) {
	err := &ChainError{
		Table:    "transactions",
		RowID:    "abc-123",
		Expected: []byte{0x01, 0x02},
		Actual:   []byte{0x03, 0x04},
		Position: 5,
	}
	msg := err.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestGenesisHashIsZero(t *testing.T) {
	if len(GenesisHash) != 32 {
		t.Fatalf("genesis hash should be 32 bytes, got %d", len(GenesisHash))
	}
	for _, b := range GenesisHash {
		if b != 0 {
			t.Fatal("genesis hash should be all zeros")
		}
	}
}

func TestParseUUIDBytes(t *testing.T) {
	result := parseUUIDBytes("550e8400-e29b-41d4-a716-446655440000")
	if len(result) != 16 {
		t.Fatalf("expected 16 bytes, got %d", len(result))
	}
	if result[0] != 0x55 || result[1] != 0x0e {
		t.Errorf("unexpected first bytes: %x %x", result[0], result[1])
	}
}

// reflectField returns a reflect.Value for testing serializeField.
func reflectField(v any) reflect.Value {
	return reflect.ValueOf(v)
}
