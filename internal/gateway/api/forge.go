package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ai-volund/volund/internal/db"
	"github.com/ai-volund/volund/internal/providers"
)

// handleListSkills returns published skills from the Forge registry.
// Public endpoint — no auth required. Supports query params:
//
//	?type=mcp&tag=search&author=volund&q=github&limit=50&offset=0
func (s *Services) handleListSkills(w http.ResponseWriter, r *http.Request) {
	if s.Skills == nil {
		writeError(w, http.StatusServiceUnavailable, "skill registry not available")
		return
	}

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	skills, err := s.Skills.List(r.Context(), db.SkillFilter{
		Type:   q.Get("type"),
		Author: q.Get("author"),
		Tag:    q.Get("tag"),
		Query:  q.Get("q"),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list skills: "+err.Error())
		return
	}

	out := make([]map[string]any, 0, len(skills))
	for _, sk := range skills {
		out = append(out, skillJSON(sk))
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": out})
}

// handleGetSkill returns a single skill by ID or name.
func (s *Services) handleGetSkill(w http.ResponseWriter, r *http.Request) {
	if s.Skills == nil {
		writeError(w, http.StatusServiceUnavailable, "skill registry not available")
		return
	}

	id := r.PathValue("id")

	// Try by ID first, then by name.
	sk, err := s.Skills.Get(r.Context(), id)
	if err != nil {
		sk, err = s.Skills.GetByName(r.Context(), id)
	}
	if err != nil {
		writeError(w, http.StatusNotFound, "skill not found")
		return
	}

	// Bump download counter on GET.
	_ = s.Skills.IncrementDownloads(r.Context(), sk.ID)

	writeJSON(w, http.StatusOK, skillJSON(sk))
}

// handlePublishSkill publishes a new skill to the Forge registry.
func (s *Services) handlePublishSkill(w http.ResponseWriter, r *http.Request) {
	if s.Skills == nil {
		writeError(w, http.StatusServiceUnavailable, "skill registry not available")
		return
	}

	var in struct {
		Name        string          `json:"name"`
		Version     string          `json:"version"`
		Description string          `json:"description"`
		Author      string          `json:"author"`
		Type        string          `json:"type"`
		Tags        []string        `json:"tags"`
		Spec        json.RawMessage `json:"spec"`
		Readme      string          `json:"readme"`
	}
	if !decode(w, r, &in) {
		return
	}

	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if in.Type == "" {
		in.Type = "prompt"
	}
	if in.Version == "" {
		in.Version = "0.0.1"
	}
	if in.Author == "" {
		claims := claimsFromReq(r)
		in.Author = claims.Subject // use user ID as fallback
	}
	if in.Spec == nil {
		in.Spec = json.RawMessage("{}")
	}
	if in.Tags == nil {
		in.Tags = []string{}
	}

	sk, err := s.Skills.Create(r.Context(), &db.RegistrySkill{
		Name:        in.Name,
		Version:     in.Version,
		Description: in.Description,
		Author:      in.Author,
		Type:        in.Type,
		Tags:        in.Tags,
		Spec:        in.Spec,
		Readme:      in.Readme,
		Published:   true,
	})
	if err != nil {
		if isConflict(err) {
			writeError(w, http.StatusConflict, "skill name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "publish skill: "+err.Error())
		return
	}

	// Auto-register providers declared in the skill spec.
	s.registerSkillProviders(r.Context(), in.Name, in.Spec)

	writeJSON(w, http.StatusCreated, skillJSON(sk))
}

// handleUpdateSkill updates an existing skill.
func (s *Services) handleUpdateSkill(w http.ResponseWriter, r *http.Request) {
	if s.Skills == nil {
		writeError(w, http.StatusServiceUnavailable, "skill registry not available")
		return
	}

	id := r.PathValue("id")
	sk, err := s.Skills.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "skill not found")
		return
	}

	var in struct {
		Version     string          `json:"version"`
		Description string          `json:"description"`
		Author      string          `json:"author"`
		Type        string          `json:"type"`
		Tags        []string        `json:"tags"`
		Spec        json.RawMessage `json:"spec"`
		Readme      string          `json:"readme"`
		Published   *bool           `json:"published"`
	}
	if !decode(w, r, &in) {
		return
	}

	if in.Version != "" {
		sk.Version = in.Version
	}
	if in.Description != "" {
		sk.Description = in.Description
	}
	if in.Author != "" {
		sk.Author = in.Author
	}
	if in.Type != "" {
		sk.Type = in.Type
	}
	if in.Tags != nil {
		sk.Tags = in.Tags
	}
	if in.Spec != nil {
		sk.Spec = in.Spec
	}
	if in.Readme != "" {
		sk.Readme = in.Readme
	}
	if in.Published != nil {
		sk.Published = *in.Published
	}

	sk, err = s.Skills.Update(r.Context(), sk)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update skill: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, skillJSON(sk))
}

// handleDeleteSkill removes a skill from the registry.
func (s *Services) handleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	if s.Skills == nil {
		writeError(w, http.StatusServiceUnavailable, "skill registry not available")
		return
	}

	id := r.PathValue("id")
	if err := s.Skills.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "skill not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// registerSkillProviders extracts provider definitions from a skill spec
// and auto-registers them in the OAuth provider registry. Providers are
// registered without credentials — admin supplies those separately.
func (s *Services) registerSkillProviders(ctx context.Context, skillName string, spec json.RawMessage) {
	if s.OAuth == nil || spec == nil {
		return
	}

	// Parse provider definitions from the skill spec.
	var parsed struct {
		Providers []struct {
			ID              string            `json:"id"`
			DisplayName     string            `json:"display_name"`
			Type            string            `json:"type"`
			AuthURL         string            `json:"auth_url"`
			TokenURL        string            `json:"token_url"`
			Scopes          []string          `json:"scopes"`
			ExtraAuthParams map[string]string `json:"extra_auth_params"`
			ExtraHeaders    map[string]string `json:"extra_headers"`
			IconURL         string            `json:"icon_url"`
			Category        string            `json:"category"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(spec, &parsed); err != nil || len(parsed.Providers) == 0 {
		return
	}

	for _, p := range parsed.Providers {
		// Only auto-register oauth2 providers — connection_config types
		// are handled differently (form-based, no OAuth redirect).
		if p.Type != "oauth2" || p.AuthURL == "" || p.TokenURL == "" {
			continue
		}

		err := s.OAuth.Registry.RegisterFromSkill(ctx, &providers.ProviderConfig{
			ID:              p.ID,
			DisplayName:     p.DisplayName,
			AuthURL:         p.AuthURL,
			TokenURL:        p.TokenURL,
			Scopes:          p.Scopes,
			ExtraAuthParams: p.ExtraAuthParams,
			ExtraHeaders:    p.ExtraHeaders,
			IconURL:         p.IconURL,
			Category:        p.Category,
			SkillID:         skillName,
		})
		if err != nil {
			slog.Warn("failed to register provider from skill", "skill", skillName, "provider", p.ID, "error", err)
		} else {
			slog.Info("registered provider from skill", "skill", skillName, "provider", p.ID)
		}
	}
}

func skillJSON(sk *db.RegistrySkill) map[string]any {
	tags := sk.Tags
	if tags == nil {
		tags = []string{}
	}
	spec := sk.Spec
	if spec == nil {
		spec = json.RawMessage("{}")
	}
	return map[string]any{
		"id":          sk.ID,
		"name":        sk.Name,
		"version":     sk.Version,
		"description": sk.Description,
		"author":      sk.Author,
		"type":        sk.Type,
		"tags":        tags,
		"spec":        spec,
		"readme":      sk.Readme,
		"downloads":   sk.Downloads,
		"published":   sk.Published,
		"created_at":  sk.CreatedAt.Format(time.RFC3339),
		"updated_at":  sk.UpdatedAt.Format(time.RFC3339),
	}
}
