package credentialproxy

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/agentserver/agentserver/internal/crypto"
)

// Config holds all configuration for the credential proxy.
type Config struct {
	Port                  string
	DatabaseURL           string
	AgentserverURL        string
	EncryptionKey         []byte
	LogLevel              slog.Level
	UpstreamTimeout       time.Duration
	AllowPrivateUpstreams bool
}

// LoadConfigFromEnv reads configuration from environment variables.
func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		Port:            envOr("CREDPROXY_PORT", "8083"),
		DatabaseURL:     os.Getenv("CREDPROXY_DATABASE_URL"),
		AgentserverURL:  os.Getenv("CREDPROXY_AGENTSERVER_URL"),
		UpstreamTimeout: 60 * time.Second,
	}

	if cfg.DatabaseURL == "" {
		return cfg, fmt.Errorf("CREDPROXY_DATABASE_URL is required")
	}
	if cfg.AgentserverURL == "" {
		return cfg, fmt.Errorf("CREDPROXY_AGENTSERVER_URL is required")
	}

	key, err := crypto.LoadKeyFromEnv("CREDPROXY_ENCRYPTION_KEY")
	if err != nil {
		return cfg, fmt.Errorf("load encryption key: %w", err)
	}
	cfg.EncryptionKey = key

	if v := os.Getenv("CREDPROXY_LOG_LEVEL"); v != "" {
		switch strings.ToLower(v) {
		case "debug":
			cfg.LogLevel = slog.LevelDebug
		case "info":
			cfg.LogLevel = slog.LevelInfo
		case "warn":
			cfg.LogLevel = slog.LevelWarn
		case "error":
			cfg.LogLevel = slog.LevelError
		}
	}

	if v := os.Getenv("CREDPROXY_UPSTREAM_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			cfg.UpstreamTimeout = d
		}
	}

	cfg.AllowPrivateUpstreams = os.Getenv("CREDPROXY_ALLOW_PRIVATE_UPSTREAMS") == "true"

	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
