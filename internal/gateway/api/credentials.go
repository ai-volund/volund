package api

import (
	"net/http"
	"time"

	"github.com/ai-volund/volund/internal/credentials"
)

// handleStoreCredential stores an encrypted credential for the authenticated user.
// POST /v1/credentials
func (s *Services) handleStoreCredential(w http.ResponseWriter, r *http.Request) {
	if s.CredBroker == nil {
		writeError(w, http.StatusServiceUnavailable, "credential broker not available")
		return
	}

	claims := claimsFromReq(r)

	var in struct {
		Provider     string   `json:"provider"`
		Token        string   `json:"token"`
		RefreshToken string   `json:"refresh_token,omitempty"`
		Scopes       []string `json:"scopes"`
		TokenType    string   `json:"token_type"`
		ExpiresAt    *string  `json:"expires_at,omitempty"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Provider == "" || in.Token == "" {
		writeError(w, http.StatusBadRequest, "provider and token are required")
		return
	}
	if in.TokenType == "" {
		in.TokenType = "oauth2"
	}
	if in.Scopes == nil {
		in.Scopes = []string{}
	}

	var expiresAt *time.Time
	if in.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *in.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid expires_at: use RFC3339 format")
			return
		}
		expiresAt = &t
	}

	err := s.CredBroker.StoreCredential(r.Context(), credentials.StoreRequest{
		TenantID:     claims.TenantID,
		UserID:       claims.Subject,
		Provider:     in.Provider,
		Token:        in.Token,
		RefreshToken: in.RefreshToken,
		Scopes:       in.Scopes,
		TokenType:    in.TokenType,
		ExpiresAt:    expiresAt,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store credential: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"status": "stored", "provider": in.Provider})
}

// handleListCredentials returns connected providers for the authenticated user.
// GET /v1/credentials
func (s *Services) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	if s.CredBroker == nil {
		writeError(w, http.StatusServiceUnavailable, "credential broker not available")
		return
	}

	claims := claimsFromReq(r)
	providers, err := s.CredBroker.ListProviders(r.Context(), claims.TenantID, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list credentials: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, providers)
}

// handleDeleteCredential removes a stored credential.
// DELETE /v1/credentials/{provider}
func (s *Services) handleDeleteCredential(w http.ResponseWriter, r *http.Request) {
	if s.CredBroker == nil {
		writeError(w, http.StatusServiceUnavailable, "credential broker not available")
		return
	}

	claims := claimsFromReq(r)
	provider := r.PathValue("provider")

	if err := s.CredBroker.DeleteCredential(r.Context(), claims.TenantID, claims.Subject, provider); err != nil {
		writeError(w, http.StatusNotFound, "credential not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleIssueToken issues a short-lived scoped token for an agent.
// This is called by agents, not by users directly.
// POST /v1/credentials/token
func (s *Services) handleIssueToken(w http.ResponseWriter, r *http.Request) {
	if s.CredBroker == nil {
		writeError(w, http.StatusServiceUnavailable, "credential broker not available")
		return
	}

	claims := claimsFromReq(r)

	var in struct {
		Provider        string   `json:"provider"`
		UserID          string   `json:"user_id"`
		Scopes          []string `json:"scopes"`
		AgentInstanceID string   `json:"agent_instance_id"`
		ConversationID  string   `json:"conversation_id"`
		SkillName       string   `json:"skill_name"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Provider == "" {
		writeError(w, http.StatusBadRequest, "provider is required")
		return
	}
	if in.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}

	resp, err := s.CredBroker.IssueToken(r.Context(), credentials.TokenRequest{
		TenantID:        claims.TenantID,
		UserID:          in.UserID,
		Provider:        in.Provider,
		Scopes:          in.Scopes,
		AgentInstanceID: in.AgentInstanceID,
		ConversationID:  in.ConversationID,
		SkillName:       in.SkillName,
	})
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleCredentialAudit returns the audit log for the tenant.
// GET /v1/credentials/audit
func (s *Services) handleCredentialAudit(w http.ResponseWriter, r *http.Request) {
	if s.Credentials == nil {
		writeError(w, http.StatusServiceUnavailable, "credential broker not available")
		return
	}

	claims := claimsFromReq(r)
	entries, err := s.Credentials.ListAuditLog(r.Context(), claims.TenantID, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "audit log: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, entries)
}
