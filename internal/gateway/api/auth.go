package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/ai-volund/volund/internal/auth"
)

// POST /v1/auth/register
func (s *Services) handleRegister(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		OrgName     string `json:"org_name"`
	}
	if !decode(w, r, &in) {
		return
	}

	pair, user, err := s.Auth.Register(r.Context(), auth.RegisterInput{
		Email:       in.Email,
		Password:    in.Password,
		DisplayName: in.DisplayName,
		OrgName:     in.OrgName,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, tokenResponse(pair, user.ID, user.Email, user.DisplayName))
}

// POST /v1/auth/login
func (s *Services) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}

	pair, user, err := s.Auth.Login(r.Context(), in.Email, in.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, tokenResponse(pair, user.ID, user.Email, user.DisplayName))
}

// POST /v1/auth/refresh
func (s *Services) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !decode(w, r, &in) {
		return
	}

	pair, err := s.Auth.RefreshToken(r.Context(), in.RefreshToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired refresh token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  pair.AccessToken,
		"refresh_token": pair.RefreshToken,
		"expires_at":    pair.ExpiresAt.Format(time.RFC3339),
	})
}

// POST /v1/auth/logout
func (s *Services) handleLogout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !decode(w, r, &in) {
		return
	}

	_ = s.Auth.Logout(r.Context(), in.RefreshToken)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GET /v1/auth/me
func (s *Services) handleMe(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	user, err := s.Users.GetByID(r.Context(), claims.Subject)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":           user.ID,
		"email":        user.Email,
		"display_name": user.DisplayName,
		"tenant_id":    claims.TenantID,
		"role":         claims.Role,
	})
}

// GET /v1/auth/oidc/providers — list configured OIDC providers.
func (s *Services) handleOIDCProviders(w http.ResponseWriter, r *http.Request) {
	if s.OIDCMgr == nil {
		writeJSON(w, http.StatusOK, map[string]any{"providers": []string{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": s.OIDCMgr.Providers()})
}

// GET /v1/auth/oidc/{provider} — redirect to provider's authorization URL.
func (s *Services) handleOIDCRedirect(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if s.OIDCMgr == nil || !s.OIDCMgr.HasProvider(provider) {
		writeError(w, http.StatusNotFound, "unknown OIDC provider")
		return
	}

	authURL, _, err := s.OIDCMgr.AuthURL(provider)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate auth URL")
		return
	}

	http.Redirect(w, r, authURL, http.StatusFound)
}

// GET /v1/auth/oidc/{provider}/callback — handle OIDC callback, issue tokens.
func (s *Services) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if s.OIDCMgr == nil || !s.OIDCMgr.HasProvider(provider) {
		writeError(w, http.StatusNotFound, "unknown OIDC provider")
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		writeError(w, http.StatusBadRequest, "missing code or state parameter")
		return
	}

	claims, err := s.OIDCMgr.Exchange(r.Context(), provider, code, state)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "OIDC authentication failed: "+err.Error())
		return
	}

	pair, user, err := s.Auth.LoginOIDC(r.Context(), claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "login failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, tokenResponse(pair, user.ID, user.Email, user.DisplayName))
}

// tokenResponse builds the standard auth response body.
func tokenResponse(pair *auth.TokenPair, userID, email, displayName string) map[string]any {
	return map[string]any{
		"access_token":  pair.AccessToken,
		"refresh_token": pair.RefreshToken,
		"expires_at":    pair.ExpiresAt.Format(time.RFC3339),
		"user": map[string]any{
			"id":           userID,
			"email":        email,
			"display_name": displayName,
		},
	}
}
