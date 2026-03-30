package api

import (
	"context"
	"net/http"
	"time"
)

// GET /v1/admin/health — comprehensive platform health check.
func (s *Services) handlePlatformHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	health := map[string]any{}

	// Postgres
	if s.Pool != nil {
		if err := s.Pool.Ping(ctx); err != nil {
			health["postgres"] = map[string]string{"status": "error", "error": err.Error()}
		} else {
			health["postgres"] = map[string]string{"status": "ok"}
		}
	} else {
		health["postgres"] = map[string]string{"status": "not configured"}
	}

	// Warm pool
	if s.Agents != nil {
		stats, err := s.Agents.GetWarmPoolStats(ctx)
		if err != nil {
			health["warm_pool"] = map[string]string{"status": "error", "error": err.Error()}
		} else {
			health["warm_pool"] = map[string]any{
				"status":    "ok",
				"total":     stats.Total,
				"available": stats.Available,
				"claimed":   stats.Claimed,
				"active":    stats.Active,
			}
		}
	}

	// Auth service
	authURL := "http://volund-auth:3456"
	authCtx, authCancel := context.WithTimeout(ctx, 2*time.Second)
	defer authCancel()
	req, _ := http.NewRequestWithContext(authCtx, "GET", authURL+"/healthz", nil)
	if resp, err := http.DefaultClient.Do(req); err != nil {
		health["auth"] = map[string]string{"status": "error", "error": err.Error()}
	} else {
		resp.Body.Close()
		if resp.StatusCode == 200 {
			health["auth"] = map[string]string{"status": "ok"}
		} else {
			health["auth"] = map[string]string{"status": "error", "error": resp.Status}
		}
	}

	// Gateway
	health["gateway"] = map[string]string{"status": "ok"}

	writeJSON(w, http.StatusOK, health)
}
