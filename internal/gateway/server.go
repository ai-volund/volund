package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/nats-io/nats.go"
	promhttp "github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"encoding/json"

	"github.com/ai-volund/volund/internal/auth"
	"github.com/ai-volund/volund/internal/config"
	"github.com/ai-volund/volund/internal/credentials"
	"github.com/ai-volund/volund/internal/db"
	"github.com/ai-volund/volund/internal/dispatch"
	"github.com/ai-volund/volund/internal/gateway/api"
	"github.com/ai-volund/volund/internal/llm"
	"github.com/ai-volund/volund/internal/storage"
	"github.com/ai-volund/volund/internal/usage"

	volundpb "github.com/ai-volund/volund-proto/gen/go/volund/v1"
)

// Server is the gateway server that exposes HTTP and gRPC endpoints.
type Server struct {
	cfg        *config.Config
	router     *llm.Router
	pool       *db.Pool
	dispatcher *dispatch.Dispatcher
	natsConn   *nats.Conn

	// claimMgr manages pod claim/release and the conv→instance routing cache.
	// nil when no DB pool is configured (gateway-only mode).
	claimMgr *ClaimManager

	// instanceSyncer watches agent heartbeats on NATS and syncs to DB.
	instSyncer *instanceSyncer
	// usageTracker subscribes to usage events on NATS and persists to DB.
	usageTracker *usage.Tracker

	// route is the shared routing table (Redis or in-memory fallback).
	// Used directly by ws.go when claimMgr is nil.
	route RoutingTable

	httpServer *http.Server
	grpcServer *grpc.Server
}

// New creates a new gateway Server.
func New(cfg *config.Config, router *llm.Router, pool *db.Pool) *Server {
	s := &Server{
		cfg:    cfg,
		router: router,
		pool:   pool,
	}

	// Initialize routing table — prefer Redis for multi-gateway, fall back to in-memory.
	if cfg.RedisAddr != "" {
		rt, err := NewRedisRoutingTable(cfg.RedisAddr)
		if err != nil {
			slog.Warn("redis routing table unavailable, falling back to in-memory",
				"addr", cfg.RedisAddr, "error", err)
			s.route = NewMemoryRoutingTable()
		} else {
			s.route = rt
		}
	} else {
		s.route = NewMemoryRoutingTable()
	}

	// Initialize the claim manager when a DB pool is available.
	if pool != nil {
		s.claimMgr = NewClaimManager(&agentRepoAdapter{repo: db.NewAgentRepo(pool)}, s.route)
	}
	return s
}

// agentRepoAdapter wraps *db.AgentRepo to satisfy the AgentInstanceRepo
// interface, converting db.AgentInstance → AgentInstanceInfo.
type agentRepoAdapter struct {
	repo *db.AgentRepo
}

func (a *agentRepoAdapter) ClaimInstance(ctx context.Context, tenantID, profileID string) (*AgentInstanceInfo, error) {
	inst, err := a.repo.ClaimInstance(ctx, tenantID, profileID)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, nil
	}
	var pid string
	if inst.ProfileID != nil {
		pid = *inst.ProfileID
	}
	var podName string
	if inst.PodName != nil {
		podName = *inst.PodName
	}
	return &AgentInstanceInfo{
		ID:        inst.ID,
		PodName:   podName,
		ProfileID: pid,
		State:     inst.State,
	}, nil
}

func (a *agentRepoAdapter) ReleaseInstance(ctx context.Context, id string) error {
	return a.repo.ReleaseInstance(ctx, id)
}

// Start launches the HTTP and gRPC servers.
func (s *Server) Start(ctx context.Context) error {
	// Connect to NATS for WebSocket bridging.
	if s.cfg.NATSUrl != "" {
		conn, err := nats.Connect(s.cfg.NATSUrl)
		if err != nil {
			slog.Warn("gateway: NATS unavailable, WebSocket streaming disabled", "error", err)
		} else {
			s.natsConn = conn
			slog.Info("gateway: connected to NATS", "url", s.cfg.NATSUrl)

			// Start instance syncer — watches heartbeats and syncs warm pool pods to DB.
			if s.pool != nil {
				syncer, err := startInstanceSyncer(conn, db.NewAgentRepo(s.pool))
				if err != nil {
					slog.Warn("gateway: instance syncer failed to start", "error", err)
				} else {
					s.instSyncer = syncer
				}

				// Start usage tracker.
				ut, err := usage.StartTracker(conn, db.NewUsageRepo(s.pool))
				if err != nil {
					slog.Warn("gateway: usage tracker failed to start", "error", err)
				} else {
					s.usageTracker = ut
				}
			}
		}
	}

	// Connect dispatcher (publishes tasks to agent pool).
	d, err := dispatch.Connect(s.cfg.NATSUrl)
	if err != nil {
		slog.Warn("gateway: task dispatcher unavailable", "error", err)
	} else {
		s.dispatcher = d
	}

	tm := auth.NewTokenManager(s.cfg.JWTSecret)

	// --- gRPC server ---
	s.grpcServer = grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(auth.UnaryServerInterceptor(tm)),
		grpc.ChainStreamInterceptor(auth.StreamServerInterceptor(tm)),
	)

	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(s.grpcServer, healthSrv)
	healthSrv.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	reflection.Register(s.grpcServer)

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

	// WebSocket: real-time agent stream bridge.
	mux.HandleFunc("GET /ws/conv/{id}", s.handleConvStream)
	// Keep old path responding with a deprecation notice.
	mux.HandleFunc("/ws/chat", s.legacyHandleChat)

	// REST API — only register if we have a DB pool.
	// Skill resolver — reads skill config to include in task dispatches.
	profileResolver := NewStaticProfileResolver(s.cfg.SkillConfigPath)

	if s.pool != nil {
		credRepo := db.NewCredentialRepo(s.pool)
		var credBroker *credentials.Broker
		if s.cfg.CredentialEncryptionKey != "" {
			var err error
			credBroker, err = credentials.NewBroker(credRepo, s.cfg.CredentialEncryptionKey)
			if err != nil {
				slog.Warn("credential broker disabled", "error", err)
			} else {
				slog.Info("credential broker enabled")
			}
		}

		// Embedding function for memory — routes through the LLM router.
		embedFn := func(ctx context.Context, texts []string) ([][]float32, error) {
			vecs, _, err := s.router.Embed(ctx, "", "text-embedding-3-small", texts)
			return vecs, err
		}

		// Initialize OIDC manager if configured.
		var oidcMgr *auth.OIDCManager
		if s.cfg.OIDCProviders != "" {
			var providers []auth.OIDCProviderConfig
			if err := json.Unmarshal([]byte(s.cfg.OIDCProviders), &providers); err != nil {
				slog.Warn("failed to parse OIDC providers config", "error", err)
			} else if len(providers) > 0 {
				mgr, err := auth.NewOIDCManager(ctx, providers)
				if err != nil {
					slog.Warn("OIDC provider discovery failed", "error", err)
				} else {
					oidcMgr = mgr
					slog.Info("OIDC SSO enabled", "providers", mgr.Providers())
				}
			}
		}

		// Initialize object storage for file attachments.
		var store storage.Store
		switch s.cfg.StorageBackend {
		case "s3":
			store = storage.NewS3Store(storage.S3Config{
				Endpoint:     s.cfg.S3Endpoint,
				Bucket:       s.cfg.S3Bucket,
				AccessKey:    s.cfg.S3AccessKey,
				SecretKey:    s.cfg.S3SecretKey,
				UsePathStyle: s.cfg.S3UsePathStyle,
			})
			slog.Info("file storage: S3", "endpoint", s.cfg.S3Endpoint, "bucket", s.cfg.S3Bucket)
		default:
			ls, err := storage.NewLocalStore(s.cfg.StorageLocalDir,
				fmt.Sprintf("http://localhost%s/v1/attachments", s.cfg.GatewayHTTPAddr))
			if err != nil {
				slog.Warn("local storage init failed, file uploads disabled", "error", err)
			} else {
				store = ls
				slog.Info("file storage: local", "path", s.cfg.StorageLocalDir)
			}
		}

		svc := &api.Services{
			Auth:            auth.NewService(tm, db.NewUserRepo(s.pool), db.NewTenantRepo(s.pool), db.NewRefreshTokenRepo(s.pool)),
			TM:              tm,
			Users:           db.NewUserRepo(s.pool),
			Tenants:         db.NewTenantRepo(s.pool),
			Agents:          db.NewAgentRepo(s.pool),
			Convos:          db.NewConversationRepo(s.pool),
			Invites:         db.NewInviteRepo(s.pool),
			Dispatcher:      s.dispatcher,
			DispatchFn:      s.dispatchTask,
			ClaimMgr:        s.claimMgr,
			Tasks:           db.NewTaskRepo(s.pool),
			Skills:          db.NewSkillRepo(s.pool),
			Memories:        db.NewMemoryRepo(s.pool),
			Credentials:     credRepo,
			CredBroker:      credBroker,
			EmbedFn:         embedFn,
			Usage:           db.NewUsageRepo(s.pool),
			Quotas:          db.NewQuotaRepo(s.pool),
			ProfileResolver: profileResolver,
			OIDCMgr:         oidcMgr,
			Attachments:     db.NewAttachmentRepo(s.pool),
			Store:           store,
			MaxUploadSize:   s.cfg.MaxUploadSize,
		}
		api.Register(mux, svc)
		slog.Info("REST API routes registered")
	} else {
		slog.Warn("no database pool — REST API routes not registered")
	}

	// Prometheus metrics endpoint.
	mux.Handle("GET /metrics", promhttp.Handler())

	handler := RecoveryMiddleware(
		CORSMiddleware(
			LoggingMiddleware(
				otelhttp.NewMiddleware("volund-gateway")(
					auth.HTTPMiddleware(tm, mux),
				),
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

// dispatchTask is the callback passed to API handlers so they can dispatch
// without importing the gateway package (avoiding circular deps).
func (s *Server) dispatchTask(ctx context.Context, task *dispatch.Task) error {
	if s.dispatcher == nil {
		return fmt.Errorf("dispatcher not available")
	}

	// If the task already has a claimed instance, dispatch directly to it.
	if task.InstanceID != "" {
		if err := s.dispatcher.DispatchToInstance(ctx, task, task.InstanceID); err != nil {
			return err
		}
	} else {
		// Fall back to pool dispatch.
		if err := s.dispatcher.Dispatch(ctx, task); err != nil {
			return err
		}
	}

	// Start watching the conv stream. The watcher will update the routing table
	// when the agent emits agent_start (with its instance_id) and clear it on agent_end.
	if task.ConversationID != "" {
		go s.watchConvStream(ctx, task.ConversationID, task.DBTaskID)
	}
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
	if s.instSyncer != nil {
		s.instSyncer.Close()
	}
	if s.usageTracker != nil {
		s.usageTracker.Close()
	}
	if s.dispatcher != nil {
		s.dispatcher.Close()
	}
	if s.natsConn != nil {
		s.natsConn.Drain() //nolint:errcheck
	}
	if s.route != nil {
		s.route.Close()
	}
}
