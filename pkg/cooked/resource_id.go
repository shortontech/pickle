package cooked

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

var (
	// ErrMalformedResourceID identifies a value with the wrong length,
	// separators, or hexadecimal content.
	ErrMalformedResourceID = errors.New("malformed resource ID")
	// ErrNonCanonicalResourceID identifies an otherwise decodable spelling that
	// is not Pickle's canonical lowercase representation.
	ErrNonCanonicalResourceID = errors.New("noncanonical resource ID")
	// ErrInvalidResourceIDParts identifies the forbidden all-zero value.
	ErrInvalidResourceIDParts = errors.New("invalid resource ID parts")
)

// ResourceID is a non-UUID application-boundary projection of two int64
// values. It deliberately exposes no RFC UUID semantics.
type ResourceID struct {
	bytes [16]byte
}

// ResourceIDParts are the two authoritative integer values projected into a
// ResourceID.
type ResourceIDParts struct {
	ScopeID  int64
	RecordID int64
}

// NewResourceID packs scopeID and recordID in network byte order while
// preserving their signed two's-complement bit patterns.
func NewResourceID(scopeID, recordID int64) (ResourceID, error) {
	if scopeID == 0 && recordID == 0 {
		return ResourceID{}, ErrInvalidResourceIDParts
	}
	var value [16]byte
	binary.BigEndian.PutUint64(value[:8], uint64(scopeID))
	binary.BigEndian.PutUint64(value[8:], uint64(recordID))
	return ResourceID{bytes: value}, nil
}

// ResourceIDFromBytes constructs a ResourceID from its exact 128-bit
// representation.
func ResourceIDFromBytes(value [16]byte) (ResourceID, error) {
	id := ResourceID{bytes: value}
	if id.IsZero() {
		return ResourceID{}, ErrInvalidResourceIDParts
	}
	return id, nil
}

// ParseResourceID parses Pickle's exact lowercase, UUID-shaped wire form.
func ParseResourceID(value string) (ResourceID, error) {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return ResourceID{}, ErrMalformedResourceID
	}

	var compact [32]byte
	j := 0
	for i := 0; i < len(value); i++ {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		c := value[i]
		if c >= 'A' && c <= 'F' {
			return ResourceID{}, ErrNonCanonicalResourceID
		}
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return ResourceID{}, ErrMalformedResourceID
		}
		compact[j] = c
		j++
	}

	var decoded [16]byte
	if _, err := hex.Decode(decoded[:], compact[:]); err != nil {
		return ResourceID{}, fmt.Errorf("%w: %v", ErrMalformedResourceID, err)
	}
	return ResourceIDFromBytes(decoded)
}

// Bytes returns the exact packed representation.
func (id ResourceID) Bytes() [16]byte {
	return id.bytes
}

// Parts returns the two signed integer components.
func (id ResourceID) Parts() ResourceIDParts {
	return ResourceIDParts{
		ScopeID:  int64(binary.BigEndian.Uint64(id.bytes[:8])),
		RecordID: int64(binary.BigEndian.Uint64(id.bytes[8:])),
	}
}

// String returns the fixed-width lowercase spelling. A zero Go value is
// formatted deterministically, but parsing and marshaling reject it.
func (id ResourceID) String() string {
	var compact [32]byte
	hex.Encode(compact[:], id.bytes[:])
	var canonical [36]byte
	copy(canonical[0:8], compact[0:8])
	canonical[8] = '-'
	copy(canonical[9:13], compact[8:12])
	canonical[13] = '-'
	copy(canonical[14:18], compact[12:16])
	canonical[18] = '-'
	copy(canonical[19:23], compact[16:20])
	canonical[23] = '-'
	copy(canonical[24:36], compact[20:32])
	return string(canonical[:])
}

// IsZero reports whether every bit is zero.
func (id ResourceID) IsZero() bool {
	return id.bytes == [16]byte{}
}

// MarshalText implements encoding.TextMarshaler.
func (id ResourceID) MarshalText() ([]byte, error) {
	if id.IsZero() {
		return nil, ErrInvalidResourceIDParts
	}
	return []byte(id.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (id *ResourceID) UnmarshalText(text []byte) error {
	if id == nil {
		return errors.New("cannot unmarshal ResourceID into nil receiver")
	}
	parsed, err := ParseResourceID(string(text))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

// MarshalJSON implements json.Marshaler.
func (id ResourceID) MarshalJSON() ([]byte, error) {
	text, err := id.MarshalText()
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(text))
}

// UnmarshalJSON implements json.Unmarshaler and accepts strings only.
func (id *ResourceID) UnmarshalJSON(data []byte) error {
	if id == nil {
		return errors.New("cannot unmarshal ResourceID into nil receiver")
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("%w: JSON value must be a string", ErrMalformedResourceID)
	}
	return id.UnmarshalText([]byte(value))
}
