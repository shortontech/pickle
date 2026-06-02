package config

type AppConfig struct {
	Name  string
	Env   string
	Debug bool
	Port  string
}

func app() AppConfig {
	return AppConfig{
		Name:  Env("APP_NAME", "graphql-safety"),
		Env:   Env("APP_ENV", "test"),
		Debug: Env("APP_DEBUG", "false") == "true",
		Port:  Env("APP_PORT", "8080"),
	}
}
