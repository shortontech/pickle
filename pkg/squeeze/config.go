package squeeze

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the top-level pickle.yaml file.
type Config struct {
	Squeeze SqueezeConfig `yaml:"squeeze"`
}

// SqueezeConfig holds all squeeze-related configuration.
type SqueezeConfig struct {
	Middleware MiddlewareConfig `yaml:"middleware"`
	Rules      map[string]bool  `yaml:"rules"`
}

// MiddlewareConfig classifies middleware by role.
type MiddlewareConfig struct {
	Auth  []string `yaml:"auth"`  // middleware that provides authentication
	Admin []string `yaml:"admin"` // middleware that provides admin access (implies auth)
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
