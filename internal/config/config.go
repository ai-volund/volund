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

	// NATS
	NATSUrl string // VOLUND_NATS_URL

	// Skills
	SkillConfigPath string // VOLUND_SKILL_CONFIG — JSON file mapping profiles to skills

	// OpenTelemetry
	OTLPEndpoint string // VOLUND_OTLP_ENDPOINT — gRPC endpoint for OTLP collector (e.g. "localhost:4317")
	Environment  string // VOLUND_ENV — deployment environment (dev, staging, prod)

	// Credential broker
	CredentialEncryptionKey string // VOLUND_CREDENTIAL_KEY — 32-byte hex key for AES-256-GCM

	// OIDC SSO providers (JSON-encoded array, or individual env vars)
	OIDCProviders string // VOLUND_OIDC_PROVIDERS — JSON array of provider configs

	// LLM providers
	OllamaURL       string // VOLUND_OLLAMA_URL — Ollama server URL (e.g. http://localhost:11434)
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

		NATSUrl:         os.Getenv("VOLUND_NATS_URL"),

		DatabaseURL:     envOrDefault("VOLUND_DATABASE_URL", "postgres://volund:volund@localhost:5432/volund?sslmode=disable"),
		OTLPEndpoint:            os.Getenv("VOLUND_OTLP_ENDPOINT"),
		Environment:             envOrDefault("VOLUND_ENV", "dev"),
		SkillConfigPath:         os.Getenv("VOLUND_SKILL_CONFIG"),
		CredentialEncryptionKey: os.Getenv("VOLUND_CREDENTIAL_KEY"),
		OIDCProviders:          os.Getenv("VOLUND_OIDC_PROVIDERS"),
		OllamaURL:              os.Getenv("VOLUND_OLLAMA_URL"),
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
