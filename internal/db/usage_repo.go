package db

import (
	"context"
	"fmt"
	"time"
)

// UsageEvent represents a row in the usage_events table.
type UsageEvent struct {
	ID             string
	TenantID       string
	ConversationID *string
	TaskID         string
	InstanceID     string
	Provider       string
	Model          string
	InputTokens    int
	OutputTokens   int
	TotalTokens    int
	CostUSD        *float64
	CreatedAt      time.Time
}

// UsageSummary holds aggregated usage for a tenant over a period.
type UsageSummary struct {
	TenantID     string   `json:"tenant_id"`
	TotalInput   int      `json:"total_input_tokens"`
	TotalOutput  int      `json:"total_output_tokens"`
	TotalTokens  int      `json:"total_tokens"`
	TotalCostUSD float64  `json:"total_cost_usd"`
	EventCount   int      `json:"event_count"`
}

// UsageRepo handles usage event persistence.
type UsageRepo struct{ pool *Pool }

// NewUsageRepo creates a UsageRepo.
func NewUsageRepo(pool *Pool) *UsageRepo { return &UsageRepo{pool: pool} }

// Record persists a usage event.
func (r *UsageRepo) Record(ctx context.Context, e *UsageEvent) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO usage_events (tenant_id, conversation_id, task_id, instance_id, provider, model, input_tokens, output_tokens, cost_usd)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, e.TenantID, e.ConversationID, e.TaskID, e.InstanceID, e.Provider, e.Model, e.InputTokens, e.OutputTokens, e.CostUSD)
	if err != nil {
		return fmt.Errorf("record usage event: %w", err)
	}
	return nil
}

// SummaryByTenant returns aggregated usage for a tenant within a time range.
func (r *UsageRepo) SummaryByTenant(ctx context.Context, tenantID string, from, to time.Time) (*UsageSummary, error) {
	var s UsageSummary
	s.TenantID = tenantID
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
		       COALESCE(SUM(total_tokens), 0), COALESCE(SUM(cost_usd), 0), COUNT(*)
		FROM usage_events
		WHERE tenant_id = $1 AND created_at >= $2 AND created_at < $3
	`, tenantID, from, to).Scan(&s.TotalInput, &s.TotalOutput, &s.TotalTokens, &s.TotalCostUSD, &s.EventCount)
	if err != nil {
		return nil, fmt.Errorf("usage summary: %w", err)
	}
	return &s, nil
}

// SummaryByConversation returns aggregated usage for a conversation.
func (r *UsageRepo) SummaryByConversation(ctx context.Context, convID string) (*UsageSummary, error) {
	var s UsageSummary
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
		       COALESCE(SUM(total_tokens), 0), COALESCE(SUM(cost_usd), 0), COUNT(*)
		FROM usage_events
		WHERE conversation_id = $1
	`, convID).Scan(&s.TotalInput, &s.TotalOutput, &s.TotalTokens, &s.TotalCostUSD, &s.EventCount)
	if err != nil {
		return nil, fmt.Errorf("usage summary by conv: %w", err)
	}
	return &s, nil
}
