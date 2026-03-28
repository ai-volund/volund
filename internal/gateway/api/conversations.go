package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ai-volund/volund/internal/db"
)

// POST /v1/conversations
func (s *Services) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)

	var in struct {
		Title string `json:"title"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Title == "" {
		in.Title = "New conversation"
	}

	userID := claims.Subject
	convo, err := s.Convos.Create(r.Context(), claims.TenantID, &userID, in.Title)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create conversation failed")
		return
	}
	writeJSON(w, http.StatusCreated, convoJSON(convo, nil))
}

// GET /v1/conversations
func (s *Services) handleListConversations(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	convos, err := s.Convos.ListByUser(r.Context(), claims.TenantID, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list failed")
		return
	}
	out := make([]any, len(convos))
	for i, c := range convos {
		out[i] = convoJSON(c, nil)
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": out})
}

// GET /v1/conversations/{id}
func (s *Services) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	convo, err := s.Convos.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	if convo.TenantID != claims.TenantID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	msgs, err := s.Convos.ListMessages(r.Context(), convo.ID, 200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load messages failed")
		return
	}
	writeJSON(w, http.StatusOK, convoJSON(convo, msgs))
}

// DELETE /v1/conversations/{id}
func (s *Services) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	convo, err := s.Convos.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	if convo.TenantID != claims.TenantID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := s.Convos.Delete(r.Context(), convo.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// POST /v1/conversations/{id}/messages — non-streaming plain message append.
func (s *Services) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	convo, err := s.Convos.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	if convo.TenantID != claims.TenantID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var in struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"` // array of content blocks
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Role == "" {
		in.Role = "user"
	}
	if in.Content == nil {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	authorID := claims.Subject
	msg, err := s.Convos.AddMessage(r.Context(), &db.Message{
		ConversationID: convo.ID,
		Role:           in.Role,
		AuthorType:     "user",
		AuthorID:       &authorID,
		Content:        in.Content,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "add message failed")
		return
	}
	writeJSON(w, http.StatusCreated, msgJSON(msg))
}

func convoJSON(c *db.Conversation, msgs []*db.Message) map[string]any {
	out := map[string]any{
		"id":         c.ID,
		"tenant_id":  c.TenantID,
		"title":      c.Title,
		"status":     c.Status,
		"created_at": c.CreatedAt.Format(time.RFC3339),
		"updated_at": c.UpdatedAt.Format(time.RFC3339),
	}
	if c.UserID != nil {
		out["user_id"] = *c.UserID
	}
	if msgs != nil {
		ms := make([]any, len(msgs))
		for i, m := range msgs {
			ms[i] = msgJSON(m)
		}
		out["messages"] = ms
	}
	return out
}

func msgJSON(m *db.Message) map[string]any {
	out := map[string]any{
		"id":              m.ID,
		"conversation_id": m.ConversationID,
		"role":            m.Role,
		"author_type":     m.AuthorType,
		"content":         m.Content,
		"created_at":      m.CreatedAt.Format(time.RFC3339),
	}
	if m.AuthorID != nil {
		out["author_id"] = *m.AuthorID
	}
	if m.AgentName != nil {
		out["agent_name"] = *m.AgentName
	}
	return out
}
