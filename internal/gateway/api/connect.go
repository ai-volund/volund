package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/ai-volund/volund/internal/credentials"
)

// ── OAuth connect flow (user-facing) ─────────────────────────────────────────

// GET /v1/connect/{provider} — redirects user to the provider's OAuth consent screen.
func (s *Services) handleConnectStart(w http.ResponseWriter, r *http.Request) {
	if s.OAuth == nil {
		writeError(w, http.StatusServiceUnavailable, "OAuth engine not available")
		return
	}

	claims := claimsFromReq(r)
	providerID := r.PathValue("provider")

	authURL, err := s.OAuth.StartAuth(r.Context(), providerID, claims.TenantID, s.resolveVolundUserID(r.Context(), claims.Subject), nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("provider %q: %v", providerID, err))
		return
	}

	// Desktop apps can't follow redirects from fetch(), so they pass ?format=json
	// to get the auth URL back as JSON instead of a 307 redirect.
	if r.URL.Query().Get("format") == "json" {
		writeJSON(w, http.StatusOK, map[string]string{"auth_url": authURL})
		return
	}

	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// GET /v1/connect/{provider}/callback — handles OAuth callback, stores tokens.
func (s *Services) handleConnectCallback(w http.ResponseWriter, r *http.Request) {
	if s.OAuth == nil {
		writeError(w, http.StatusServiceUnavailable, "OAuth engine not available")
		return
	}

	// Check for OAuth error response.
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		desc := r.URL.Query().Get("error_description")
		writeError(w, http.StatusBadRequest, fmt.Sprintf("OAuth error: %s — %s", errMsg, desc))
		return
	}

	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" || code == "" {
		writeError(w, http.StatusBadRequest, "missing state or code parameter")
		return
	}

	result, err := s.OAuth.HandleCallback(r.Context(), state, code)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Calculate expiry time.
	var expiresAt *time.Time
	if result.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
		expiresAt = &t
	}

	// Store the tokens in the credential broker.
	err = s.CredBroker.StoreCredential(r.Context(), credentials.StoreRequest{
		TenantID:     result.TenantID,
		UserID:       result.UserID,
		Provider:     result.ProviderID,
		Token:        result.AccessToken,
		RefreshToken: result.RefreshToken,
		Scopes:       result.Scopes,
		TokenType:    "oauth2",
		ExpiresAt:    expiresAt,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store credential: "+err.Error())
		return
	}

	// Return a simple success page that the user can close.
	// The desktop app polls /v1/connect to detect the new connection.
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><title>Connected</title>
<style>body{font-family:system-ui;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;background:#0a0a0a;color:#fafafa}
.card{text-align:center;padding:3rem;border:1px solid #333;border-radius:1rem;max-width:400px}
h1{font-size:1.5rem;margin:0 0 .5rem}p{color:#888;margin:0 0 1.5rem;font-size:.9rem}
.check{font-size:3rem;margin-bottom:1rem}</style></head>
<body><div class="card"><div class="check">&#10003;</div><h1>%s Connected</h1>
<p>You can close this tab and return to the app.</p></div></body></html>`, result.ProviderID)
}

// GET /v1/connect — lists available providers and user's connection status.
// Only shows providers that the admin has configured (supplied credentials for).
func (s *Services) handleConnectList(w http.ResponseWriter, r *http.Request) {
	if s.OAuth == nil {
		writeError(w, http.StatusServiceUnavailable, "OAuth engine not available")
		return
	}

	claims := claimsFromReq(r)

	// Only show providers that are configured (admin has supplied credentials).
	providerConfigs, err := s.OAuth.Registry.ListConfigured(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list providers: "+err.Error())
		return
	}

	// Get user's connected providers.
	connected, err := s.CredBroker.ListProviders(r.Context(), claims.TenantID, s.resolveVolundUserID(r.Context(), claims.Subject))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list connections: "+err.Error())
		return
	}

	// Build a set of connected provider IDs.
	connectedSet := make(map[string]bool)
	for _, c := range connected {
		connectedSet[c.Provider] = true
	}

	// Build response.
	var out []map[string]any
	for _, p := range providerConfigs {
		out = append(out, map[string]any{
			"id":           p.ID,
			"display_name": p.DisplayName,
			"category":     p.Category,
			"icon_url":     p.IconURL,
			"scopes":       p.Scopes,
			"connected":    connectedSet[p.ID],
			"connect_url":  fmt.Sprintf("/v1/connect/%s", p.ID),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"providers": out})
}

// ── Admin provider management ────────────────────────────────────────────────

func isAdmin(r *http.Request) bool {
	claims := claimsFromReq(r)
	return claims.Role == "admin" || claims.Role == "platform_admin" || claims.Role == "owner"
}

// PUT /v1/admin/providers/{id}/credentials — admin supplies client_id + client_secret
// for a provider that was registered by a skill manifest. This is the ONLY admin
// action needed to enable a provider. Everything else comes from the skill.
func (s *Services) handleSetProviderCredentials(w http.ResponseWriter, r *http.Request) {
	if s.OAuth == nil {
		writeError(w, http.StatusServiceUnavailable, "OAuth engine not available")
		return
	}
	if !isAdmin(r) {
		writeError(w, http.StatusForbidden, "admin role required")
		return
	}

	id := r.PathValue("id")

	var in struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.ClientID == "" || in.ClientSecret == "" {
		writeError(w, http.StatusBadRequest, "client_id and client_secret are required")
		return
	}

	if err := s.OAuth.Registry.SetCredentials(r.Context(), id, in.ClientID, in.ClientSecret); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "configured", "provider": id})
}

// GET /v1/admin/providers — lists ALL providers (including unconfigured ones).
// Admin sees which providers need credentials.
func (s *Services) handleListProviders(w http.ResponseWriter, r *http.Request) {
	if s.OAuth == nil {
		writeError(w, http.StatusServiceUnavailable, "OAuth engine not available")
		return
	}
	if !isAdmin(r) {
		writeError(w, http.StatusForbidden, "admin role required")
		return
	}

	providerConfigs, err := s.OAuth.Registry.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list providers: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"providers": providerConfigs})
}

// DELETE /v1/admin/providers/{id} — removes a registered provider.
func (s *Services) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	if s.OAuth == nil {
		writeError(w, http.StatusServiceUnavailable, "OAuth engine not available")
		return
	}
	if !isAdmin(r) {
		writeError(w, http.StatusForbidden, "admin role required")
		return
	}

	id := r.PathValue("id")
	if err := s.OAuth.Registry.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
