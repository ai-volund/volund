package api

import (
	"net/http"
	"strconv"
	"time"
)

// GET /v1/admin/audit — list recent audit log entries.
func (s *Services) handleListAudit(w http.ResponseWriter, r *http.Request) {
	if s.Audit == nil {
		writeError(w, http.StatusServiceUnavailable, "audit log not available")
		return
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			limit = n
		}
	}
	tenantID := r.URL.Query().Get("tenant_id")

	entries, err := s.Audit.List(r.Context(), limit, tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list audit: "+err.Error())
		return
	}

	out := make([]map[string]any, len(entries))
	for i, e := range entries {
		out[i] = map[string]any{
			"id":          e.ID,
			"tenant_id":   e.TenantID,
			"user_id":     e.UserID,
			"action":      e.Action,
			"resource":    e.Resource,
			"resource_id": e.ResourceID,
			"detail":      e.Detail,
			"ip_address":  e.IPAddress,
			"user_agent":  e.UserAgent,
			"created_at":  e.CreatedAt.Format(time.RFC3339),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}
