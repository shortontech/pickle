package config

type DatabaseConfig struct {
	Default     string
	Connections map[string]ConnectionConfig
}

func database() DatabaseConfig {
	return DatabaseConfig{
		Default: "pgsql",
		Connections: map[string]ConnectionConfig{
			"pgsql": {
				Driver:   "pgsql",
				Host:     Env("DB_HOST", "127.0.0.1"),
				Port:     Env("DB_PORT", "5432"),
				Name:     Env("DB_DATABASE", "pickle_adminlte_session"),
				User:     Env("DB_USERNAME", "postgres"),
				Password: Env("DB_PASSWORD", "pickle"),
				Options:  map[string]string{"sslmode": "disable"},
			},
		},
	}
}
