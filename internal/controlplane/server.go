package controlplane

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/ai-volund/volund/internal/agent"
	"github.com/ai-volund/volund/internal/auth"
	"github.com/ai-volund/volund/internal/config"
	"github.com/ai-volund/volund/internal/llm"
	"github.com/ai-volund/volund/internal/task"
	"github.com/ai-volund/volund/internal/tenant"

	volundpb "github.com/ai-volund/volund-proto/gen/go/volund/v1"
)

// Server is the control plane gRPC server.
type Server struct {
	cfg        *config.Config
	router     *llm.Router
	grpcServer *grpc.Server
}

// New creates a new controlplane Server.
func New(cfg *config.Config, router *llm.Router) *Server {
	return &Server{cfg: cfg, router: router}
}

// Start launches the control plane gRPC server.
func (s *Server) Start(ctx context.Context) error {
	tm := auth.NewTokenManager(s.cfg.JWTSecret)

	s.grpcServer = grpc.NewServer(
		grpc.ChainUnaryInterceptor(auth.UnaryServerInterceptor(tm)),
		grpc.ChainStreamInterceptor(auth.StreamServerInterceptor(tm)),
	)

	// Health check
	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(s.grpcServer, healthSrv)
	healthSrv.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	// Reflection
	reflection.Register(s.grpcServer)

	// Register control plane services
	volundpb.RegisterTenantServiceServer(s.grpcServer, tenant.NewService())
	volundpb.RegisterAgentServiceServer(s.grpcServer, agent.NewService())
	volundpb.RegisterTaskServiceServer(s.grpcServer, task.NewService())
	volundpb.RegisterLLMServiceServer(s.grpcServer, llm.NewService(s.router))

	lis, err := net.Listen("tcp", s.cfg.ControlPlaneAddr)
	if err != nil {
		return fmt.Errorf("controlplane listen: %w", err)
	}

	go func() {
		slog.Info("controlplane gRPC server starting", "addr", s.cfg.ControlPlaneAddr)
		if err := s.grpcServer.Serve(lis); err != nil {
			slog.Error("controlplane gRPC server error", "error", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the control plane server.
func (s *Server) Stop(_ context.Context) {
	if s.grpcServer != nil {
		slog.Info("shutting down controlplane gRPC server")
		s.grpcServer.GracefulStop()
	}
}
