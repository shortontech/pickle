package config

type AppConfig struct {
	Name  string
	Env   string
	Debug bool
	Port  string
}

func app() AppConfig {
	return AppConfig{
		Name:  Env("APP_NAME", "monorepo"),
		Env:   Env("APP_ENV", "local"),
		Debug: Env("APP_DEBUG", "true") == "true",
		Port:  Env("APP_PORT", "8080"),
	}
}
