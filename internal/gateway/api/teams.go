package api

import (
	"net/http"
	"time"

	"github.com/ai-volund/volund/internal/db"
)

// POST /v1/teams — create a team.
func (s *Services) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	var in struct {
		Name                  string   `json:"name"`
		Description           string   `json:"description"`
		OrchestratorProfileID *string  `json:"orchestrator_profile_id"`
		MemberProfileIDs      []string `json:"member_profile_ids"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	team, err := s.Teams.Create(r.Context(), &db.Team{
		TenantID:              claims.TenantID,
		Name:                  in.Name,
		Description:           in.Description,
		OrchestratorProfileID: in.OrchestratorProfileID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create team: "+err.Error())
		return
	}

	// Add members.
	for _, pid := range in.MemberProfileIDs {
		_ = s.Teams.AddMember(r.Context(), team.ID, pid, "member")
	}

	writeJSON(w, http.StatusCreated, teamJSON(team))
}

// GET /v1/teams — list teams for the current tenant.
func (s *Services) handleListTeams(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	teams, err := s.Teams.ListByTenant(r.Context(), claims.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list teams: "+err.Error())
		return
	}
	out := make([]map[string]any, len(teams))
	for i, t := range teams {
		out[i] = teamJSON(t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"teams": out})
}

// GET /v1/teams/{id} — get team with members.
func (s *Services) handleGetTeam(w http.ResponseWriter, r *http.Request) {
	team, err := s.Teams.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "team not found")
		return
	}
	members, _ := s.Teams.ListMembers(r.Context(), team.ID)
	m := teamJSON(team)
	memberOut := make([]map[string]any, len(members))
	for i, mem := range members {
		memberOut[i] = map[string]any{
			"profile_id": mem.ProfileID,
			"role":       mem.Role,
			"added_at":   mem.AddedAt.Format(time.RFC3339),
		}
	}
	m["members"] = memberOut
	writeJSON(w, http.StatusOK, m)
}

// PUT /v1/teams/{id} — update a team.
func (s *Services) handleUpdateTeam(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.Teams.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "team not found")
		return
	}
	var in struct {
		Name                  *string `json:"name"`
		Description           *string `json:"description"`
		OrchestratorProfileID *string `json:"orchestrator_profile_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Name != nil { existing.Name = *in.Name }
	if in.Description != nil { existing.Description = *in.Description }
	if in.OrchestratorProfileID != nil { existing.OrchestratorProfileID = in.OrchestratorProfileID }

	updated, err := s.Teams.Update(r.Context(), existing)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update team: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, teamJSON(updated))
}

// DELETE /v1/teams/{id} — delete a team.
func (s *Services) handleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	if err := s.Teams.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, "delete team: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// POST /v1/teams/{id}/members — add a member to a team.
func (s *Services) handleAddTeamMember(w http.ResponseWriter, r *http.Request) {
	teamID := r.PathValue("id")
	var in struct {
		ProfileID string `json:"profile_id"`
		Role      string `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Role == "" { in.Role = "member" }
	if err := s.Teams.AddMember(r.Context(), teamID, in.ProfileID, in.Role); err != nil {
		writeError(w, http.StatusInternalServerError, "add member: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "added"})
}

// DELETE /v1/teams/{id}/members/{profileId} — remove a member.
func (s *Services) handleRemoveTeamMember(w http.ResponseWriter, r *http.Request) {
	if err := s.Teams.RemoveMember(r.Context(), r.PathValue("id"), r.PathValue("profileId")); err != nil {
		writeError(w, http.StatusInternalServerError, "remove member: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func teamJSON(t *db.Team) map[string]any {
	m := map[string]any{
		"id":          t.ID,
		"tenant_id":   t.TenantID,
		"name":        t.Name,
		"description": t.Description,
		"created_at":  t.CreatedAt.Format(time.RFC3339),
		"updated_at":  t.UpdatedAt.Format(time.RFC3339),
	}
	if t.OrchestratorProfileID != nil {
		m["orchestrator_profile_id"] = *t.OrchestratorProfileID
	}
	return m
}
