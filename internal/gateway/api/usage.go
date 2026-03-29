package api

import (
	"net/http"
	"time"
)

// GET /v1/usage/summary — aggregate usage for the authenticated tenant.
func (s *Services) handleUsageSummary(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)

	// Default: last 30 days.
	to := time.Now()
	from := to.AddDate(0, 0, -30)

	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			to = t
		}
	}

	if s.Usage == nil {
		writeError(w, http.StatusServiceUnavailable, "usage tracking not available")
		return
	}

	summary, err := s.Usage.SummaryByTenant(r.Context(), claims.TenantID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "usage query failed")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// GET /v1/usage/breakdown — per-model usage breakdown for the authenticated tenant.
func (s *Services) handleUsageBreakdown(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)

	to := time.Now()
	from := to.AddDate(0, 0, -30)

	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			to = t
		}
	}

	if s.Usage == nil {
		writeError(w, http.StatusServiceUnavailable, "usage tracking not available")
		return
	}

	breakdown, err := s.Usage.BreakdownByModel(r.Context(), claims.TenantID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "usage breakdown failed")
		return
	}
	writeJSON(w, http.StatusOK, breakdown)
}

// GET /v1/usage/quota — current quota status for the authenticated tenant.
func (s *Services) handleQuotaStatus(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)

	if s.Quotas == nil {
		writeError(w, http.StatusServiceUnavailable, "quota tracking not available")
		return
	}

	status, err := s.Quotas.CheckQuota(r.Context(), claims.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "quota check failed")
		return
	}
	if status == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "unlimited"})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// GET /v1/usage/conversations/{id} — usage for a specific conversation.
func (s *Services) handleUsageByConversation(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	convID := r.PathValue("id")

	// Verify the conversation belongs to the tenant.
	convo, err := s.Convos.Get(r.Context(), convID)
	if err != nil {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	if convo.TenantID != claims.TenantID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	if s.Usage == nil {
		writeError(w, http.StatusServiceUnavailable, "usage tracking not available")
		return
	}

	summary, err := s.Usage.SummaryByConversation(r.Context(), convID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "usage query failed")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}
