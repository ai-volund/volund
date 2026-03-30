package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// ── Admin: tenant-level skill installation ──────────────────────────────────

// POST /v1/admin/skills/{id}/install — installs a skill for the tenant.
// Any authenticated tenant member can install skills.
func (s *Services) handleInstallSkill(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)

	skillID := r.PathValue("id")

	// Verify skill exists.
	sk, err := s.Skills.Get(r.Context(), skillID)
	if err != nil {
		// Try by name.
		sk, err = s.Skills.GetByName(r.Context(), skillID)
		if err != nil {
			writeError(w, http.StatusNotFound, "skill not found")
			return
		}
	}

	if err := s.Skills.InstallForTenant(r.Context(), claims.TenantID, sk.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "install skill: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "installed", "skill": sk.Name})
}

// DELETE /v1/admin/skills/{id}/install — uninstalls a skill from the tenant.
// Any authenticated tenant member can uninstall skills.
func (s *Services) handleUninstallSkill(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)

	skillID := r.PathValue("id")

	sk, err := s.Skills.Get(r.Context(), skillID)
	if err != nil {
		sk, err = s.Skills.GetByName(r.Context(), skillID)
		if err != nil {
			writeError(w, http.StatusNotFound, "skill not found")
			return
		}
	}

	if err := s.Skills.UninstallForTenant(r.Context(), claims.TenantID, sk.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "uninstall skill: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "uninstalled", "skill": sk.Name})
}

// GET /v1/admin/skills/installed — lists skills installed for the tenant.
func (s *Services) handleListInstalledSkills(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)

	skills, err := s.Skills.ListTenantSkills(r.Context(), claims.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list installed: "+err.Error())
		return
	}

	out := make([]map[string]any, 0, len(skills))
	for _, sk := range skills {
		out = append(out, skillJSON(sk))
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": out})
}

// ── User: skill enablement ──────────────────────────────────────────────────

// POST /v1/skills/{id}/enable — enables a skill for the current user.
func (s *Services) handleEnableSkill(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	skillID := r.PathValue("id")

	sk, err := s.Skills.Get(r.Context(), skillID)
	if err != nil {
		sk, err = s.Skills.GetByName(r.Context(), skillID)
		if err != nil {
			writeError(w, http.StatusNotFound, "skill not found")
			return
		}
	}

	userID := s.resolveVolundUserID(r.Context(), claims.Subject)
	if err := s.Skills.EnableForUser(r.Context(), claims.TenantID, userID, sk.ID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "enabled", "skill": sk.Name})
}

// DELETE /v1/skills/{id}/enable — disables a skill for the current user.
func (s *Services) handleDisableSkill(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	skillID := r.PathValue("id")

	sk, err := s.Skills.Get(r.Context(), skillID)
	if err != nil {
		sk, err = s.Skills.GetByName(r.Context(), skillID)
		if err != nil {
			writeError(w, http.StatusNotFound, "skill not found")
			return
		}
	}

	userID := s.resolveVolundUserID(r.Context(), claims.Subject)
	if err := s.Skills.DisableForUser(r.Context(), claims.TenantID, userID, sk.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "disable skill: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "disabled", "skill": sk.Name})
}

// GET /v1/skills — lists skills available to the user (tenant-installed) with enable status.
func (s *Services) handleListAvailableSkills(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)

	userID := s.resolveVolundUserID(r.Context(), claims.Subject)
	skills, err := s.Skills.ListAvailableSkills(r.Context(), claims.TenantID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list skills: "+err.Error())
		return
	}

	out := make([]map[string]any, 0, len(skills))
	for _, sk := range skills {
		m := map[string]any{
			"id":          sk.ID,
			"name":        sk.Name,
			"version":     sk.Version,
			"description": sk.Description,
			"author":      sk.Author,
			"type":        sk.Type,
			"tags":        sk.Tags,
			"enabled":     sk.Enabled,
			"created_at":  sk.CreatedAt.Format(time.RFC3339),
		}

		// Extract provider IDs from spec so the UI can show credential status.
		var parsed struct {
			Providers []struct {
				ID string `json:"id"`
			} `json:"providers"`
		}
		if sk.Spec != nil {
			json.Unmarshal(sk.Spec, &parsed)
		}
		providerIDs := make([]string, 0)
		for _, p := range parsed.Providers {
			providerIDs = append(providerIDs, p.ID)
		}
		m["required_providers"] = providerIDs

		out = append(out, m)
	}

	writeJSON(w, http.StatusOK, map[string]any{"skills": out})
}
