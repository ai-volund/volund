package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// AgentProfile is a named agent configuration.
type AgentProfile struct {
	ID            string
	TenantID      string
	Name          string
	ProfileType   string
	SystemPrompt  string
	ModelProvider string
	ModelID       string
	Temperature   float64
	MaxTokens     int
	Skills        json.RawMessage
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// AgentInstance is a running or warm-pool agent pod.
type AgentInstance struct {
	ID             string
	TenantID       string
	ProfileID      *string
	PodName        *string
	PodNamespace   *string
	State          string // active, warm, cold
	ClaimedAt      *time.Time
	ReleasedAt     *time.Time
	LastHeartbeat  *time.Time
	CreatedAt      time.Time
}

// AgentRepo handles agent profile and instance persistence.
type AgentRepo struct{ pool *Pool }

// NewAgentRepo creates an AgentRepo.
func NewAgentRepo(pool *Pool) *AgentRepo { return &AgentRepo{pool: pool} }

// ── Profiles ──────────────────────────────────────────────────────────────────

func (r *AgentRepo) CreateProfile(ctx context.Context, p *AgentProfile) (*AgentProfile, error) {
	skills := p.Skills
	if skills == nil {
		skills = json.RawMessage("[]")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO agent_profiles
		  (tenant_id, name, profile_type, system_prompt, model_provider, model_id,
		   temperature, max_tokens, skills)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id, tenant_id, name, profile_type, system_prompt, model_provider,
		          model_id, temperature, max_tokens, skills, created_at, updated_at
	`, p.TenantID, p.Name, p.ProfileType, p.SystemPrompt, p.ModelProvider, p.ModelID,
		p.Temperature, p.MaxTokens, skills)
	return scanProfile(row)
}

func (r *AgentRepo) GetProfile(ctx context.Context, id string) (*AgentProfile, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, profile_type, system_prompt, model_provider,
		       model_id, temperature, max_tokens, skills, created_at, updated_at
		FROM agent_profiles WHERE id = $1
	`, id)
	return scanProfile(row)
}

func (r *AgentRepo) ListProfiles(ctx context.Context, tenantID string) ([]*AgentProfile, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, name, profile_type, system_prompt, model_provider,
		       model_id, temperature, max_tokens, skills, created_at, updated_at
		FROM agent_profiles WHERE tenant_id = $1 ORDER BY created_at
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*AgentProfile, error) {
		return scanProfile(row)
	})
}

// ── Instances ─────────────────────────────────────────────────────────────────

func (r *AgentRepo) CreateInstance(ctx context.Context, tenantID string, profileID *string) (*AgentInstance, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO agent_instances (tenant_id, profile_id, state)
		VALUES ($1, $2, 'cold')
		RETURNING id, tenant_id, profile_id, pod_name, pod_namespace, state,
		          claimed_at, released_at, last_heartbeat, created_at
	`, tenantID, profileID)
	return scanInstance(row)
}

// ClaimInstance atomically claims the oldest warm instance for a tenant+profile.
// Returns nil if no warm instance is available.
func (r *AgentRepo) ClaimInstance(ctx context.Context, tenantID, profileID string) (*AgentInstance, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE agent_instances
		SET state = 'active', claimed_at = NOW()
		WHERE id = (
			SELECT id FROM agent_instances
			WHERE tenant_id = $1 AND profile_id = $2 AND state = 'warm'
			ORDER BY last_heartbeat DESC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, tenant_id, profile_id, pod_name, pod_namespace, state,
		          claimed_at, released_at, last_heartbeat, created_at
	`, tenantID, profileID)
	inst, err := scanInstance(row)
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return inst, nil
}

func (r *AgentRepo) ReleaseInstance(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE agent_instances
		SET state = 'warm', released_at = NOW(), claimed_at = NULL
		WHERE id = $1
	`, id)
	return err
}

func (r *AgentRepo) Heartbeat(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE agent_instances SET last_heartbeat = NOW() WHERE id = $1
	`, id)
	return err
}

func (r *AgentRepo) SetPod(ctx context.Context, id, podName, namespace string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE agent_instances
		SET pod_name = $2, pod_namespace = $3, state = 'warm', last_heartbeat = NOW()
		WHERE id = $1
	`, id, podName, namespace)
	return err
}

func (r *AgentRepo) GetInstance(ctx context.Context, id string) (*AgentInstance, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, profile_id, pod_name, pod_namespace, state,
		       claimed_at, released_at, last_heartbeat, created_at
		FROM agent_instances WHERE id = $1
	`, id)
	return scanInstance(row)
}

// ── Scan helpers ──────────────────────────────────────────────────────────────

func scanProfile(row pgx.Row) (*AgentProfile, error) {
	p := &AgentProfile{}
	err := row.Scan(
		&p.ID, &p.TenantID, &p.Name, &p.ProfileType, &p.SystemPrompt,
		&p.ModelProvider, &p.ModelID, &p.Temperature, &p.MaxTokens,
		&p.Skills, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan agent profile: %w", err)
	}
	return p, nil
}

func scanInstance(row pgx.Row) (*AgentInstance, error) {
	i := &AgentInstance{}
	err := row.Scan(
		&i.ID, &i.TenantID, &i.ProfileID, &i.PodName, &i.PodNamespace,
		&i.State, &i.ClaimedAt, &i.ReleasedAt, &i.LastHeartbeat, &i.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan agent instance: %w", err)
	}
	return i, nil
}

// isNoRows returns true when pgx reports no rows found.
func isNoRows(err error) bool {
	return err != nil && err.Error() == "no rows in result set"
}
