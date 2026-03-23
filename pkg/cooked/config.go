package cooked

import (
	"bufio"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

var envOnce sync.Once
var envMap map[string]string

// Env returns the value of the environment variable named by key,
// or fallback if the variable is not set. On first call, loads
// .env file if it exists.
func Env(key, fallback string) string {
	envOnce.Do(loadEnv)
	if v, ok := envMap[key]; ok {
		return v
	}
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// loadEnv reads a .env file from the current directory if it exists.
// Lines are KEY=VALUE pairs. Comments (#) and blank lines are ignored.
// Quoted values (single or double) are unquoted. Existing environment
// variables take precedence over .env values.
func loadEnv() {
	envMap = make(map[string]string)

	f, err := os.Open(".env")
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])

		// Unquote if wrapped in matching quotes
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		// Environment variables take precedence over .env
		if os.Getenv(key) == "" {
			envMap[key] = value
		}
	}
}

// ConnectionConfig describes a single database connection.
type ConnectionConfig struct {
	Driver   string
	Host     string
	Port     string
	Name     string
	User     string
	Password string
	Region   string            // AWS region (DynamoDB), GCP region, etc.
	Options  map[string]string // Driver-specific options (sslmode, charset, etc.)
}

// DSN returns the driver-specific data source name.
func (c ConnectionConfig) DSN() string {
	switch c.Driver {
	case "pgsql":
		params := url.Values{}
		params.Set("sslmode", "disable")
		for k, v := range c.Options {
			params.Set(k, v)
		}
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?%s",
			url.PathEscape(c.User), url.PathEscape(c.Password), c.Host, c.Port, c.Name, params.Encode())
	case "mysql":
		params := url.Values{}
		params.Set("parseTime", "true")
		for k, v := range c.Options {
			params.Set(k, v)
		}
		return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?%s",
			url.PathEscape(c.User), url.PathEscape(c.Password), c.Host, c.Port, c.Name, params.Encode())
	case "sqlite":
		return c.Name
	default:
		panic("unsupported database driver: " + c.Driver)
	}
}

// driverName maps Laravel driver names to Go sql driver names.
func (c ConnectionConfig) driverName() string {
	switch c.Driver {
	case "pgsql":
		return "pgx"
	case "mysql":
		return "mysql"
	case "sqlite":
		return "sqlite3"
	default:
		panic("unsupported database driver: " + c.Driver)
	}
}

// OpenDB opens a database connection using the given ConnectionConfig,
// pings it, and returns *sql.DB. Fatals on failure — call at startup.
func OpenDB(conn ConnectionConfig) *sql.DB {
	db, err := sql.Open(conn.driverName(), conn.DSN())
	if err != nil {
		log.Fatalf("pickle: failed to open database: %v", err)
	}
	if err := db.Ping(); err != nil {
		log.Fatalf("pickle: failed to ping database: %v", err)
	}
	return db
}

// RuntimeConfig holds configuration values that can be hot-reloaded without
// restarting the process. Access via Config() — never cache the pointer.
type RuntimeConfig struct {
	EncryptionKey     []byte
	EncryptionKeyNext []byte
	DatabaseDSNs      map[string]string // connection name → DSN
}

var runtimeConfig atomic.Pointer[RuntimeConfig]

// Config returns the current runtime config. Lock-free, safe for concurrent reads.
func Config() *RuntimeConfig {
	cfg := runtimeConfig.Load()
	if cfg == nil {
		return &RuntimeConfig{}
	}
	return cfg
}

// InitRuntimeConfig reads environment variables and initializes the runtime config.
// Call once at startup.
func InitRuntimeConfig() {
	cfg := buildRuntimeConfig()
	runtimeConfig.Store(cfg)
}

// ConnectionSwapFunc is called during config reload when a database DSN changes.
// It receives the connection name, driver, and new DSN. Set by the models package
// to wire up ManagedConnection swapping.
var ConnectionSwapFunc func(name, driver, dsn string) error

// ConnectionNamesFunc returns the names of all managed connections.
// Set by the models package during initialization.
var ConnectionNamesFunc func() []string

// ReloadConfig re-reads environment variables, validates them, and atomically
// swaps the in-memory RuntimeConfig. Returns the new config, a list of changed
// env var names, and any validation error.
func ReloadConfig() (*RuntimeConfig, []string, error) {
	newCfg, err := buildAndValidateRuntimeConfig()
	if err != nil {
		return nil, nil, err
	}

	oldCfg := Config()
	var changes []string

	// Detect encryption key changes
	if !bytesEqual(oldCfg.EncryptionKey, newCfg.EncryptionKey) {
		changes = append(changes, "PICKLE_ENCRYPTION_KEY")
	}
	if !bytesEqual(oldCfg.EncryptionKeyNext, newCfg.EncryptionKeyNext) {
		changes = append(changes, "PICKLE_ENCRYPTION_KEY_NEXT")
	}

	// Detect DSN changes and swap connections
	for name, newDSN := range newCfg.DatabaseDSNs {
		oldDSN := ""
		if oldCfg.DatabaseDSNs != nil {
			oldDSN = oldCfg.DatabaseDSNs[name]
		}
		if newDSN != oldDSN {
			changes = append(changes, "DATABASE_DSN_"+strings.ToUpper(name))
			if ConnectionSwapFunc != nil {
				driver := "pgx"
				if err := ConnectionSwapFunc(name, driver, newDSN); err != nil {
					return nil, nil, fmt.Errorf("connection %q: %w", name, err)
				}
			}
		}
	}

	// Atomic swap
	runtimeConfig.Store(newCfg)
	return newCfg, changes, nil
}

// buildRuntimeConfig reads env vars and builds a RuntimeConfig without validation.
func buildRuntimeConfig() *RuntimeConfig {
	cfg := &RuntimeConfig{
		DatabaseDSNs: make(map[string]string),
	}

	if key := os.Getenv("PICKLE_ENCRYPTION_KEY"); key != "" {
		if decoded, err := base64.StdEncoding.DecodeString(key); err == nil {
			cfg.EncryptionKey = decoded
		}
	}
	if key := os.Getenv("PICKLE_ENCRYPTION_KEY_NEXT"); key != "" {
		if decoded, err := base64.StdEncoding.DecodeString(key); err == nil {
			cfg.EncryptionKeyNext = decoded
		}
	}

	return cfg
}

// buildAndValidateRuntimeConfig reads env vars and validates them.
func buildAndValidateRuntimeConfig() (*RuntimeConfig, error) {
	cfg := &RuntimeConfig{
		DatabaseDSNs: make(map[string]string),
	}

	if key := os.Getenv("PICKLE_ENCRYPTION_KEY"); key != "" {
		decoded, err := base64.StdEncoding.DecodeString(key)
		if err != nil {
			return nil, fmt.Errorf("PICKLE_ENCRYPTION_KEY: invalid base64")
		}
		cfg.EncryptionKey = decoded
	}
	if key := os.Getenv("PICKLE_ENCRYPTION_KEY_NEXT"); key != "" {
		decoded, err := base64.StdEncoding.DecodeString(key)
		if err != nil {
			return nil, fmt.Errorf("PICKLE_ENCRYPTION_KEY_NEXT: invalid base64")
		}
		cfg.EncryptionKeyNext = decoded
	}

	// Copy DSNs from managed connections for change detection
	if ConnectionNamesFunc != nil {
		for _, name := range ConnectionNamesFunc() {
			envKey := "DATABASE_DSN_" + strings.ToUpper(name)
			if dsn := os.Getenv(envKey); dsn != "" {
				cfg.DatabaseDSNs[name] = dsn
			}
		}
	}

	return cfg, nil
}

// bytesEqual compares two byte slices for equality.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
