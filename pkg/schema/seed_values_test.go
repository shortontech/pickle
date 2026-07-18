package schema

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRelativeSeedTimeUsesExplicitAnchor(t *testing.T) {
	anchor := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	ctx := SeedValueContext{RootSeed: 7, Scenario: "Demo", NodePath: "events/0", Column: "occurred_at", AnchorTime: anchor}
	past, err := SeedValue(&SeedSpec{Kind: "past_time", Arguments: []string{"24h"}}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	future, err := SeedValue(&SeedSpec{Kind: "future_time", Arguments: []string{"24h"}}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !past.(time.Time).Before(anchor) || !future.(time.Time).After(anchor) {
		t.Fatalf("relative values past=%s future=%s anchor=%s", past, future, anchor)
	}
	ctx.AnchorTime = time.Time{}
	value, err := SeedValue(&SeedSpec{Kind: "future_time", Arguments: []string{"24h"}}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !value.(time.Time).After(DefaultSeedAnchor) {
		t.Fatalf("omitted anchor did not preserve fixed default: %s", value)
	}
}

func TestSeedAnchorDoesNotChangeRandomIdentity(t *testing.T) {
	base := SeedValueContext{RootSeed: 8675309, Scenario: "Demo", NodePath: "users/0", Column: "id"}
	one, err := SeedValue(&SeedSpec{Kind: "uuid"}, base)
	if err != nil {
		t.Fatal(err)
	}
	base.AnchorTime = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	two, err := SeedValue(&SeedSpec{Kind: "uuid"}, base)
	if err != nil {
		t.Fatal(err)
	}
	if one != two {
		t.Fatalf("anchor reshuffled stable identity: %v != %v", one, two)
	}
}

func TestSeedValueStableSubstreams(t *testing.T) {
	ctx := SeedValueContext{RootSeed: 8675309, Scenario: "CRMSeeder", NodePath: "users", RowOrdinal: 0, Column: "first_name"}
	spec := &SeedSpec{Kind: "first_name"}
	first, err := SeedValue(spec, ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SeedValue(spec, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("same seed produced %v then %v", first, second)
	}

	other := ctx
	other.Column = "unrelated"
	if _, err := SeedValue(spec, other); err != nil {
		t.Fatal(err)
	}
	again, _ := SeedValue(spec, ctx)
	if again != first {
		t.Fatalf("unrelated stream reshuffled value: %v != %v", again, first)
	}
}

func TestGenerateSeedRowPasswordComposite(t *testing.T) {
	table := &Table{Name: "users", Columns: []*Column{
		{Name: "id", Type: BigInteger},
		{Name: "first_name", Type: String},
		{Name: "last_name", Type: String},
		{Name: "password_hash", Type: String, Seeder: &SeedSpec{Kind: "password", Fields: []string{"first_name", "last_name", "id"}}},
	}}
	row, err := GenerateSeedRow(table, map[string]any{"id": int64(1), "first_name": "Ada", "last_name": "Lovelace"}, SeedValueContext{RootSeed: 1, Scenario: "CRMSeeder"})
	if err != nil {
		t.Fatal(err)
	}
	if got := row["password_hash"]; got != "ada-lovelace-1" {
		t.Fatalf("password composite = %q", got)
	}
}

func TestGenerateSeedRowRequiresCompositeFields(t *testing.T) {
	table := &Table{Name: "users", Columns: []*Column{{Name: "password_hash", Type: String, Seeder: &SeedSpec{Kind: "password", Fields: []string{"first_name"}}}}}
	if _, err := GenerateSeedRow(table, nil, SeedValueContext{}); err == nil {
		t.Fatal("expected missing composite field error")
	}
}

func TestSeedValueSupportsEveryBuiltInProvider(t *testing.T) {
	tests := []SeedSpec{
		{Kind: "value", Arguments: []string{"fixed"}}, {Kind: "values", Arguments: []string{"a"}},
		{Kind: "random_string", Arguments: []string{"8"}}, {Kind: "random_string_in", Arguments: []string{"a"}},
		{Kind: "integer", Arguments: []string{"1", "9"}}, {Kind: "big_integer", Arguments: []string{"1", "9"}},
		{Kind: "decimal", Arguments: []string{"1.25", "9.75", "2"}}, {Kind: "money", Arguments: []string{"1", "9"}},
		{Kind: "boolean"}, {Kind: "boolean_weighted", Arguments: []string{"0.5"}}, {Kind: "uuid"}, {Kind: "bytes", Arguments: []string{"8"}},
		{Kind: "first_name"}, {Kind: "last_name"}, {Kind: "full_name"}, {Kind: "username"}, {Kind: "job_title"}, {Kind: "department"},
		{Kind: "company_name"}, {Kind: "company_suffix"}, {Kind: "industry"}, {Kind: "email"}, {Kind: "safe_email"}, {Kind: "domain_name"},
		{Kind: "url"}, {Kind: "ipv4"}, {Kind: "ipv6"}, {Kind: "user_agent"}, {Kind: "phone_number"}, {Kind: "street_address"},
		{Kind: "city"}, {Kind: "state"}, {Kind: "postal_code"}, {Kind: "country"}, {Kind: "country_code"}, {Kind: "locale"}, {Kind: "time_zone"},
		{Kind: "date_between", Arguments: []string{"2024-01-01", "2024-12-31"}}, {Kind: "time_between", Arguments: []string{"09:00:00", "17:00:00"}},
		{Kind: "past_time", Arguments: []string{"720h"}}, {Kind: "future_time", Arguments: []string{"720h"}},
		{Kind: "sentence", Arguments: []string{"8"}}, {Kind: "paragraph", Arguments: []string{"3"}}, {Kind: "words", Arguments: []string{"5"}},
		{Kind: "product_name"}, {Kind: "currency_code"},
	}
	ctx := SeedValueContext{RootSeed: 7, Scenario: "Coverage", NodePath: "node", RowOrdinal: 1, Column: "field"}
	for _, test := range tests {
		t.Run(test.Kind, func(t *testing.T) {
			first, err := SeedValue(&test, ctx)
			if err != nil {
				t.Fatal(err)
			}
			second, err := SeedValue(&test, ctx)
			if err != nil {
				t.Fatal(err)
			}
			if valueKey(first) != valueKey(second) {
				t.Fatalf("provider is not deterministic: %#v != %#v", first, second)
			}
		})
	}
}

func TestGenerateSeedRowCastsFixedValues(t *testing.T) {
	table := &Table{Name: "examples", Columns: []*Column{
		{Name: "count", Type: Integer, Seeder: &SeedSpec{Kind: "value", Arguments: []string{"42"}}},
		{Name: "enabled", Type: Boolean, Seeder: &SeedSpec{Kind: "value", Arguments: []string{"true"}}},
		{Name: "at", Type: Timestamp, Seeder: &SeedSpec{Kind: "value", Arguments: []string{"2024-01-02T03:04:05Z"}}},
	}}
	row, err := GenerateSeedRow(table, nil, SeedValueContext{})
	if err != nil {
		t.Fatal(err)
	}
	if row["count"] != 42 || row["enabled"] != true {
		t.Fatalf("casts = %#v", row)
	}
	if _, ok := row["at"].(time.Time); !ok {
		t.Fatalf("timestamp = %#v", row["at"])
	}
}

func TestGenerateSeedRowCastsCustomJSONAndScalars(t *testing.T) {
	table := &Table{Name: "examples", Columns: []*Column{
		{Name: "payload", Type: JSONB, Seeder: &SeedSpec{Kind: "json", Reference: "PayloadSeeder"}},
		{Name: "count", Type: BigInteger, Seeder: &SeedSpec{Kind: "custom", Reference: "CountSeeder"}},
		{Name: "ratio", Type: Double, Seeder: &SeedSpec{Kind: "custom", Reference: "RatioSeeder"}},
		{Name: "data", Type: Binary, Seeder: &SeedSpec{Kind: "custom", Reference: "DataSeeder"}},
	}}
	resolver := func(name string, _ SeedValueContext) (any, bool, error) {
		values := map[string]any{"PayloadSeeder": map[string]any{"role": "admin"}, "CountSeeder": int(42), "RatioSeeder": "1.25", "DataSeeder": "pickle"}
		value, found := values[name]
		return value, found, nil
	}
	row, err := GenerateSeedRowWith(table, nil, SeedValueContext{}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if string(row["payload"].([]byte)) != `{"role":"admin"}` {
		t.Fatalf("json = %s", row["payload"])
	}
	if row["count"] != int64(42) || row["ratio"] != 1.25 || string(row["data"].([]byte)) != "pickle" {
		t.Fatalf("casts = %#v", row)
	}
}

func TestGenerateSeedRowRejectsInvalidCustomJSON(t *testing.T) {
	table := &Table{Name: "examples", Columns: []*Column{{Name: "payload", Type: JSONB, Seeder: &SeedSpec{Kind: "json", Reference: "PayloadSeeder"}}}}
	resolver := func(string, SeedValueContext) (any, bool, error) { return "not-json", true, nil }
	if _, err := GenerateSeedRowWith(table, nil, SeedValueContext{}, resolver); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

func TestGenerateSeedRowValidatesAndCastsOverrides(t *testing.T) {
	table := &Table{Name: "examples", Columns: []*Column{{Name: "count", Type: BigInteger}}}
	row, err := GenerateSeedRow(table, map[string]any{"count": "42"}, SeedValueContext{})
	if err != nil {
		t.Fatal(err)
	}
	if row["count"] != int64(42) {
		t.Fatalf("override = %#v", row["count"])
	}
	if _, err := GenerateSeedRow(table, nil, SeedValueContext{}); err == nil {
		t.Fatal("expected missing required value error")
	}
	if _, err := GenerateSeedRow(table, map[string]any{"count": 1, "typo": true}, SeedValueContext{}); err == nil {
		t.Fatal("expected unknown override error")
	}
}

func TestGenerateSeedRowEnforcesLengthAndDecimalShape(t *testing.T) {
	table := &Table{Name: "examples", Columns: []*Column{{Name: "code", Type: String, Length: 3}, {Name: "amount", Type: Decimal, Precision: 5, Scale: 2}}}
	if _, err := GenerateSeedRow(table, map[string]any{"code": "four", "amount": "1.00"}, SeedValueContext{}); err == nil || !strings.Contains(err.Error(), "maximum length") {
		t.Fatalf("length error = %v", err)
	}
	if _, err := GenerateSeedRow(table, map[string]any{"code": "ok", "amount": "1.234"}, SeedValueContext{}); err == nil || !strings.Contains(err.Error(), "scale") {
		t.Fatalf("scale error = %v", err)
	}
	if _, err := GenerateSeedRow(table, map[string]any{"code": "ok", "amount": "1234.56"}, SeedValueContext{}); err == nil || !strings.Contains(err.Error(), "precision") {
		t.Fatalf("precision error = %v", err)
	}
}

func TestGenerateSeedRowCreatesDeterministicFrameworkIdentities(t *testing.T) {
	table := &Table{Name: "records", CompositePrimaryKeys: []string{"tenant_id", "id"}, Columns: []*Column{
		{Name: "tenant_id", Type: BigInteger, HasDefault: true},
		{Name: "id", Type: BigInteger, HasDefault: true},
		{Name: "public_id", Type: UUID, IsPrimaryKey: true, HasDefault: true},
	}}
	ctx := SeedValueContext{RootSeed: 8675309, Scenario: "CRM", NodePath: "RecordSeeder/0"}
	first, err := GenerateSeedRow(table, nil, ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateSeedRow(table, nil, ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"tenant_id", "id", "public_id"} {
		if first[column] == nil || first[column] != second[column] {
			t.Fatalf("identity %s: %#v != %#v", column, first[column], second[column])
		}
	}
}

func valueKey(value any) string { return fmt.Sprintf("%#v", value) }
