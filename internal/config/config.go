package config

import (
	"os"
)

// Config holds all configuration for the Volund platform.
type Config struct {
	GatewayHTTPAddr  string
	GatewayGRPCAddr  string
	ControlPlaneAddr string
	JWTSecret        string
	LogLevel         string

	// Database
	DatabaseURL string // VOLUND_DATABASE_URL (postgres DSN)

	// LLM providers
	OpenAIAPIKey    string // VOLUND_OPENAI_API_KEY
	OpenAIBaseURL   string // VOLUND_OPENAI_BASE_URL (optional, for custom endpoints)
	AnthropicAPIKey string // VOLUND_ANTHROPIC_API_KEY
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	return &Config{
		GatewayHTTPAddr:  envOrDefault("VOLUND_GATEWAY_HTTP_ADDR", ":8080"),
		GatewayGRPCAddr:  envOrDefault("VOLUND_GATEWAY_GRPC_ADDR", ":9090"),
		ControlPlaneAddr: envOrDefault("VOLUND_CONTROLPLANE_ADDR", ":9091"),
		JWTSecret:        envOrDefault("VOLUND_JWT_SECRET", "dev-secret-change-me"),
		LogLevel:         envOrDefault("VOLUND_LOG_LEVEL", "info"),

		DatabaseURL:     envOrDefault("VOLUND_DATABASE_URL", "postgres://volund:volund@localhost:5432/volund?sslmode=disable"),
		OpenAIAPIKey:    os.Getenv("VOLUND_OPENAI_API_KEY"),
		OpenAIBaseURL:   os.Getenv("VOLUND_OPENAI_BASE_URL"),
		AnthropicAPIKey: os.Getenv("VOLUND_ANTHROPIC_API_KEY"),
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
