package squeeze

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the top-level pickle.yaml file.
type Config struct {
	Squeeze  SqueezeConfig            `yaml:"squeeze"`
	Apps     map[string]AppConfig     `yaml:"apps,omitempty"`
	Services map[string]ServiceConfig `yaml:"services,omitempty"`
}

// AppConfig describes a single app in a monorepo layout.
type AppConfig struct {
	Path       string   `yaml:"path"`                 // directory containing go.mod, relative to pickle.yaml
	Migrations []string `yaml:"migrations,omitempty"` // migration dirs relative to app path; default: ["database/migrations"]
	Config     string   `yaml:"config,omitempty"`     // config dir relative to app path; default: "config"
}

// IsMonorepo returns true if the config defines multiple apps (separate go.mod files).
func (c *Config) IsMonorepo() bool {
	return len(c.Apps) > 0
}

// ServiceConfig describes a service in a multi-service project (one go.mod, shared models).
type ServiceConfig struct {
	Dir string `yaml:"dir"` // relative to project root, e.g. "services/api"
}

// IsMultiService returns true if the config defines multiple services.
func (c *Config) IsMultiService() bool {
	return len(c.Services) > 0
}

// SqueezeConfig holds all squeeze-related configuration.
type SqueezeConfig struct {
	Middleware MiddlewareConfig `yaml:"middleware"`
	Rules      map[string]bool  `yaml:"rules"`
}

// MiddlewareConfig classifies middleware by role.
type MiddlewareConfig struct {
	Auth      []string `yaml:"auth"`       // middleware that provides authentication
	Admin     []string `yaml:"admin"`      // middleware that provides admin access (implies auth)
	RateLimit []string `yaml:"rate_limit"` // middleware that provides rate limiting
	CSRF      []string `yaml:"csrf"`       // middleware that provides CSRF protection
}

// IsAuthMiddleware returns true if the given middleware name is classified as auth.
func (mc MiddlewareConfig) IsAuthMiddleware(name string) bool {
	for _, m := range mc.Auth {
		if m == name {
			return true
		}
	}
	// Admin middleware implies auth
	for _, m := range mc.Admin {
		if m == name {
			return true
		}
	}
	return false
}

// IsAdminMiddleware returns true if the given middleware name is classified as admin.
func (mc MiddlewareConfig) IsAdminMiddleware(name string) bool {
	for _, m := range mc.Admin {
		if m == name {
			return true
		}
	}
	return false
}

// IsRateLimitMiddleware returns true if the given middleware name is classified as rate limiting.
func (mc MiddlewareConfig) IsRateLimitMiddleware(name string) bool {
	for _, m := range mc.RateLimit {
		if m == name {
			return true
		}
	}
	return false
}

// IsCSRFMiddleware returns true if the given middleware name is classified as CSRF protection.
// Defaults to matching "CSRF" if no CSRF middleware is configured.
func (mc MiddlewareConfig) IsCSRFMiddleware(name string) bool {
	if len(mc.CSRF) == 0 {
		return name == "CSRF"
	}
	for _, m := range mc.CSRF {
		if m == name {
			return true
		}
	}
	return false
}

// RuleEnabled returns true if the named rule is enabled.
// All rules default to true when not specified.
func (sc SqueezeConfig) RuleEnabled(name string) bool {
	if sc.Rules == nil {
		return true
	}
	enabled, ok := sc.Rules[name]
	if !ok {
		return true
	}
	return enabled
}

// LoadConfig reads pickle.yaml from the project root.
// Returns a default config if the file doesn't exist.
func LoadConfig(projectDir string) (*Config, error) {
	path := filepath.Join(projectDir, "pickle.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
