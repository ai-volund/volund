package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Schedule represents a cron-based agent workflow.
type Schedule struct {
	ID              string          `json:"id"`
	TenantID        string          `json:"tenant_id"`
	UserID          string          `json:"user_id"`
	Name            string          `json:"name"`
	CronExpression  string          `json:"cron_expression"`
	AgentProfileID  *string         `json:"agent_profile_id"`
	Prompt          string          `json:"prompt"`
	DeliveryMethod  string          `json:"delivery_method"`
	DeliveryConfig  json.RawMessage `json:"delivery_config"`
	Enabled         bool            `json:"enabled"`
	LastRunAt       *time.Time      `json:"last_run_at"`
	NextRunAt       *time.Time      `json:"next_run_at"`
	LastRunStatus   *string         `json:"last_run_status"`
	LastRunError    *string         `json:"last_run_error"`
	RunCount        int             `json:"run_count"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type ScheduleRepo struct{ pool *Pool }

func NewScheduleRepo(pool *Pool) *ScheduleRepo {
	return &ScheduleRepo{pool: pool}
}

func (r *ScheduleRepo) Create(ctx context.Context, s *Schedule) (*Schedule, error) {
	if s.DeliveryConfig == nil {
		s.DeliveryConfig = json.RawMessage("{}")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO schedules (tenant_id, user_id, name, cron_expression, agent_profile_id, prompt, delivery_method, delivery_config, enabled, next_run_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at, updated_at`,
		s.TenantID, s.UserID, s.Name, s.CronExpression, s.AgentProfileID, s.Prompt, s.DeliveryMethod, s.DeliveryConfig, s.Enabled, s.NextRunAt)
	if err := row.Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, fmt.Errorf("create schedule: %w", err)
	}
	return s, nil
}

func (r *ScheduleRepo) Get(ctx context.Context, id string) (*Schedule, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, user_id, name, cron_expression, agent_profile_id, prompt,
		       delivery_method, delivery_config, enabled, last_run_at, next_run_at,
		       last_run_status, last_run_error, run_count, created_at, updated_at
		FROM schedules WHERE id = $1`, id)
	return scanSchedule(row)
}

func (r *ScheduleRepo) ListByUser(ctx context.Context, tenantID, userID string) ([]*Schedule, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, user_id, name, cron_expression, agent_profile_id, prompt,
		       delivery_method, delivery_config, enabled, last_run_at, next_run_at,
		       last_run_status, last_run_error, run_count, created_at, updated_at
		FROM schedules WHERE tenant_id = $1 AND user_id = $2 ORDER BY created_at DESC`, tenantID, userID)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Schedule, error) {
		return scanSchedule(row)
	})
}

func (r *ScheduleRepo) ListAll(ctx context.Context) ([]*Schedule, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, user_id, name, cron_expression, agent_profile_id, prompt,
		       delivery_method, delivery_config, enabled, last_run_at, next_run_at,
		       last_run_status, last_run_error, run_count, created_at, updated_at
		FROM schedules ORDER BY next_run_at ASC NULLS LAST`)
	if err != nil {
		return nil, fmt.Errorf("list all schedules: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Schedule, error) {
		return scanSchedule(row)
	})
}

// ListDue returns enabled schedules whose next_run_at is in the past.
func (r *ScheduleRepo) ListDue(ctx context.Context) ([]*Schedule, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, user_id, name, cron_expression, agent_profile_id, prompt,
		       delivery_method, delivery_config, enabled, last_run_at, next_run_at,
		       last_run_status, last_run_error, run_count, created_at, updated_at
		FROM schedules WHERE enabled = true AND next_run_at <= NOW()
		ORDER BY next_run_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list due schedules: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Schedule, error) {
		return scanSchedule(row)
	})
}

func (r *ScheduleRepo) Update(ctx context.Context, s *Schedule) (*Schedule, error) {
	if s.DeliveryConfig == nil {
		s.DeliveryConfig = json.RawMessage("{}")
	}
	row := r.pool.QueryRow(ctx, `
		UPDATE schedules SET name = $2, cron_expression = $3, agent_profile_id = $4, prompt = $5,
		       delivery_method = $6, delivery_config = $7, enabled = $8, next_run_at = $9, updated_at = NOW()
		WHERE id = $1
		RETURNING id, tenant_id, user_id, name, cron_expression, agent_profile_id, prompt,
		          delivery_method, delivery_config, enabled, last_run_at, next_run_at,
		          last_run_status, last_run_error, run_count, created_at, updated_at`,
		s.ID, s.Name, s.CronExpression, s.AgentProfileID, s.Prompt, s.DeliveryMethod, s.DeliveryConfig, s.Enabled, s.NextRunAt)
	return scanSchedule(row)
}

func (r *ScheduleRepo) MarkRun(ctx context.Context, id, status string, runErr *string, nextRun *time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE schedules SET last_run_at = NOW(), last_run_status = $2, last_run_error = $3,
		       next_run_at = $4, run_count = run_count + 1, updated_at = NOW()
		WHERE id = $1`, id, status, runErr, nextRun)
	return err
}

func (r *ScheduleRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM schedules WHERE id = $1`, id)
	return err
}

func scanSchedule(row pgx.Row) (*Schedule, error) {
	var s Schedule
	err := row.Scan(&s.ID, &s.TenantID, &s.UserID, &s.Name, &s.CronExpression, &s.AgentProfileID,
		&s.Prompt, &s.DeliveryMethod, &s.DeliveryConfig, &s.Enabled, &s.LastRunAt, &s.NextRunAt,
		&s.LastRunStatus, &s.LastRunError, &s.RunCount, &s.CreatedAt, &s.UpdatedAt)
	return &s, err
}
