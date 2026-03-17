package names

import (
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
)

func TestSnakeToPascal(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"user", "User"},
		{"create_user", "CreateUser"},
		{"user_controller", "UserController"},
		{"created_at", "CreatedAt"},
		// Common initialisms
		{"user_id", "UserID"},
		{"customer_uuid", "CustomerUUID"},
		{"api_key", "APIKey"},
		{"http_client", "HTTPClient"},
		{"https_url", "HTTPSURL"},
		{"ip_address", "IPAddress"},
		{"sql_query", "SQLQuery"},
		{"ssh_key", "SSHKey"},
		{"json_body", "JSONBody"},
		{"xml_parser", "XMLParser"},
		{"html_template", "HTMLTemplate"},
		{"css_class", "CSSClass"},
		{"cpu_count", "CPUCount"},
		{"ram_usage", "RAMUsage"},
		{"os_name", "OSName"},
		{"io_reader", "IOReader"},
		{"eof_marker", "EOFMarker"},
		{"acl_rule", "ACLRule"},
		{"tls_cert", "TLSCert"},
		{"tcp_conn", "TCPConn"},
		{"udp_packet", "UDPPacket"},
		{"dns_record", "DNSRecord"},
		{"uri_path", "URIPath"},
		// Initialism alone
		{"id", "ID"},
		{"uuid", "UUID"},
		{"url", "URL"},
		// Mixed: initialism in middle
		{"get_url_param", "GetURLParam"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := SnakeToPascal(tt.input)
			if got != tt.expected {
				t.Errorf("SnakeToPascal(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestPascalToSnake(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"User", "user"},
		{"CreateUser", "create_user"},
		{"UserController", "user_controller"},
		{"HTTPServer", "http_server"},
		{"JSONBody", "json_body"},
		{"UserID", "user_id"},
		{"APIKey", "api_key"},
		{"CreatedAt", "created_at"},
		{"A", "a"},
		// Consecutive caps followed by lower: e.g. "HTMLParser" → "html_parser"
		{"HTMLParser", "html_parser"},
		{"URLPath", "url_path"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := PascalToSnake(tt.input)
			if got != tt.expected {
				t.Errorf("PascalToSnake(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestTableToStructName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"users", "User"},
		{"posts", "Post"},
		{"transfers", "Transfer"},
		{"api_keys", "APIKey"},
		{"user_ids", "UserID"},
		// No trailing 's'
		{"datum", "Datum"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := TableToStructName(tt.input)
			if got != tt.expected {
				t.Errorf("TableToStructName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestPluralize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Regular
		{"user", "users"},
		{"post", "posts"},
		{"transfer", "transfers"},
		// Suffix -s, -x, -z, -ch, -sh → +es
		{"class", "classes"},
		{"box", "boxes"},
		{"buzz", "buzzes"},
		{"match", "matches"},
		{"dish", "dishes"},
		// -y with consonant before → -ies
		{"category", "categories"},
		{"entry", "entries"},
		// -y with vowel before → +s
		{"day", "days"},
		{"key", "keys"},
		// Irregular plurals
		{"person", "people"},
		{"child", "children"},
		{"man", "men"},
		{"woman", "women"},
		{"mouse", "mice"},
		{"goose", "geese"},
		{"tooth", "teeth"},
		{"foot", "feet"},
		{"datum", "data"},
		{"medium", "media"},
		{"analysis", "analyses"},
		{"basis", "bases"},
		{"crisis", "crises"},
		// Case-insensitive lookup for irregulars
		{"Person", "people"},
		{"DATUM", "data"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := Pluralize(tt.input)
			if got != tt.expected {
				t.Errorf("Pluralize(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestColumnGoType(t *testing.T) {
	tests := []struct {
		name     string
		col      *schema.Column
		expected string
	}{
		{"uuid not null", &schema.Column{Type: schema.UUID}, "uuid.UUID"},
		{"uuid nullable", &schema.Column{Type: schema.UUID, IsNullable: true}, "*uuid.UUID"},
		{"string not null", &schema.Column{Type: schema.String}, "string"},
		{"string nullable", &schema.Column{Type: schema.String, IsNullable: true}, "*string"},
		{"text not null", &schema.Column{Type: schema.Text}, "string"},
		{"text nullable", &schema.Column{Type: schema.Text, IsNullable: true}, "*string"},
		{"integer not null", &schema.Column{Type: schema.Integer}, "int"},
		{"integer nullable", &schema.Column{Type: schema.Integer, IsNullable: true}, "*int"},
		{"biginteger not null", &schema.Column{Type: schema.BigInteger}, "int64"},
		{"biginteger nullable", &schema.Column{Type: schema.BigInteger, IsNullable: true}, "*int64"},
		{"decimal not null", &schema.Column{Type: schema.Decimal}, "decimal.Decimal"},
		{"decimal nullable", &schema.Column{Type: schema.Decimal, IsNullable: true}, "*decimal.Decimal"},
		{"boolean not null", &schema.Column{Type: schema.Boolean}, "bool"},
		{"boolean nullable", &schema.Column{Type: schema.Boolean, IsNullable: true}, "*bool"},
		{"timestamp not null", &schema.Column{Type: schema.Timestamp}, "time.Time"},
		{"timestamp nullable", &schema.Column{Type: schema.Timestamp, IsNullable: true}, "*time.Time"},
		{"date not null", &schema.Column{Type: schema.Date}, "time.Time"},
		{"date nullable", &schema.Column{Type: schema.Date, IsNullable: true}, "*time.Time"},
		{"jsonb not null", &schema.Column{Type: schema.JSONB}, "json.RawMessage"},
		{"jsonb nullable", &schema.Column{Type: schema.JSONB, IsNullable: true}, "*json.RawMessage"},
		{"binary not null", &schema.Column{Type: schema.Binary}, "[]byte"},
		// Binary nullable stays []byte (special case — no pointer)
		{"binary nullable", &schema.Column{Type: schema.Binary, IsNullable: true}, "[]byte"},
		{"time not null", &schema.Column{Type: schema.Time}, "string"},
		{"time nullable", &schema.Column{Type: schema.Time, IsNullable: true}, "*string"},
		{"unknown", &schema.Column{Type: schema.ColumnType(9999)}, "any"},
		{"unknown nullable", &schema.Column{Type: schema.ColumnType(9999), IsNullable: true}, "*any"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ColumnGoType(tt.col)
			if got != tt.expected {
				t.Errorf("ColumnGoType(%+v) = %q, want %q", tt.col, got, tt.expected)
			}
		})
	}
}

func TestColumnBaseGoType(t *testing.T) {
	tests := []struct {
		colType  schema.ColumnType
		expected string
	}{
		{schema.UUID, "uuid.UUID"},
		{schema.String, "string"},
		{schema.Text, "string"},
		{schema.Time, "string"},
		{schema.Integer, "int"},
		{schema.BigInteger, "int64"},
		{schema.Decimal, "decimal.Decimal"},
		{schema.Boolean, "bool"},
		{schema.Timestamp, "time.Time"},
		{schema.Date, "time.Time"},
		{schema.JSONB, "json.RawMessage"},
		{schema.Binary, "[]byte"},
		{schema.ColumnType(9999), "any"},
	}

	for _, tt := range tests {
		t.Run(tt.colType.String(), func(t *testing.T) {
			col := &schema.Column{Type: tt.colType}
			got := ColumnBaseGoType(col)
			if got != tt.expected {
				t.Errorf("ColumnBaseGoType(%q) = %q, want %q", tt.colType, got, tt.expected)
			}
		})
	}
}

func TestColumnImport(t *testing.T) {
	tests := []struct {
		colType  schema.ColumnType
		expected string
	}{
		{schema.UUID, "github.com/google/uuid"},
		{schema.Decimal, "github.com/shopspring/decimal"},
		{schema.Timestamp, "time"},
		{schema.Date, "time"},
		{schema.JSONB, "encoding/json"},
		// Types with no import
		{schema.String, ""},
		{schema.Text, ""},
		{schema.Integer, ""},
		{schema.BigInteger, ""},
		{schema.Boolean, ""},
		{schema.Binary, ""},
		{schema.Time, ""},
		{schema.ColumnType(9999), ""},
	}

	for _, tt := range tests {
		t.Run(tt.colType.String(), func(t *testing.T) {
			col := &schema.Column{Type: tt.colType}
			got := ColumnImport(col)
			if got != tt.expected {
				t.Errorf("ColumnImport(%q) = %q, want %q", tt.colType, got, tt.expected)
			}
		})
	}
}

func TestIsVowel(t *testing.T) {
	vowels := []byte{'a', 'e', 'i', 'o', 'u'}
	for _, v := range vowels {
		if !isVowel(v) {
			t.Errorf("isVowel(%q) = false, want true", v)
		}
	}
	consonants := []byte{'b', 'c', 'd', 'f', 'g', 'h', 'j', 'k', 'l', 'm', 'n', 'p', 'q', 'r', 's', 't', 'v', 'w', 'x', 'y', 'z'}
	for _, c := range consonants {
		if isVowel(c) {
			t.Errorf("isVowel(%q) = true, want false", c)
		}
	}
}
