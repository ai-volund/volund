package api

import (
	"net/http"
	"time"

	"github.com/ai-volund/volund/internal/db"
)

// PUT /v1/admin/tenants/{id}/quota — set or update a tenant's quota.
func (s *Services) handleSetTenantQuota(w http.ResponseWriter, r *http.Request) {
	tenantID := r.PathValue("id")

	var in struct {
		MaxTokens     *int64   `json:"max_tokens"`
		MaxCostUSD    *float64 `json:"max_cost_usd"`
		OnLimitAction string   `json:"on_limit_action"` // "block" or "warn"
	}
	if !decode(w, r, &in) {
		return
	}

	if in.OnLimitAction == "" {
		in.OnLimitAction = "block"
	}

	now := time.Now()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)

	q := &db.TenantQuota{
		TenantID:      tenantID,
		MaxTokens:     in.MaxTokens,
		MaxCostUSD:    in.MaxCostUSD,
		PeriodStart:   periodStart,
		PeriodEnd:     periodEnd,
		OnLimitAction: in.OnLimitAction,
	}

	if err := s.Quotas.Upsert(r.Context(), q); err != nil {
		writeError(w, http.StatusInternalServerError, "set quota: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "updated",
		"tenant_id":       tenantID,
		"max_tokens":      in.MaxTokens,
		"max_cost_usd":    in.MaxCostUSD,
		"on_limit_action": in.OnLimitAction,
		"period_start":    periodStart.Format(time.RFC3339),
		"period_end":      periodEnd.Format(time.RFC3339),
	})
}

// GET /v1/admin/tenants/{id}/quota — get a tenant's quota and current usage.
func (s *Services) handleGetTenantQuota(w http.ResponseWriter, r *http.Request) {
	tenantID := r.PathValue("id")

	status, err := s.Quotas.CheckQuota(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "check quota: "+err.Error())
		return
	}
	if status == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"tenant_id": tenantID,
			"quota":     nil,
			"message":   "no quota configured — unlimited",
		})
		return
	}

	writeJSON(w, http.StatusOK, status)
}
