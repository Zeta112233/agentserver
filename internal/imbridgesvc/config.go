package imbridgesvc

import "os"

// Config holds configuration for the standalone imbridge service.
type Config struct {
	ListenAddr  string // IMBRIDGE_LISTEN_ADDR, default ":8083"
	DatabaseURL string // DATABASE_URL (shared PostgreSQL with agentserver)
}

// LoadConfigFromEnv returns a Config populated from environment variables.
func LoadConfigFromEnv() Config {
	return Config{
		ListenAddr:  envOr("IMBRIDGE_LISTEN_ADDR", ":8083"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
