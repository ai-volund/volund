package api

import (
	"net/http"
	"time"
)

// GET /v1/notifications — list notifications for the current user.
func (s *Services) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	userID := s.resolveVolundUserID(r.Context(), claims.Subject)

	if s.Notifications == nil {
		writeJSON(w, http.StatusOK, map[string]any{"notifications": []any{}, "unread": 0})
		return
	}

	notifs, err := s.Notifications.List(r.Context(), userID, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list notifications: "+err.Error())
		return
	}

	unread, _ := s.Notifications.CountUnread(r.Context(), userID)

	out := make([]map[string]any, len(notifs))
	for i, n := range notifs {
		out[i] = map[string]any{
			"id":         n.ID,
			"type":       n.Type,
			"title":      n.Title,
			"body":       n.Body,
			"read":       n.Read,
			"metadata":   n.Metadata,
			"created_at": n.CreatedAt.Format(time.RFC3339),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"notifications": out, "unread": unread})
}

// POST /v1/notifications/{id}/read — mark a notification as read.
func (s *Services) handleMarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	userID := s.resolveVolundUserID(r.Context(), claims.Subject)
	id := r.PathValue("id")

	if s.Notifications == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	if id == "all" {
		_ = s.Notifications.MarkAllRead(r.Context(), userID)
	} else {
		_ = s.Notifications.MarkRead(r.Context(), id, userID)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
