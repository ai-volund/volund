package api

import (
	"net/http"
)

// PUT /v1/admin/tenants/{id}/members/{userId}/role — change a user's role within a tenant.
func (s *Services) handleUpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	tenantID := r.PathValue("id")
	userID := r.PathValue("userId")

	var in struct {
		Role string `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}

	switch in.Role {
	case "platform_admin", "admin", "owner", "member":
		// valid
	default:
		writeError(w, http.StatusBadRequest, "invalid role — must be platform_admin, admin, owner, or member")
		return
	}

	if err := s.Users.UpdateRole(r.Context(), tenantID, userID, in.Role); err != nil {
		writeError(w, http.StatusInternalServerError, "update role: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":    "updated",
		"tenant_id": tenantID,
		"user_id":   userID,
		"role":      in.Role,
	})
}

// DELETE /v1/admin/tenants/{id}/members/{userId} — remove a user from a tenant.
func (s *Services) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	tenantID := r.PathValue("id")
	userID := r.PathValue("userId")

	if err := s.Users.RemoveFromTenant(r.Context(), tenantID, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "remove member: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}
