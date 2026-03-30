package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type Team struct {
	ID                    string    `json:"id"`
	TenantID              string    `json:"tenant_id"`
	Name                  string    `json:"name"`
	Description           string    `json:"description"`
	OrchestratorProfileID *string   `json:"orchestrator_profile_id"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type TeamMember struct {
	TeamID    string    `json:"team_id"`
	ProfileID string    `json:"profile_id"`
	Role      string    `json:"role"`
	AddedAt   time.Time `json:"added_at"`
}

type TeamRepo struct{ pool *Pool }

func NewTeamRepo(pool *Pool) *TeamRepo { return &TeamRepo{pool: pool} }

func (r *TeamRepo) Create(ctx context.Context, t *Team) (*Team, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO teams (tenant_id, name, description, orchestrator_profile_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at`,
		t.TenantID, t.Name, t.Description, t.OrchestratorProfileID)
	if err := row.Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, fmt.Errorf("create team: %w", err)
	}
	return t, nil
}

func (r *TeamRepo) Get(ctx context.Context, id string) (*Team, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, description, orchestrator_profile_id, created_at, updated_at
		FROM teams WHERE id = $1`, id)
	var t Team
	err := row.Scan(&t.ID, &t.TenantID, &t.Name, &t.Description, &t.OrchestratorProfileID, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *TeamRepo) ListByTenant(ctx context.Context, tenantID string) ([]*Team, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, name, description, orchestrator_profile_id, created_at, updated_at
		FROM teams WHERE tenant_id = $1 ORDER BY name`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Team, error) {
		var t Team
		err := row.Scan(&t.ID, &t.TenantID, &t.Name, &t.Description, &t.OrchestratorProfileID, &t.CreatedAt, &t.UpdatedAt)
		return &t, err
	})
}

func (r *TeamRepo) Update(ctx context.Context, t *Team) (*Team, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE teams SET name = $2, description = $3, orchestrator_profile_id = $4, updated_at = NOW()
		WHERE id = $1
		RETURNING id, tenant_id, name, description, orchestrator_profile_id, created_at, updated_at`, t.ID, t.Name, t.Description, t.OrchestratorProfileID)
	var updated Team
	err := row.Scan(&updated.ID, &updated.TenantID, &updated.Name, &updated.Description, &updated.OrchestratorProfileID, &updated.CreatedAt, &updated.UpdatedAt)
	return &updated, err
}

func (r *TeamRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM teams WHERE id = $1`, id)
	return err
}

func (r *TeamRepo) AddMember(ctx context.Context, teamID, profileID, role string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO team_members (team_id, profile_id, role)
		VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, teamID, profileID, role)
	return err
}

func (r *TeamRepo) RemoveMember(ctx context.Context, teamID, profileID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM team_members WHERE team_id = $1 AND profile_id = $2`, teamID, profileID)
	return err
}

func (r *TeamRepo) ListMembers(ctx context.Context, teamID string) ([]*TeamMember, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT team_id, profile_id, role, added_at
		FROM team_members WHERE team_id = $1 ORDER BY added_at`, teamID)
	if err != nil {
		return nil, fmt.Errorf("list team members: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*TeamMember, error) {
		var m TeamMember
		err := row.Scan(&m.TeamID, &m.ProfileID, &m.Role, &m.AddedAt)
		return &m, err
	})
}
