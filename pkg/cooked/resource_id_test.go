package cooked

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http/httptest"
	"testing"
)

var (
	_ fmt.Stringer             = ResourceID{}
	_ encoding.TextMarshaler   = ResourceID{}
	_ encoding.TextUnmarshaler = (*ResourceID)(nil)
	_ json.Marshaler           = ResourceID{}
	_ json.Unmarshaler         = (*ResourceID)(nil)
)

func TestResourceIDRoundTripsPartsBytesAndText(t *testing.T) {
	tests := []ResourceIDParts{
		{ScopeID: 1, RecordID: 2},
		{ScopeID: math.MaxInt64, RecordID: math.MinInt64},
		{ScopeID: -1, RecordID: -2},
		{ScopeID: 0, RecordID: 42},
	}
	for _, want := range tests {
		t.Run(fmt.Sprintf("%d_%d", want.ScopeID, want.RecordID), func(t *testing.T) {
			id, err := NewResourceID(want.ScopeID, want.RecordID)
			if err != nil {
				t.Fatal(err)
			}
			if got := id.Parts(); got != want {
				t.Fatalf("parts = %+v, want %+v", got, want)
			}
			fromBytes, err := ResourceIDFromBytes(id.Bytes())
			if err != nil || fromBytes != id {
				t.Fatalf("bytes round trip = %v, %v", fromBytes, err)
			}
			parsed, err := ParseResourceID(id.String())
			if err != nil || parsed != id {
				t.Fatalf("text round trip = %v, %v", parsed, err)
			}
		})
	}
}

func TestResourceIDCanonicalSpelling(t *testing.T) {
	id, err := NewResourceID(0x0123456789abcdef, 0x1020304050607080)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := id.String(), "01234567-89ab-cdef-1020-304050607080"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestParseResourceIDRejectsInvalidForms(t *testing.T) {
	canonical := "01234567-89ab-cdef-1020-304050607080"
	tests := []struct {
		name  string
		value string
		want  error
	}{
		{"uppercase", "01234567-89AB-CDEF-1020-304050607080", ErrNonCanonicalResourceID},
		{"leading whitespace", " " + canonical, ErrMalformedResourceID},
		{"trailing whitespace", canonical + " ", ErrMalformedResourceID},
		{"braces", "{" + canonical + "}", ErrMalformedResourceID},
		{"urn", "urn:uuid:" + canonical, ErrMalformedResourceID},
		{"no hyphens", "0123456789abcdef1020304050607080", ErrMalformedResourceID},
		{"wrong hyphens", "0123456-789ab-cdef-1020-3040506070800", ErrMalformedResourceID},
		{"non hex", "01234567-89ab-cdeg-1020-304050607080", ErrMalformedResourceID},
		{"short", canonical[:35], ErrMalformedResourceID},
		{"long", canonical + "0", ErrMalformedResourceID},
		{"zero", "00000000-0000-0000-0000-000000000000", ErrInvalidResourceIDParts},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseResourceID(tt.value)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want category %v", err, tt.want)
			}
		})
	}
}

func TestResourceIDRejectsZeroConstructionAndMarshal(t *testing.T) {
	if _, err := NewResourceID(0, 0); !errors.Is(err, ErrInvalidResourceIDParts) {
		t.Fatalf("NewResourceID zero error = %v", err)
	}
	if _, err := ResourceIDFromBytes([16]byte{}); !errors.Is(err, ErrInvalidResourceIDParts) {
		t.Fatalf("ResourceIDFromBytes zero error = %v", err)
	}
	if _, err := json.Marshal(ResourceID{}); !errors.Is(err, ErrInvalidResourceIDParts) {
		t.Fatalf("zero marshal error = %v", err)
	}
}

func TestResourceIDJSONAndText(t *testing.T) {
	id, _ := NewResourceID(7, 11)
	data, err := json.Marshal(id)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), `"00000000-0000-0007-0000-00000000000b"`; got != want {
		t.Fatalf("JSON = %s, want %s", got, want)
	}
	var decoded ResourceID
	if err := json.Unmarshal(data, &decoded); err != nil || decoded != id {
		t.Fatalf("JSON round trip = %v, %v", decoded, err)
	}
	text, err := id.MarshalText()
	if err != nil || string(text) != id.String() {
		t.Fatalf("text marshal = %q, %v", text, err)
	}
	var textDecoded ResourceID
	if err := textDecoded.UnmarshalText(text); err != nil || textDecoded != id {
		t.Fatalf("text round trip = %v, %v", textDecoded, err)
	}
}

func TestResourceIDJSONRejectsNonStrings(t *testing.T) {
	for _, input := range []string{`123`, `{}`, `[]`, `null`, `true`} {
		t.Run(input, func(t *testing.T) {
			var id ResourceID
			if err := json.Unmarshal([]byte(input), &id); !errors.Is(err, ErrMalformedResourceID) {
				t.Fatalf("error = %v, want malformed resource ID", err)
			}
		})
	}
}

func TestOptionalResourceIDJSONNull(t *testing.T) {
	var payload struct {
		ID *ResourceID `json:"id"`
	}
	if err := json.Unmarshal([]byte(`{"id":null}`), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ID != nil {
		t.Fatalf("optional ResourceID = %v, want nil", payload.ID)
	}
}

func TestContextResourceIDParams(t *testing.T) {
	id, _ := NewResourceID(23, 99)
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	ctx.SetParam("party_id", id.String())

	parsed, err := ctx.ParamResourceID("party_id")
	if err != nil || parsed != id {
		t.Fatalf("ParamResourceID = %v, %v", parsed, err)
	}
	parts, err := ctx.ParamResourceIDParts("party_id")
	if err != nil || parts != (ResourceIDParts{ScopeID: 23, RecordID: 99}) {
		t.Fatalf("ParamResourceIDParts = %+v, %v", parts, err)
	}
}
