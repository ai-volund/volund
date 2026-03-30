package api

import (
	"net/http"
)

// GET /v1/auth/me — returns the current user's identity from the JWT claims.
// This endpoint remains even after migrating auth to better-auth,
// since it's useful for clients to verify their JWT and get user info.
func (s *Services) handleMe(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	user, err := s.Users.GetByID(r.Context(), claims.Subject)
	if err != nil {
		// If user not found in DB (e.g. better-auth nanoid → ba_user_map bridge),
		// return what we have from the JWT claims.
		writeJSON(w, http.StatusOK, map[string]any{
			"id":        claims.Subject,
			"tenant_id": claims.TenantID,
			"role":      claims.Role,
		})
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
