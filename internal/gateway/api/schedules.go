package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ai-volund/volund/internal/db"
)

// POST /v1/schedules — create a scheduled workflow.
func (s *Services) handleCreateSchedule(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	userID := s.resolveVolundUserID(r.Context(), claims.Subject)

	var in struct {
		Name            string          `json:"name"`
		CronExpression  string          `json:"cron_expression"`
		AgentProfileID  *string         `json:"agent_profile_id"`
		Prompt          string          `json:"prompt"`
		DeliveryMethod  string          `json:"delivery_method"`
		DeliveryConfig  json.RawMessage `json:"delivery_config"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Name == "" || in.CronExpression == "" || in.Prompt == "" {
		writeError(w, http.StatusBadRequest, "name, cron_expression, and prompt are required")
		return
	}
	if in.DeliveryMethod == "" {
		in.DeliveryMethod = "conversation"
	}

	sched := &db.Schedule{
		TenantID:       claims.TenantID,
		UserID:         userID,
		Name:           in.Name,
		CronExpression: in.CronExpression,
		AgentProfileID: in.AgentProfileID,
		Prompt:         in.Prompt,
		DeliveryMethod: in.DeliveryMethod,
		DeliveryConfig: in.DeliveryConfig,
		Enabled:        true,
	}

	created, err := s.Schedules.Create(r.Context(), sched)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create schedule: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, scheduleJSON(created))
}

// GET /v1/schedules — list schedules for the current user.
func (s *Services) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	userID := s.resolveVolundUserID(r.Context(), claims.Subject)

	schedules, err := s.Schedules.ListByUser(r.Context(), claims.TenantID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list schedules: "+err.Error())
		return
	}

	out := make([]map[string]any, len(schedules))
	for i, sc := range schedules {
		out[i] = scheduleJSON(sc)
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedules": out})
}

// GET /v1/schedules/{id} — get a schedule.
func (s *Services) handleGetSchedule(w http.ResponseWriter, r *http.Request) {
	sc, err := s.Schedules.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}
	writeJSON(w, http.StatusOK, scheduleJSON(sc))
}

// PUT /v1/schedules/{id} — update a schedule.
func (s *Services) handleUpdateSchedule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.Schedules.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "schedule not found")
		return
	}

	var in struct {
		Name            *string          `json:"name"`
		CronExpression  *string          `json:"cron_expression"`
		AgentProfileID  *string          `json:"agent_profile_id"`
		Prompt          *string          `json:"prompt"`
		DeliveryMethod  *string          `json:"delivery_method"`
		DeliveryConfig  json.RawMessage  `json:"delivery_config"`
		Enabled         *bool            `json:"enabled"`
	}
	if !decode(w, r, &in) {
		return
	}

	if in.Name != nil { existing.Name = *in.Name }
	if in.CronExpression != nil { existing.CronExpression = *in.CronExpression }
	if in.AgentProfileID != nil { existing.AgentProfileID = in.AgentProfileID }
	if in.Prompt != nil { existing.Prompt = *in.Prompt }
	if in.DeliveryMethod != nil { existing.DeliveryMethod = *in.DeliveryMethod }
	if in.DeliveryConfig != nil { existing.DeliveryConfig = in.DeliveryConfig }
	if in.Enabled != nil { existing.Enabled = *in.Enabled }

	updated, err := s.Schedules.Update(r.Context(), existing)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update schedule: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, scheduleJSON(updated))
}

// DELETE /v1/schedules/{id} — delete a schedule.
func (s *Services) handleDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	if err := s.Schedules.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, "delete schedule: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// GET /v1/admin/schedules — list all schedules across tenants (admin only).
func (s *Services) handleListAllSchedules(w http.ResponseWriter, r *http.Request) {
	schedules, err := s.Schedules.ListAll(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list all schedules: "+err.Error())
		return
	}
	out := make([]map[string]any, len(schedules))
	for i, sc := range schedules {
		out[i] = scheduleJSON(sc)
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedules": out})
}

func scheduleJSON(s *db.Schedule) map[string]any {
	m := map[string]any{
		"id":              s.ID,
		"tenant_id":       s.TenantID,
		"user_id":         s.UserID,
		"name":            s.Name,
		"cron_expression": s.CronExpression,
		"prompt":          s.Prompt,
		"delivery_method": s.DeliveryMethod,
		"delivery_config": s.DeliveryConfig,
		"enabled":         s.Enabled,
		"run_count":       s.RunCount,
		"created_at":      s.CreatedAt.Format(time.RFC3339),
		"updated_at":      s.UpdatedAt.Format(time.RFC3339),
	}
	if s.AgentProfileID != nil { m["agent_profile_id"] = *s.AgentProfileID }
	if s.LastRunAt != nil { m["last_run_at"] = s.LastRunAt.Format(time.RFC3339) }
	if s.NextRunAt != nil { m["next_run_at"] = s.NextRunAt.Format(time.RFC3339) }
	if s.LastRunStatus != nil { m["last_run_status"] = *s.LastRunStatus }
	if s.LastRunError != nil { m["last_run_error"] = *s.LastRunError }
	return m
}
