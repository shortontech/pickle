package generator

import "testing"

func TestSnakeToPascal(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"id", "ID"},
		{"user_id", "UserID"},
		{"created_at", "CreatedAt"},
		{"brale_transfer_id", "BraleTransferID"},
		{"processor_order_id", "ProcessorOrderID"},
		{"name", "Name"},
		{"email", "Email"},
		{"status", "Status"},
		{"api_url", "APIURL"},
	}

	for _, tt := range tests {
		got := snakeToPascal(tt.in)
		if got != tt.want {
			t.Errorf("snakeToPascal(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTableToStructName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"users", "User"},
		{"posts", "Post"},
		{"transfers", "Transfer"},
		{"categories", "Categorie"}, // naive singular — good enough for now
	}

	for _, tt := range tests {
		got := tableToStructName(tt.in)
		if got != tt.want {
			t.Errorf("tableToStructName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
