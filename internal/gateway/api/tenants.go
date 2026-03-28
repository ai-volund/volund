package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/ai-volund/volund/internal/auth"
	"github.com/ai-volund/volund/internal/db"
)

// POST /v1/tenants
func (s *Services) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	if claims.Role != "owner" && claims.Role != "platform_admin" {
		writeError(w, http.StatusForbidden, "only owners can create tenants")
		return
	}

	var in struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
		Plan string `json:"plan"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Slug == "" {
		in.Slug = slugify(in.Name)
	}
	if in.Plan == "" {
		in.Plan = "free"
	}

	tenant, err := s.Tenants.Create(r.Context(), in.Name, in.Slug, in.Plan)
	if err != nil {
		if isConflict(err) {
			writeError(w, http.StatusConflict, "slug already taken")
			return
		}
		writeError(w, http.StatusInternalServerError, "create tenant failed")
		return
	}
	writeJSON(w, http.StatusCreated, tenantJSON(tenant))
}

// GET /v1/tenants
func (s *Services) handleListTenants(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)

	// Non-admins only see their own tenant.
	if claims.Role != "platform_admin" {
		tenant, err := s.Tenants.GetByID(r.Context(), claims.TenantID)
		if err != nil {
			writeError(w, http.StatusNotFound, "tenant not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tenants": []any{tenantJSON(tenant)}})
		return
	}

	tenants, err := s.Tenants.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list failed")
		return
	}
	out := make([]any, len(tenants))
	for i, t := range tenants {
		out[i] = tenantJSON(t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tenants": out})
}

// GET /v1/tenants/{id}
func (s *Services) handleGetTenant(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	id := r.PathValue("id")
	if claims.Role != "platform_admin" && claims.TenantID != id {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	tenant, err := s.Tenants.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "tenant not found")
		return
	}
	writeJSON(w, http.StatusOK, tenantJSON(tenant))
}

// PUT /v1/tenants/{id}
func (s *Services) handleUpdateTenant(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	id := r.PathValue("id")
	if claims.Role != "platform_admin" && (claims.TenantID != id || claims.Role != "owner") {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var in struct {
		Name string `json:"name"`
		Plan string `json:"plan"`
	}
	if !decode(w, r, &in) {
		return
	}

	tenant, err := s.Tenants.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "tenant not found")
		return
	}
	if in.Name != "" {
		tenant.Name = in.Name
	}
	if in.Plan != "" {
		tenant.Plan = in.Plan
	}
	updated, err := s.Tenants.Update(r.Context(), tenant)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, tenantJSON(updated))
}

// DELETE /v1/tenants/{id}
func (s *Services) handleDeleteTenant(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	if claims.Role != "platform_admin" {
		writeError(w, http.StatusForbidden, "only platform admins can delete tenants")
		return
	}
	id := r.PathValue("id")
	if err := s.Tenants.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// GET /v1/tenants/{id}/members
func (s *Services) handleListMembers(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	id := r.PathValue("id")
	if claims.TenantID != id && claims.Role != "platform_admin" {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	users, err := s.Users.ListByTenant(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list failed")
		return
	}
	out := make([]any, len(users))
	for i, u := range users {
		out[i] = map[string]any{
			"id":           u.ID,
			"email":        u.Email,
			"display_name": u.DisplayName,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": out})
}

// POST /v1/tenants/{id}/invites
func (s *Services) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	tenantID := r.PathValue("id")
	if claims.TenantID != tenantID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if claims.Role != "owner" && claims.Role != "admin" {
		writeError(w, http.StatusForbidden, "only admins can send invites")
		return
	}

	var in struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Role == "" {
		in.Role = "member"
	}

	token, err := auth.GenerateToken(24)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "generate token failed")
		return
	}

	inviterID := claims.Subject
	invite, err := s.Invites.Create(r.Context(), tenantID, in.Email, in.Role, token, 7*24*time.Hour, &inviterID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create invite failed")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         invite.ID,
		"email":      invite.Email,
		"role":       invite.Role,
		"token":      invite.Token, // caller sends this to the invitee
		"expires_at": invite.ExpiresAt.Format(time.RFC3339),
	})
}

// POST /v1/invites/{token}/accept
func (s *Services) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")

	// Must be logged in to accept.
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		// Also support new user registration via invite — require an account body.
		var in struct {
			Email    string `json:"email"`
			Password string `json:"password"`
			Name     string `json:"display_name"`
		}
		if !decode(w, r, &in) {
			return
		}

		invite, err := s.Invites.GetByToken(r.Context(), token)
		if err != nil || invite == nil {
			writeError(w, http.StatusNotFound, "invite not found or expired")
			return
		}

		// Create the user and add to the tenant from the invite.
		pair, user, err := s.Auth.Register(r.Context(), auth.RegisterInput{
			Email:    in.Email,
			Password: in.Password,
			OrgName:  "", // don't create a new org
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.Users.AddToTenant(r.Context(), invite.TenantID, user.ID, invite.Role); err != nil {
			writeError(w, http.StatusInternalServerError, "join tenant failed")
			return
		}
		_ = s.Invites.Accept(r.Context(), invite.ID)
		writeJSON(w, http.StatusOK, tokenResponse(pair, user.ID, user.Email, user.DisplayName))
		return
	}

	// Logged-in user accepting.
	invite, err := s.Invites.GetByToken(r.Context(), token)
	if err != nil || invite == nil {
		writeError(w, http.StatusNotFound, "invite not found or expired")
		return
	}
	if err := s.Users.AddToTenant(r.Context(), invite.TenantID, claims.Subject, invite.Role); err != nil {
		writeError(w, http.StatusInternalServerError, "join tenant failed")
		return
	}
	_ = s.Invites.Accept(r.Context(), invite.ID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted", "tenant_id": invite.TenantID})
}

// tenantJSON converts a db.Tenant to a map for the wire format.
func tenantJSON(t *db.Tenant) map[string]any {
	return map[string]any{
		"id":         t.ID,
		"name":       t.Name,
		"slug":       t.Slug,
		"plan":       t.Plan,
		"created_at": t.CreatedAt.Format(time.RFC3339),
	}
}

// slugify converts a tenant name to a URL-safe slug (also defined in auth/service.go but that's a different package).
func slugify(name string) string {
	s := strings.ToLower(name)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '_' {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
