package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/ai-volund/volund/internal/config"
	"github.com/ai-volund/volund/internal/gateway"
)

func gatewayCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "gateway",
		Short: "Start the gateway (HTTP + gRPC)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			setupLogging(cfg.LogLevel)

			slog.Info("starting volund gateway", "version", version)

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			srv := gateway.New(cfg)
			if err := srv.Start(ctx); err != nil {
				return err
			}

			<-ctx.Done()
			slog.Info("shutdown signal received")

			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer shutdownCancel()

			srv.Stop(shutdownCtx)
			slog.Info("gateway stopped")
			return nil
		},
	}
}
