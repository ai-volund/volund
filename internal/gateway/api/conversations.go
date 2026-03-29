package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/ai-volund/volund/internal/credentials"
	"github.com/ai-volund/volund/internal/db"
	"github.com/ai-volund/volund/internal/dispatch"
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

// PATCH /v1/conversations/{id}
func (s *Services) handleUpdateConversation(w http.ResponseWriter, r *http.Request) {
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
		Title string `json:"title"`
	}
	if !decode(w, r, &in) {
		return
	}

	if err := s.Convos.UpdateTitle(r.Context(), convo.ID, in.Title); err != nil {
		writeError(w, http.StatusInternalServerError, "update conversation failed")
		return
	}
	convo.Title = in.Title
	writeJSON(w, http.StatusOK, convoJSON(convo, nil))
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

// POST /v1/conversations/{id}/messages — persist user message and dispatch to agent.
// Returns the saved message immediately; the agent response streams via WebSocket.
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
		// Agent routing hints (optional — control plane uses profile defaults if absent).
		ProfileName string `json:"profile_name,omitempty"`
		ProfileType string `json:"profile_type,omitempty"`
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

	// 1. Persist the user message.
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

	// 2. Dispatch to the agent pool (fire-and-forget from the HTTP handler's perspective).
	if s.DispatchFn != nil {
		go s.dispatchToAgent(context.WithoutCancel(r.Context()), convo, in.ProfileName, in.ProfileType, claims.TenantID, claims.Subject)
	}

	writeJSON(w, http.StatusCreated, msgJSON(msg))
}

// dispatchToAgent assembles conversation history and dispatches a task to the agent pool.
// When a ClaimManager is available, it first tries to claim a warm pod for targeted
// dispatch. If no warm pod is available (or claim fails), it falls back to pool dispatch.
func (s *Services) dispatchToAgent(ctx context.Context, convo *db.Conversation, profileName, profileType, tenantID, userID string) {
	// Load full conversation history.
	msgs, err := s.Convos.ListMessages(ctx, convo.ID, 500)
	if err != nil {
		slog.Error("dispatch: failed to load messages", "conv_id", convo.ID, "error", err)
		return
	}

	// Resolve profile defaults.
	if profileName == "" {
		profileName = "default-orchestrator"
	}
	if profileType == "" {
		profileType = "orchestrator"
	}

	task := &dispatch.Task{
		TaskID:         dispatch.NewTaskID(),
		ConversationID: convo.ID,
		TenantID:       tenantID,
		ProfileName:    profileName,
		ProfileType:    profileType,
		Messages:       buildMessages(msgs),
	}

	// Resolve profile config and skills.
	if s.ProfileResolver != nil {
		cfg, skills, err := s.ProfileResolver.ResolveProfile(ctx, profileName)
		if err != nil {
			slog.Warn("dispatch: failed to resolve profile, continuing with defaults",
				"profile", profileName, "error", err)
		} else {
			if cfg != nil {
				task.Provider = cfg.Provider
				task.Model = cfg.Model
				task.MaxTokens = cfg.MaxTokens
				task.Temperature = cfg.Temperature
				task.MaxToolRounds = cfg.MaxToolRounds
				task.SystemPrompt = cfg.SystemPrompt
			}
			if len(skills) > 0 {
				task.Skills = skills
			}
			slog.Info("dispatch: resolved profile",
				"profile", profileName, "skills", len(skills),
				"provider", task.Provider, "model", task.Model)
		}
	}

	// Create a persistent task record for tracking.
	if s.Tasks != nil {
		convID := convo.ID
		dbTask, taskErr := s.Tasks.Create(ctx, tenantID, &convID, "Agent task: "+profileName)
		if taskErr != nil {
			slog.Warn("dispatch: failed to create task record", "error", taskErr)
		} else {
			task.DBTaskID = dbTask.ID
		}
	}

	// Resolve user's active skills (tenant-installed + user-enabled) and append to task.
	if s.Skills != nil && userID != "" {
		activeSkills, err := s.Skills.ListActiveSkills(ctx, tenantID, userID)
		if err != nil {
			slog.Warn("dispatch: failed to list active skills", "error", err)
		} else {
			for _, sk := range activeSkills {
				spec := skillToSpec(sk)
				if spec != nil {
					task.Skills = append(task.Skills, *spec)
				}
			}
			if len(activeSkills) > 0 {
				slog.Info("dispatch: attached active skills", "count", len(activeSkills), "conv_id", convo.ID)
			}
		}
	}

	// Issue scoped credentials for the task if the broker is available.
	if s.CredBroker != nil && userID != "" {
		providers, err := s.CredBroker.ListProviders(ctx, tenantID, userID)
		if err != nil {
			slog.Warn("dispatch: failed to list credential providers", "error", err)
		} else {
			for _, p := range providers {
				resp, err := s.CredBroker.IssueToken(ctx, credentials.TokenRequest{
					TenantID:       tenantID,
					UserID:         userID,
					Provider:       p.Provider,
					ConversationID: convo.ID,
				})
				if err != nil {
					slog.Debug("dispatch: skipping credential", "provider", p.Provider, "error", err)
					continue
				}
				task.Credentials = append(task.Credentials, dispatch.TaskCredential{
					Provider:  p.Provider,
					Token:     resp.Token,
					Scopes:    resp.Scopes,
					ExpiresIn: resp.ExpiresIn,
				})
			}
			if len(task.Credentials) > 0 {
				slog.Info("dispatch: attached credentials", "count", len(task.Credentials), "conv_id", convo.ID)
			}
		}
	}

	// Try to claim a warm pod for targeted dispatch.
	// ClaimMgr needs the profile UUID, not name — resolve first.
	if s.ClaimMgr != nil && s.Agents != nil {
		profile, profileErr := s.Agents.GetProfileByName(ctx, tenantID, profileName)
		if profileErr != nil {
			slog.Debug("dispatch: profile lookup failed, skipping claim",
				"profile", profileName, "error", profileErr)
		} else {
			claim, claimErr := s.ClaimMgr.EnsureInstance(ctx, convo.ID, tenantID, profile.ID)
			if claimErr != nil {
				slog.Warn("dispatch: pod claim failed, falling back to pool dispatch",
					"conv_id", convo.ID, "error", claimErr)
			} else if claim != nil {
				// Use pod name for NATS routing (agents subscribe by pod name).
				// Fall back to instance UUID if pod name isn't available.
				if claim.PodName != "" {
					task.InstanceID = claim.PodName
				} else {
					task.InstanceID = claim.InstanceID
				}
			}
		}
		// instanceID == "" means no warm pods — fall through to pool dispatch.
	}

	if err := s.DispatchFn(ctx, task); err != nil {
		slog.Error("dispatch: failed to dispatch task", "conv_id", convo.ID, "task_id", task.TaskID, "error", err)
	}
}

// skillToSpec converts a DB RegistrySkill to a dispatch.SkillSpec.
// It parses the spec JSON to extract runtime configuration.
func skillToSpec(sk *db.RegistrySkill) *dispatch.SkillSpec {
	spec := &dispatch.SkillSpec{
		Name:        sk.Name,
		Type:        sk.Type,
		Version:     sk.Version,
		Description: sk.Description,
	}

	// Parse runtime config from the spec JSONB.
	if sk.Spec != nil {
		var parsed struct {
			Runtime *struct {
				Image     string `json:"image"`
				Mode      string `json:"mode"`
				Transport string `json:"transport"`
			} `json:"runtime"`
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal(sk.Spec, &parsed); err == nil {
			if parsed.Runtime != nil {
				spec.Runtime = &dispatch.SkillRuntimeSpec{
					Image:     parsed.Runtime.Image,
					Mode:      parsed.Runtime.Mode,
					Transport: parsed.Runtime.Transport,
				}
			}
			if parsed.Prompt != "" {
				spec.Prompt = parsed.Prompt
			}
		}
	}

	return spec
}

// buildMessages converts DB messages to dispatch transport format.
func buildMessages(msgs []*db.Message) []dispatch.Message {
	out := make([]dispatch.Message, 0, len(msgs))
	for _, m := range msgs {
		var blocks []dispatch.Block
		if err := json.Unmarshal(m.Content, &blocks); err != nil {
			// Not a block array — treat raw content as plain text.
			blocks = []dispatch.Block{{Type: "text", Text: string(m.Content)}}
		}
		out = append(out, dispatch.Message{
			Role:    m.Role,
			Content: blocks,
		})
	}
	return out
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
