package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/ai-volund/volund/internal/auth"
	"github.com/ai-volund/volund/internal/config"
	"github.com/ai-volund/volund/internal/llm"

	volundpb "github.com/ai-volund/volund-proto/gen/go/volund/v1"
)

// Server is the gateway server that exposes HTTP and gRPC endpoints.
type Server struct {
	cfg        *config.Config
	router     *llm.Router
	httpServer *http.Server
	grpcServer *grpc.Server
}

// New creates a new gateway Server.
func New(cfg *config.Config, router *llm.Router) *Server {
	return &Server{cfg: cfg, router: router}
}

// Start launches the HTTP and gRPC servers.
func (s *Server) Start(ctx context.Context) error {
	tm := auth.NewTokenManager(s.cfg.JWTSecret)

	// --- gRPC server ---
	s.grpcServer = grpc.NewServer(
		grpc.ChainUnaryInterceptor(auth.UnaryServerInterceptor(tm)),
		grpc.ChainStreamInterceptor(auth.StreamServerInterceptor(tm)),
	)

	// Health check
	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(s.grpcServer, healthSrv)
	healthSrv.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	// Reflection for debugging
	reflection.Register(s.grpcServer)

	// Register stub gateway services
	volundpb.RegisterAuthServiceServer(s.grpcServer, &volundpb.UnimplementedAuthServiceServer{})
	volundpb.RegisterChatServiceServer(s.grpcServer, &volundpb.UnimplementedChatServiceServer{})
	volundpb.RegisterForgeServiceServer(s.grpcServer, &volundpb.UnimplementedForgeServiceServer{})
	volundpb.RegisterUsageServiceServer(s.grpcServer, &volundpb.UnimplementedUsageServiceServer{})

	grpcLis, err := net.Listen("tcp", s.cfg.GatewayGRPCAddr)
	if err != nil {
		return fmt.Errorf("gateway grpc listen: %w", err)
	}

	go func() {
		slog.Info("gateway gRPC server starting", "addr", s.cfg.GatewayGRPCAddr)
		if err := s.grpcServer.Serve(grpcLis); err != nil {
			slog.Error("gateway gRPC server error", "error", err)
		}
	}()

	// --- HTTP server ---
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	// WebSocket: real-time LLM streaming
	mux.HandleFunc("/ws/chat", s.handleChat)

	// Apply middleware chain: recovery -> cors -> logging -> auth -> handler
	handler := RecoveryMiddleware(
		CORSMiddleware(
			LoggingMiddleware(
				auth.HTTPMiddleware(tm, mux),
			),
		),
	)

	s.httpServer = &http.Server{
		Addr:    s.cfg.GatewayHTTPAddr,
		Handler: handler,
	}

	go func() {
		slog.Info("gateway HTTP server starting", "addr", s.cfg.GatewayHTTPAddr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("gateway HTTP server error", "error", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down both servers.
func (s *Server) Stop(ctx context.Context) {
	if s.httpServer != nil {
		slog.Info("shutting down gateway HTTP server")
		if err := s.httpServer.Shutdown(ctx); err != nil {
			slog.Error("gateway HTTP shutdown error", "error", err)
		}
	}
	if s.grpcServer != nil {
		slog.Info("shutting down gateway gRPC server")
		s.grpcServer.GracefulStop()
	}
}
