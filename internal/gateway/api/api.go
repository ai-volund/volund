// Package api contains the HTTP REST API handlers for the Volund gateway.
// Routes follow /v1/{resource} conventions and return JSON.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ai-volund/volund/internal/auth"
	"github.com/ai-volund/volund/internal/credentials"
	"github.com/ai-volund/volund/internal/db"
	"github.com/ai-volund/volund/internal/dispatch"
	"github.com/ai-volund/volund/internal/providers"
	"github.com/ai-volund/volund/internal/storage"
)

// ClaimResult holds the result of a pod claim.
type ClaimResult struct {
	InstanceID string // DB UUID — used for task tracking
	PodName    string // K8s pod name — used as NATS dispatch subject
}

// InstanceClaimer manages pod claim/release for conversations.
// Satisfied by *gateway.ClaimManager; kept as an interface to avoid circular deps.
type InstanceClaimer interface {
	EnsureInstance(ctx context.Context, convID, tenantID, profileID string) (*ClaimResult, error)
	Release(ctx context.Context, convID string) error
}

// ProfileResolver resolves an agent profile's configuration and skills
// for inclusion in the task dispatch payload.
type ProfileResolver interface {
	ResolveProfile(ctx context.Context, profileName string) (*dispatch.ProfileConfig, []dispatch.SkillSpec, error)
}

// Services holds all dependencies the API handlers need.
type Services struct {
	// Pool is the raw database pool for ad-hoc queries (e.g. ba_user_map lookups).
	Pool    *db.Pool
	Users   *db.UserRepo
	Tenants *db.TenantRepo
	Agents  *db.AgentRepo
	Convos  *db.ConversationRepo
	Invites *db.InviteRepo
	Skills      *db.SkillRepo
	Memories    *db.MemoryRepo
	Tasks       *db.TaskRepo
	Credentials *db.CredentialRepo
	CredBroker  *credentials.Broker
	// EmbedFn generates embeddings via the LLM router. nil = embeddings unavailable.
	EmbedFn func(ctx context.Context, texts []string) ([][]float32, error)

	// Dispatcher publishes tasks to the agent pool via NATS.
	Dispatcher *dispatch.Dispatcher
	// DispatchFn is the gateway callback for dispatching a task (sets routing table).
	DispatchFn func(ctx context.Context, task *dispatch.Task) error
	// ClaimMgr manages pod claim/release. nil when DB is not configured.
	ClaimMgr InstanceClaimer
	// ProfileResolver resolves profile config + skills. nil = no profile resolution.
	ProfileResolver ProfileResolver
	// Usage tracks token usage. nil = usage tracking disabled.
	Usage *db.UsageRepo
	// Quotas tracks tenant quotas. nil = quotas disabled (unlimited).
	Quotas *db.QuotaRepo
	// OIDCMgr removed — OIDC is now handled by volund-auth (better-auth).
	// Attachments manages file attachment records. nil = uploads disabled.
	Attachments *db.AttachmentRepo
	// Store is the object storage backend for file attachments. nil = uploads disabled.
	Store storage.Store
	// MaxUploadSize is the maximum file upload size in bytes.
	MaxUploadSize int64
	// OAuth is the generic OAuth2 engine for service provider connections. nil = disabled.
	OAuth *providers.Engine
	// Audit is the audit log repo. nil = audit disabled.
	Audit *db.AuditRepo
	// LLMProviders is the DB repo for admin-managed LLM provider config. nil = disabled.
	LLMProviders *db.LLMProviderRepo
	// LLMListModelsFn lists all models across providers. nil = not available.
	LLMListModelsFn func(ctx context.Context, provider string) (any, error)
	// LLMReloadFn is called after provider CRUD to hot-reload the router. nil = no-op.
	LLMReloadFn func()
	// LLMTestFn tests a provider config by listing its models. nil = testing disabled.
	LLMTestFn func(ctx context.Context, p *db.LLMProvider) ([]string, error)
}

// Register mounts all API routes on mux under /v1/.
func Register(mux *http.ServeMux, svc *Services) {
	// Auth — /v1/auth/me still works with JWT claims.
	// All other auth endpoints removed — auth is now handled by volund-auth (better-auth).
	mux.HandleFunc("GET /v1/auth/me", RequireAuth(svc.handleMe))

	// Tenants
	mux.HandleFunc("POST /v1/tenants", RequireAuth(svc.handleCreateTenant))
	mux.HandleFunc("GET /v1/tenants", RequireAuth(svc.handleListTenants))
	mux.HandleFunc("GET /v1/tenants/{id}", RequireAuth(svc.handleGetTenant))
	mux.HandleFunc("PUT /v1/tenants/{id}", RequireAuth(svc.handleUpdateTenant))
	mux.HandleFunc("DELETE /v1/tenants/{id}", RequireAuth(svc.handleDeleteTenant))
	mux.HandleFunc("GET /v1/tenants/{id}/members", RequireAuth(svc.handleListMembers))
	mux.HandleFunc("POST /v1/tenants/{id}/invites", RequireAuth(svc.handleCreateInvite))

	// Invite accept (public — token is the secret)
	mux.HandleFunc("POST /v1/invites/{token}/accept", svc.handleAcceptInvite)

	// Agent profiles — user-scoped custom agents
	mux.HandleFunc("POST /v1/agents", RequireAuth(svc.handleCreateAgent))
	mux.HandleFunc("GET /v1/agents", RequireAuth(svc.handleListAgents))
	mux.HandleFunc("GET /v1/agents/{id}", RequireAuth(svc.handleGetAgent))
	mux.HandleFunc("PUT /v1/agents/{id}", RequireAuth(svc.handleUpdateAgent))
	mux.HandleFunc("DELETE /v1/agents/{id}", RequireAuth(svc.handleDeleteAgent))

	// Agent profiles — admin-managed system agents
	// Admin — member management
	mux.HandleFunc("PUT /v1/admin/tenants/{id}/members/{userId}/role", RequireAdmin(svc.handleUpdateMemberRole))
	mux.HandleFunc("DELETE /v1/admin/tenants/{id}/members/{userId}", RequireAdmin(svc.handleRemoveMember))

	// Admin — agent instances & warm pool
	mux.HandleFunc("GET /v1/admin/instances", RequireAdmin(svc.handleListInstances))
	mux.HandleFunc("DELETE /v1/admin/instances/{id}", RequireAdmin(svc.handleForceReleaseInstance))
	mux.HandleFunc("GET /v1/admin/warmpool", RequireAdmin(svc.handleWarmPoolStats))

	// Admin — platform health
	mux.HandleFunc("GET /v1/admin/health", RequireAdmin(svc.handlePlatformHealth))

	// Admin — audit log
	mux.HandleFunc("GET /v1/admin/audit", RequireAdmin(svc.handleListAudit))

	// Agent profiles — admin-managed system agents
	mux.HandleFunc("POST /v1/admin/agents", RequireAdmin(svc.handleCreateSystemAgent))

	// Conversations
	mux.HandleFunc("POST /v1/conversations", RequireAuth(svc.handleCreateConversation))
	mux.HandleFunc("GET /v1/conversations", RequireAuth(svc.handleListConversations))
	mux.HandleFunc("GET /v1/conversations/{id}", RequireAuth(svc.handleGetConversation))
	mux.HandleFunc("PATCH /v1/conversations/{id}", RequireAuth(svc.handleUpdateConversation))
	mux.HandleFunc("DELETE /v1/conversations/{id}", RequireAuth(svc.handleDeleteConversation))
	mux.HandleFunc("POST /v1/conversations/{id}/messages", RequireAuth(svc.handlePostMessage))
	mux.HandleFunc("POST /v1/conversations/{id}/attachments", RequireAuth(svc.handleUploadAttachment))
	mux.HandleFunc("GET /v1/conversations/{id}/attachments", RequireAuth(svc.handleListAttachments))
	mux.HandleFunc("GET /v1/attachments/{id}", RequireAuth(svc.handleGetAttachment))

	// Tasks
	mux.HandleFunc("GET /v1/tasks", RequireAuth(svc.handleListTasks))
	mux.HandleFunc("GET /v1/tasks/{id}", RequireAuth(svc.handleGetTask))
	mux.HandleFunc("POST /v1/tasks/{id}/cancel", RequireAuth(svc.handleCancelTask))

	// Skill activation — tenant install + user enable
	mux.HandleFunc("GET /v1/skills", RequireAuth(svc.handleListAvailableSkills))
	mux.HandleFunc("POST /v1/skills/{id}/enable", RequireAuth(svc.handleEnableSkill))
	mux.HandleFunc("DELETE /v1/skills/{id}/enable", RequireAuth(svc.handleDisableSkill))
	mux.HandleFunc("POST /v1/admin/skills/{id}/install", RequireAdmin(svc.handleInstallSkill))
	mux.HandleFunc("DELETE /v1/admin/skills/{id}/install", RequireAdmin(svc.handleUninstallSkill))
	mux.HandleFunc("GET /v1/admin/skills/installed", RequireAdmin(svc.handleListInstalledSkills))

	// Forge registry — skill catalog (public read, auth required for write)
	mux.HandleFunc("GET /v1/forge/skills", svc.handleListSkills)
	mux.HandleFunc("GET /v1/forge/skills/{id}", svc.handleGetSkill)
	mux.HandleFunc("POST /v1/forge/skills", RequireAuth(svc.handlePublishSkill))
	mux.HandleFunc("PUT /v1/forge/skills/{id}", RequireAuth(svc.handleUpdateSkill))
	mux.HandleFunc("DELETE /v1/forge/skills/{id}", RequireAuth(svc.handleDeleteSkill))

	// Memory — long-term memory with pgvector embeddings
	mux.HandleFunc("POST /v1/memory", RequireAuth(svc.handleStoreMemory))
	mux.HandleFunc("POST /v1/memory/search", RequireAuth(svc.handleSearchMemory))
	mux.HandleFunc("GET /v1/memory", RequireAuth(svc.handleListMemories))
	mux.HandleFunc("DELETE /v1/memory/{id}", RequireAuth(svc.handleDeleteMemory))

	// Usage tracking
	mux.HandleFunc("GET /v1/usage/summary", RequireAuth(svc.handleUsageSummary))
	mux.HandleFunc("GET /v1/usage/breakdown", RequireAuth(svc.handleUsageBreakdown))
	mux.HandleFunc("GET /v1/usage/quota", RequireAuth(svc.handleQuotaStatus))
	mux.HandleFunc("GET /v1/usage/conversations/{id}", RequireAuth(svc.handleUsageByConversation))

	// Credential broker — per-user credential management + agent token issuance
	mux.HandleFunc("POST /v1/credentials", RequireAuth(svc.handleStoreCredential))
	mux.HandleFunc("GET /v1/credentials", RequireAuth(svc.handleListCredentials))
	mux.HandleFunc("DELETE /v1/credentials/{provider}", RequireAuth(svc.handleDeleteCredential))
	mux.HandleFunc("POST /v1/credentials/token", RequireAuth(svc.handleIssueToken))
	mux.HandleFunc("GET /v1/credentials/audit", RequireAuth(svc.handleCredentialAudit))

	// OAuth connect flow — generic OAuth2 provider connections
	mux.HandleFunc("GET /v1/connect", RequireAuth(svc.handleConnectList))
	mux.HandleFunc("GET /v1/connect/{provider}", RequireAuth(svc.handleConnectStart))
	mux.HandleFunc("GET /v1/connect/{provider}/callback", svc.handleConnectCallback) // no auth — user returns from OAuth redirect

	// Admin — OAuth provider management (credentials only; definitions come from skill manifests)
	mux.HandleFunc("PUT /v1/admin/providers/{id}/credentials", RequireAdmin(svc.handleSetProviderCredentials))
	mux.HandleFunc("GET /v1/admin/providers", RequireAdmin(svc.handleListProviders))
	mux.HandleFunc("DELETE /v1/admin/providers/{id}", RequireAdmin(svc.handleDeleteProvider))

	// Admin — LLM provider management (dynamic add/remove/configure AI providers)
	mux.HandleFunc("POST /v1/admin/llm/providers", RequireAdmin(svc.handleCreateLLMProvider))
	mux.HandleFunc("GET /v1/admin/llm/providers", RequireAdmin(svc.handleListLLMProviders))
	mux.HandleFunc("GET /v1/admin/llm/providers/{id}", RequireAdmin(svc.handleGetLLMProvider))
	mux.HandleFunc("PUT /v1/admin/llm/providers/{id}", RequireAdmin(svc.handleUpdateLLMProvider))
	mux.HandleFunc("DELETE /v1/admin/llm/providers/{id}", RequireAdmin(svc.handleDeleteLLMProvider))
	mux.HandleFunc("POST /v1/admin/llm/providers/{id}/test", RequireAdmin(svc.handleTestLLMProvider))
	mux.HandleFunc("GET /v1/admin/llm/models", RequireAdmin(svc.handleListLLMModels))
}

// RequireAuth wraps a handler to require a valid JWT; returns 401 if missing.
func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.ClaimsFromContext(r.Context()); !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

// RequireAdmin wraps a handler to require platform_admin or admin role.
// Note: "owner" is a tenant-level role (owns their org) and does NOT grant
// platform admin access. Only platform_admin and admin can access admin endpoints.
func RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromReq(r)
		switch claims.Role {
		case "platform_admin", "admin":
			next(w, r)
		default:
			writeError(w, http.StatusForbidden, "admin role required")
		}
	})
}

// writeJSON encodes v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Nothing we can do at this point — headers already sent.
		_ = err
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decode decodes the request body into v; returns false and writes 400 on error.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return false
	}
	return true
}

// claimsFromReq extracts claims — panics if RequireAuth wasn't used.
func claimsFromReq(r *http.Request) *auth.Claims {
	c, _ := auth.ClaimsFromContext(r.Context())
	return c
}

// isConflict is a loose check for Postgres duplicate key errors.
func isConflict(err error) bool {
	return err != nil && errors.Is(err, errors.New("duplicate key")) ||
		containsStr(err, "duplicate key") || containsStr(err, "unique constraint")
}

func containsStr(err error, s string) bool {
	if err == nil {
		return false
	}
	return len(err.Error()) > 0 && stringContains(err.Error(), s)
}

// resolveVolundUserID maps a better-auth nanoid user ID to the Volund UUID
// via the ba_user_map bridge table. Falls back to the input if no mapping exists
// (for backward compatibility with legacy HS256 JWTs that already use UUIDs).
func (s *Services) resolveVolundUserID(ctx context.Context, baUserID string) string {
	if s.Pool == nil {
		return baUserID
	}
	var volundID string
	err := s.Pool.QueryRow(ctx,
		`SELECT volund_user_id::text FROM ba_user_map WHERE ba_user_id = $1`,
		baUserID).Scan(&volundID)
	if err != nil {
		return baUserID // Not found — assume it's already a UUID (legacy token).
	}
	return volundID
}

func stringContains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		func() bool {
			for i := 0; i <= len(haystack)-len(needle); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		}())
}
