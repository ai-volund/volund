package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/ai-volund/volund/internal/db"
)

// GET /v1/tasks
func (s *Services) handleListTasks(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	tasks, err := s.Tasks.ListByTenant(r.Context(), claims.TenantID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list tasks failed")
		return
	}
	out := make([]any, len(tasks))
	for i, t := range tasks {
		out[i] = taskJSON(t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": out})
}

// GET /v1/tasks/{id}
func (s *Services) handleGetTask(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	task, err := s.Tasks.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if task.TenantID != claims.TenantID && claims.Role != "platform_admin" {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	writeJSON(w, http.StatusOK, taskJSON(task))
}

// POST /v1/tasks/{id}/cancel
func (s *Services) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	task, err := s.Tasks.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if task.TenantID != claims.TenantID && claims.Role != "platform_admin" {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if task.Status != "pending" && task.Status != "running" {
		writeError(w, http.StatusConflict, "task cannot be cancelled in status: "+task.Status)
		return
	}
	if err := s.Tasks.Cancel(r.Context(), task.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "cancel task failed")
		return
	}
	task.Status = "cancelled"
	writeJSON(w, http.StatusOK, taskJSON(task))
}

func taskJSON(t *db.Task) map[string]any {
	out := map[string]any{
		"id":         t.ID,
		"tenant_id":  t.TenantID,
		"title":      t.Title,
		"status":     t.Status,
		"created_at": t.CreatedAt.Format(time.RFC3339),
		"updated_at": t.UpdatedAt.Format(time.RFC3339),
	}
	if t.ConversationID != nil {
		out["conversation_id"] = *t.ConversationID
	}
	if t.AgentID != nil {
		out["agent_id"] = *t.AgentID
	}
	if t.Result != nil {
		out["result"] = t.Result
	}
	if t.Error != nil {
		out["error"] = *t.Error
	}
	if t.CompletedAt != nil {
		out["completed_at"] = t.CompletedAt.Format(time.RFC3339)
	}
	return out
}
