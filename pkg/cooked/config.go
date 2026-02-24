package cooked

import (
	"bufio"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
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
}

// DSN returns the driver-specific data source name.
func (c ConnectionConfig) DSN() string {
	switch c.Driver {
	case "pgsql":
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
			c.User, c.Password, c.Host, c.Port, c.Name)
	case "mysql":
		return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
			c.User, c.Password, c.Host, c.Port, c.Name)
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
