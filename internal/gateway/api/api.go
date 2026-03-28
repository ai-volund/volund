// Package api contains the HTTP REST API handlers for the Volund gateway.
// Routes follow /v1/{resource} conventions and return JSON.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ai-volund/volund/internal/auth"
	"github.com/ai-volund/volund/internal/db"
)

// Services holds all dependencies the API handlers need.
type Services struct {
	Auth    *auth.Service
	TM      *auth.TokenManager
	Users   *db.UserRepo
	Tenants *db.TenantRepo
	Agents  *db.AgentRepo
	Convos  *db.ConversationRepo
	Invites *db.InviteRepo
}

// Register mounts all API routes on mux under /v1/.
func Register(mux *http.ServeMux, svc *Services) {
	// Auth
	mux.HandleFunc("POST /v1/auth/register", svc.handleRegister)
	mux.HandleFunc("POST /v1/auth/login", svc.handleLogin)
	mux.HandleFunc("POST /v1/auth/refresh", svc.handleRefresh)
	mux.HandleFunc("POST /v1/auth/logout", svc.handleLogout)
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

	// Agent profiles
	mux.HandleFunc("POST /v1/agents", RequireAuth(svc.handleCreateAgent))
	mux.HandleFunc("GET /v1/agents", RequireAuth(svc.handleListAgents))
	mux.HandleFunc("GET /v1/agents/{id}", RequireAuth(svc.handleGetAgent))
	mux.HandleFunc("PUT /v1/agents/{id}", RequireAuth(svc.handleUpdateAgent))
	mux.HandleFunc("DELETE /v1/agents/{id}", RequireAuth(svc.handleDeleteAgent))

	// Conversations
	mux.HandleFunc("POST /v1/conversations", RequireAuth(svc.handleCreateConversation))
	mux.HandleFunc("GET /v1/conversations", RequireAuth(svc.handleListConversations))
	mux.HandleFunc("GET /v1/conversations/{id}", RequireAuth(svc.handleGetConversation))
	mux.HandleFunc("DELETE /v1/conversations/{id}", RequireAuth(svc.handleDeleteConversation))
	mux.HandleFunc("POST /v1/conversations/{id}/messages", RequireAuth(svc.handlePostMessage))
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
