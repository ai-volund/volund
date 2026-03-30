package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ai-volund/volund/internal/db"
)

// POST /v1/admin/llm/providers — register a new LLM provider.
func (s *Services) handleCreateLLMProvider(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string          `json:"name"`
		Type     string          `json:"type"`     // "openai", "anthropic", "ollama", "openai-compatible"
		APIKey   string          `json:"api_key"`
		BaseURL  string          `json:"base_url"`
		Config   json.RawMessage `json:"config"`
		Enabled  *bool           `json:"enabled"`
		Priority *int            `json:"priority"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Name == "" || in.Type == "" {
		writeError(w, http.StatusBadRequest, "name and type are required")
		return
	}

	switch in.Type {
	case "openai", "anthropic", "ollama", "openai-compatible":
		// valid
	default:
		writeError(w, http.StatusBadRequest, "type must be openai, anthropic, ollama, or openai-compatible")
		return
	}

	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	priority := 0
	if in.Priority != nil {
		priority = *in.Priority
	}

	p := &db.LLMProvider{
		Name:     in.Name,
		Type:     in.Type,
		APIKey:   in.APIKey,
		BaseURL:  in.BaseURL,
		Config:   in.Config,
		Enabled:  enabled,
		Priority: priority,
	}

	created, err := s.LLMProviders.Create(r.Context(), p)
	if err != nil {
		if isConflict(err) {
			writeError(w, http.StatusConflict, "provider name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "create provider: "+err.Error())
		return
	}

	// Hot-reload: register the new provider in the running router.
	if s.LLMReloadFn != nil {
		s.LLMReloadFn()
	}

	writeJSON(w, http.StatusCreated, llmProviderJSON(created, false))
}

// GET /v1/admin/llm/providers — list all configured LLM providers.
func (s *Services) handleListLLMProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.LLMProviders.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list providers: "+err.Error())
		return
	}
	out := make([]map[string]any, len(providers))
	for i, p := range providers {
		out[i] = llmProviderJSON(p, true)
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": out})
}

// GET /v1/admin/llm/providers/{id} — get a single provider.
func (s *Services) handleGetLLMProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.LLMProviders.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}
	writeJSON(w, http.StatusOK, llmProviderJSON(p, false))
}

// PUT /v1/admin/llm/providers/{id} — update a provider.
func (s *Services) handleUpdateLLMProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.LLMProviders.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}

	var in struct {
		Name     *string          `json:"name"`
		Type     *string          `json:"type"`
		APIKey   *string          `json:"api_key"`
		BaseURL  *string          `json:"base_url"`
		Config   json.RawMessage  `json:"config"`
		Enabled  *bool            `json:"enabled"`
		Priority *int             `json:"priority"`
	}
	if !decode(w, r, &in) {
		return
	}

	if in.Name != nil {
		existing.Name = *in.Name
	}
	if in.Type != nil {
		existing.Type = *in.Type
	}
	if in.APIKey != nil {
		existing.APIKey = *in.APIKey
	}
	if in.BaseURL != nil {
		existing.BaseURL = *in.BaseURL
	}
	if in.Config != nil {
		existing.Config = in.Config
	}
	if in.Enabled != nil {
		existing.Enabled = *in.Enabled
	}
	if in.Priority != nil {
		existing.Priority = *in.Priority
	}

	updated, err := s.LLMProviders.Update(r.Context(), existing)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update provider: "+err.Error())
		return
	}

	if s.LLMReloadFn != nil {
		s.LLMReloadFn()
	}

	writeJSON(w, http.StatusOK, llmProviderJSON(updated, false))
}

// DELETE /v1/admin/llm/providers/{id} — remove a provider.
func (s *Services) handleDeleteLLMProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.LLMProviders.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "delete provider: "+err.Error())
		return
	}

	if s.LLMReloadFn != nil {
		s.LLMReloadFn()
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// POST /v1/admin/llm/providers/{id}/test — test a provider connection.
func (s *Services) handleTestLLMProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.LLMProviders.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}

	// Try to list models as a health check.
	if s.LLMTestFn == nil {
		writeError(w, http.StatusServiceUnavailable, "LLM testing not available")
		return
	}

	models, testErr := s.LLMTestFn(r.Context(), p)
	if testErr != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "error",
			"error":   testErr.Error(),
			"models":  []string{},
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"models": models,
	})
}

// GET /v1/admin/llm/models — list all models across all enabled providers.
func (s *Services) handleListLLMModels(w http.ResponseWriter, r *http.Request) {
	if s.LLMListModelsFn == nil {
		writeError(w, http.StatusServiceUnavailable, "LLM router not available")
		return
	}

	models, err := s.LLMListModelsFn(r.Context(), "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list models: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func llmProviderJSON(p *db.LLMProvider, maskKey bool) map[string]any {
	apiKey := p.APIKey
	if maskKey && apiKey != "" {
		if len(apiKey) > 8 {
			apiKey = apiKey[:4] + "..." + apiKey[len(apiKey)-4:]
		} else {
			apiKey = "****"
		}
	}
	return map[string]any{
		"id":         p.ID,
		"name":       p.Name,
		"type":       p.Type,
		"api_key":    apiKey,
		"base_url":   p.BaseURL,
		"config":     p.Config,
		"enabled":    p.Enabled,
		"priority":   p.Priority,
		"created_at": p.CreatedAt.Format(time.RFC3339),
		"updated_at": p.UpdatedAt.Format(time.RFC3339),
	}
}
