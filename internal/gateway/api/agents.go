package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ai-volund/volund/internal/db"
)

// POST /v1/agents — creates a user-scoped custom agent.
func (s *Services) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)

	var in struct {
		Name          string          `json:"name"`
		ProfileType   string          `json:"profile_type"`
		SystemPrompt  string          `json:"system_prompt"`
		ModelProvider string          `json:"model_provider"`
		ModelID       string          `json:"model_id"`
		Temperature   float64         `json:"temperature"`
		MaxTokens     int             `json:"max_tokens"`
		Skills        json.RawMessage `json:"skills"`
	}
	if !decode(w, r, &in) {
		return
	}

	if in.ModelProvider == "" {
		in.ModelProvider = "openai"
	}
	if in.ModelID == "" {
		in.ModelID = "gpt-4o"
	}
	if in.Temperature == 0 {
		in.Temperature = 0.7
	}
	if in.MaxTokens == 0 {
		in.MaxTokens = 4096
	}
	if in.ProfileType == "" {
		in.ProfileType = "specialist"
	}

	// User-created agents are always user-scoped.
	userID := claims.Subject
	profile, err := s.Agents.CreateProfile(r.Context(), &db.AgentProfile{
		TenantID:      claims.TenantID,
		Name:          in.Name,
		ProfileType:   in.ProfileType,
		Visibility:    "user",
		OwnerID:       &userID,
		SystemPrompt:  in.SystemPrompt,
		ModelProvider: in.ModelProvider,
		ModelID:       in.ModelID,
		Temperature:   in.Temperature,
		MaxTokens:     in.MaxTokens,
		Skills:        in.Skills,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create agent failed")
		return
	}
	writeJSON(w, http.StatusCreated, agentJSON(profile))
}

// POST /v1/admin/agents — creates a system-scoped agent visible to all tenant users.
func (s *Services) handleCreateSystemAgent(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)

	if claims.Role != "admin" && claims.Role != "platform_admin" {
		writeError(w, http.StatusForbidden, "admin role required")
		return
	}

	var in struct {
		Name          string          `json:"name"`
		ProfileType   string          `json:"profile_type"`
		SystemPrompt  string          `json:"system_prompt"`
		ModelProvider string          `json:"model_provider"`
		ModelID       string          `json:"model_id"`
		Temperature   float64         `json:"temperature"`
		MaxTokens     int             `json:"max_tokens"`
		Skills        json.RawMessage `json:"skills"`
	}
	if !decode(w, r, &in) {
		return
	}

	if in.ModelProvider == "" {
		in.ModelProvider = "openai"
	}
	if in.ModelID == "" {
		in.ModelID = "gpt-4o"
	}
	if in.Temperature == 0 {
		in.Temperature = 0.7
	}
	if in.MaxTokens == 0 {
		in.MaxTokens = 4096
	}
	if in.ProfileType == "" {
		in.ProfileType = "specialist"
	}

	profile, err := s.Agents.CreateProfile(r.Context(), &db.AgentProfile{
		TenantID:      claims.TenantID,
		Name:          in.Name,
		ProfileType:   in.ProfileType,
		Visibility:    "system",
		SystemPrompt:  in.SystemPrompt,
		ModelProvider: in.ModelProvider,
		ModelID:       in.ModelID,
		Temperature:   in.Temperature,
		MaxTokens:     in.MaxTokens,
		Skills:        in.Skills,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create system agent failed")
		return
	}
	writeJSON(w, http.StatusCreated, agentJSON(profile))
}

// GET /v1/agents — returns system agents + current user's custom agents.
func (s *Services) handleListAgents(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	profiles, err := s.Agents.ListProfiles(r.Context(), claims.TenantID, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list failed")
		return
	}
	out := make([]any, len(profiles))
	for i, p := range profiles {
		out[i] = agentJSON(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}

// GET /v1/agents/{id}
func (s *Services) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	profile, err := s.Agents.GetProfile(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if profile.TenantID != claims.TenantID && claims.Role != "platform_admin" {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	// User-scoped agents are only visible to the owner.
	if profile.Visibility == "user" && (profile.OwnerID == nil || *profile.OwnerID != claims.Subject) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	writeJSON(w, http.StatusOK, agentJSON(profile))
}

// PUT /v1/agents/{id}
func (s *Services) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	profile, err := s.Agents.GetProfile(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if profile.TenantID != claims.TenantID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	// Users can only edit their own custom agents. Admins can edit system agents.
	if profile.Visibility == "user" && (profile.OwnerID == nil || *profile.OwnerID != claims.Subject) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if profile.Visibility == "system" && claims.Role != "admin" && claims.Role != "platform_admin" {
		writeError(w, http.StatusForbidden, "admin role required to edit system agents")
		return
	}

	var in struct {
		Name          string          `json:"name"`
		ProfileType   string          `json:"profile_type"`
		SystemPrompt  string          `json:"system_prompt"`
		ModelProvider string          `json:"model_provider"`
		ModelID       string          `json:"model_id"`
		Temperature   *float64        `json:"temperature"`
		MaxTokens     *int            `json:"max_tokens"`
		Skills        json.RawMessage `json:"skills"`
	}
	if !decode(w, r, &in) {
		return
	}

	if in.Name != "" {
		profile.Name = in.Name
	}
	if in.ProfileType != "" {
		profile.ProfileType = in.ProfileType
	}
	if in.SystemPrompt != "" {
		profile.SystemPrompt = in.SystemPrompt
	}
	if in.ModelProvider != "" {
		profile.ModelProvider = in.ModelProvider
	}
	if in.ModelID != "" {
		profile.ModelID = in.ModelID
	}
	if in.Temperature != nil {
		profile.Temperature = *in.Temperature
	}
	if in.MaxTokens != nil {
		profile.MaxTokens = *in.MaxTokens
	}
	if in.Skills != nil {
		profile.Skills = in.Skills
	}

	updated, err := s.Agents.UpdateProfile(r.Context(), profile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, agentJSON(updated))
}

// DELETE /v1/agents/{id}
func (s *Services) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	profile, err := s.Agents.GetProfile(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if profile.TenantID != claims.TenantID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	// Users can only delete their own custom agents. Admins can delete system agents.
	if profile.Visibility == "user" && (profile.OwnerID == nil || *profile.OwnerID != claims.Subject) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if profile.Visibility == "system" && claims.Role != "admin" && claims.Role != "platform_admin" {
		writeError(w, http.StatusForbidden, "admin role required to delete system agents")
		return
	}
	if err := s.Agents.DeleteProfile(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func agentJSON(p *db.AgentProfile) map[string]any {
	skills := p.Skills
	if skills == nil {
		skills = json.RawMessage("[]")
	}
	out := map[string]any{
		"id":             p.ID,
		"tenant_id":      p.TenantID,
		"name":           p.Name,
		"profile_type":   p.ProfileType,
		"visibility":     p.Visibility,
		"system_prompt":  p.SystemPrompt,
		"model_provider": p.ModelProvider,
		"model_id":       p.ModelID,
		"temperature":    p.Temperature,
		"max_tokens":     p.MaxTokens,
		"skills":         skills,
		"created_at":     p.CreatedAt.Format(time.RFC3339),
		"updated_at":     p.UpdatedAt.Format(time.RFC3339),
	}
	if p.OwnerID != nil {
		out["owner_id"] = *p.OwnerID
	}
	return out
}
