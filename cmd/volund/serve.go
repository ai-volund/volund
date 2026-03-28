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
	"github.com/ai-volund/volund/internal/gateway"
)

const shutdownTimeout = 10 * time.Second

func serveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start gateway and control plane in a single process",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			setupLogging(cfg.LogLevel)

			slog.Info("starting volund (all services)", "version", version)

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			gw := gateway.New(cfg)
			cp := controlplane.New(cfg)

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
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})))
}
