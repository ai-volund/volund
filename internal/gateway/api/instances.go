package api

import (
	"net/http"
	"time"
)

// GET /v1/admin/instances — list all agent instances across tenants.
func (s *Services) handleListInstances(w http.ResponseWriter, r *http.Request) {
	instances, err := s.Agents.ListAllInstances(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list instances: "+err.Error())
		return
	}

	out := make([]map[string]any, len(instances))
	for i, inst := range instances {
		m := map[string]any{
			"id":         inst.ID,
			"tenant_id":  inst.TenantID,
			"state":      inst.State,
			"created_at": inst.CreatedAt.Format(time.RFC3339),
		}
		if inst.ProfileID != nil {
			m["profile_id"] = *inst.ProfileID
		}
		if inst.PodName != nil {
			m["pod_name"] = *inst.PodName
		}
		if inst.PodNamespace != nil {
			m["pod_namespace"] = *inst.PodNamespace
		}
		if inst.ClaimedAt != nil {
			m["claimed_at"] = inst.ClaimedAt.Format(time.RFC3339)
		}
		if inst.LastHeartbeat != nil {
			m["last_heartbeat"] = inst.LastHeartbeat.Format(time.RFC3339)
		}
		out[i] = m
	}

	writeJSON(w, http.StatusOK, map[string]any{"instances": out})
}

// GET /v1/admin/warmpool — warm pool statistics.
func (s *Services) handleWarmPoolStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.Agents.GetWarmPoolStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "warm pool stats: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// DELETE /v1/admin/instances/{id} — force-release an instance.
func (s *Services) handleForceReleaseInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Agents.ForceRelease(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "force release: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "released", "id": id})
}
