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

	// Redis — shared routing table and session cache
	RedisAddr string // VOLUND_REDIS_ADDR — Redis address (e.g. "redis:6379")

	// Object storage for file attachments
	StorageBackend  string // VOLUND_STORAGE_BACKEND — "local" (default) or "s3"
	StorageLocalDir string // VOLUND_STORAGE_LOCAL_DIR — local filesystem path for attachments
	S3Endpoint      string // VOLUND_S3_ENDPOINT — S3/MinIO endpoint (e.g. "minio:9000")
	S3Bucket        string // VOLUND_S3_BUCKET — bucket name
	S3AccessKey     string // VOLUND_S3_ACCESS_KEY
	S3SecretKey     string // VOLUND_S3_SECRET_KEY
	S3UsePathStyle  bool   // VOLUND_S3_PATH_STYLE — "true" for MinIO
	MaxUploadSize   int64  // VOLUND_MAX_UPLOAD_SIZE — max file size in bytes (default 100MB)

	// OIDC SSO providers (JSON-encoded array, or individual env vars)
	OIDCProviders string // VOLUND_OIDC_PROVIDERS — JSON array of provider configs

	// Base URL for OAuth redirect callbacks (e.g. "http://localhost:8080")
	// OAuth providers are registered at runtime via the admin API, not compiled in.
	BaseURL string // VOLUND_BASE_URL

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
		RedisAddr:              os.Getenv("VOLUND_REDIS_ADDR"),
		StorageBackend:         envOrDefault("VOLUND_STORAGE_BACKEND", "local"),
		StorageLocalDir:        envOrDefault("VOLUND_STORAGE_LOCAL_DIR", "/data/attachments"),
		S3Endpoint:             os.Getenv("VOLUND_S3_ENDPOINT"),
		S3Bucket:               envOrDefault("VOLUND_S3_BUCKET", "volund-attachments"),
		S3AccessKey:            os.Getenv("VOLUND_S3_ACCESS_KEY"),
		S3SecretKey:            os.Getenv("VOLUND_S3_SECRET_KEY"),
		S3UsePathStyle:         os.Getenv("VOLUND_S3_PATH_STYLE") == "true",
		MaxUploadSize:          parseIntOrDefault(os.Getenv("VOLUND_MAX_UPLOAD_SIZE"), 100*1024*1024),
		CredentialEncryptionKey: os.Getenv("VOLUND_CREDENTIAL_KEY"),
		OIDCProviders:          os.Getenv("VOLUND_OIDC_PROVIDERS"),
		BaseURL:                envOrDefault("VOLUND_BASE_URL", "http://localhost:8080"),
		OllamaURL:              os.Getenv("VOLUND_OLLAMA_URL"),
		OpenAIAPIKey:    os.Getenv("VOLUND_OPENAI_API_KEY"),
		OpenAIBaseURL:   os.Getenv("VOLUND_OPENAI_BASE_URL"),
		AnthropicAPIKey: os.Getenv("VOLUND_ANTHROPIC_API_KEY"),
	}
}

func parseIntOrDefault(s string, fallback int64) int64 {
	if s == "" {
		return fallback
	}
	var v int64
	for _, c := range s {
		if c >= '0' && c <= '9' {
			v = v*10 + int64(c-'0')
		} else {
			return fallback
		}
	}
	return v
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
