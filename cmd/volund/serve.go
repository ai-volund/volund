package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/ai-volund/volund/internal/config"
	"github.com/ai-volund/volund/internal/controlplane"
	"github.com/ai-volund/volund/internal/db"
	"github.com/ai-volund/volund/internal/gateway"
	"github.com/ai-volund/volund/internal/llm"
	votel "github.com/ai-volund/volund/internal/otel"
	anthropicProvider "github.com/ai-volund/volund/internal/llm/anthropic"
	ollamaProvider "github.com/ai-volund/volund/internal/llm/ollama"
	openaiProvider "github.com/ai-volund/volund/internal/llm/openai"
)

const shutdownTimeout = 10 * time.Second

func serveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start gateway and control plane in a single process",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			setupLogging(cfg.LogLevel)

			// Initialize OpenTelemetry.
			otelShutdown, err := votel.Init(votel.Config{
				ServiceName:  "volund-gateway",
				OTLPEndpoint: cfg.OTLPEndpoint,
				Environment:  cfg.Environment,
			})
			if err != nil {
				slog.Error("otel init failed", "error", err)
				os.Exit(1)
			}
			defer func() {
				if err := otelShutdown(context.Background()); err != nil {
					slog.Error("otel shutdown error", "error", err)
				}
			}()

			slog.Info("starting volund (all services)", "version", version)

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			// Database
			var pool *db.Pool
			if cfg.DatabaseURL != "" {
				if err := db.Migrate(cfg.DatabaseURL); err != nil {
					slog.Error("migration failed", "error", err)
					os.Exit(1)
				}
				var err error
				pool, err = db.Connect(ctx, cfg.DatabaseURL)
				if err != nil {
					slog.Error("db connect failed", "error", err)
					os.Exit(1)
				}
				defer pool.Close()
				slog.Info("database connected")
			} else {
				slog.Warn("VOLUND_DATABASE_URL not set — running without database")
			}

			router := buildRouter(cfg)

			gw := gateway.New(cfg, router, pool)
			cp := controlplane.New(cfg, router)

			if err := gw.Start(ctx); err != nil {
				return err
			}
			if err := cp.Start(ctx); err != nil {
				return err
			}

			<-ctx.Done()
			slog.Info("shutdown signal received")

			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer shutdownCancel()

			cp.Stop(shutdownCtx)
			gw.Stop(shutdownCtx)
			slog.Info("all services stopped")
			return nil
		},
	}

	cmd.Flags().Bool("all", true, "Run all services (default)")
	return cmd
}

// buildRouter creates the LLM router and registers providers based on config.
func buildRouter(cfg *config.Config) *llm.Router {
	router := llm.NewRouter()
	if cfg.OpenAIAPIKey != "" {
		router.Register(openaiProvider.New(cfg.OpenAIAPIKey, cfg.OpenAIBaseURL))
		slog.Info("registered LLM provider", "provider", "openai")
	}
	if cfg.AnthropicAPIKey != "" {
		router.Register(anthropicProvider.New(cfg.AnthropicAPIKey, ""))
		slog.Info("registered LLM provider", "provider", "anthropic")
	}
	if cfg.OllamaURL != "" {
		router.Register(ollamaProvider.New(cfg.OllamaURL))
		slog.Info("registered LLM provider", "provider", "ollama", "url", cfg.OllamaURL)
	}
	if len(router.Providers()) == 0 {
		slog.Warn("no LLM providers configured — set VOLUND_OPENAI_API_KEY, VOLUND_ANTHROPIC_API_KEY, or VOLUND_OLLAMA_URL")
	}
	return router
}

func setupLogging(level string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	jsonHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	slog.SetDefault(slog.New(votel.NewTraceSlogHandler(jsonHandler)))
}
