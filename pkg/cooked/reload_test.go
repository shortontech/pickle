package cooked

import "testing"

func TestScrubDSN(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "postgres DSN",
			input: `postgres://myuser:supersecret@localhost:5432/mydb?sslmode=disable`,
			want:  `postgres://myuser:***@localhost:5432/mydb?sslmode=disable`,
		},
		{
			name:  "mysql DSN",
			input: `mysql://root:hunter2@tcp(127.0.0.1:3306)/app`,
			want:  `mysql://root:***@tcp(127.0.0.1:3306)/app`,
		},
		{
			name:  "redis URL",
			input: `redis://default:redispass@redis.example.com:6379/0`,
			want:  `redis://default:***@redis.example.com:6379/0`,
		},
		{
			name:  "key=value password",
			input: `host=localhost password=secret123 dbname=app`,
			want:  `host=localhost password=*** dbname=app`,
		},
		{
			name:  "key=value passwd",
			input: `host=localhost passwd=secret123 dbname=app`,
			want:  `host=localhost passwd=*** dbname=app`,
		},
		{
			name:  "key=value PASSWORD uppercase",
			input: `HOST=localhost PASSWORD=secret123 DBNAME=app`,
			want:  `HOST=localhost PASSWORD=*** DBNAME=app`,
		},
		{
			name:  "key=value secret",
			input: `secret=abc123 other=value`,
			want:  `secret=*** other=value`,
		},
		{
			name:  "no credentials",
			input: `just a normal error message`,
			want:  `just a normal error message`,
		},
		{
			name:  "DSN embedded in error",
			input: `failed to connect to postgres://admin:p4ssw0rd@db.host:5432/prod: timeout`,
			want:  `failed to connect to postgres://admin:***@db.host:5432/prod: timeout`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scrubDSN(tt.input)
			if got != tt.want {
				t.Errorf("scrubDSN(%q)\n  got:  %s\n  want: %s", tt.input, got, tt.want)
			}
		})
	}
}
