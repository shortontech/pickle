package config

type DatabaseConfig struct {
	Default     string
	Connections map[string]ConnectionConfig
}

func database() DatabaseConfig {
	return DatabaseConfig{
		Default: Env("DB_CONNECTION", "sqlite"),
		Connections: map[string]ConnectionConfig{
			"sqlite": {
				Driver: "sqlite",
				Name:   Env("DB_DATABASE", ":memory:"),
			},
		},
	}
}
