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
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	return &Config{
		GatewayHTTPAddr:  envOrDefault("VOLUND_GATEWAY_HTTP_ADDR", ":8080"),
		GatewayGRPCAddr:  envOrDefault("VOLUND_GATEWAY_GRPC_ADDR", ":9090"),
		ControlPlaneAddr: envOrDefault("VOLUND_CONTROLPLANE_ADDR", ":9091"),
		JWTSecret:        envOrDefault("VOLUND_JWT_SECRET", "dev-secret-change-me"),
		LogLevel:         envOrDefault("VOLUND_LOG_LEVEL", "info"),
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
