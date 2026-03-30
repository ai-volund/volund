package db

import (
	"context"
	"fmt"
	"time"
)

// TenantQuota represents the quota configuration for a tenant.
type TenantQuota struct {
	ID            string
	TenantID      string
	MaxTokens     *int64   // nil = unlimited
	MaxCostUSD    *float64 // nil = unlimited
	PeriodStart   time.Time
	PeriodEnd     time.Time
	OnLimitAction string // "alert", "queue", "block"
	AlertWebhook  string
	Alerted80     bool
	Alerted90     bool
	Alerted100    bool
}

// QuotaStatus holds current usage against quota limits.
type QuotaStatus struct {
	TenantID    string   `json:"tenant_id"`
	MaxTokens   *int64   `json:"max_tokens"`
	MaxCostUSD  *float64 `json:"max_cost_usd"`
	UsedTokens  int64    `json:"used_tokens"`
	UsedCostUSD float64  `json:"used_cost_usd"`
	PctTokens   float64  `json:"pct_tokens"`   // 0-100
	PctCost     float64  `json:"pct_cost"`      // 0-100
	Action      string   `json:"on_limit_action"`
	Exceeded    bool     `json:"exceeded"`
	Message     string   `json:"message,omitempty"`
}

// QuotaRepo handles tenant quota operations.
type QuotaRepo struct{ pool *Pool }

// NewQuotaRepo creates a QuotaRepo.
func NewQuotaRepo(pool *Pool) *QuotaRepo { return &QuotaRepo{pool: pool} }

// Get returns the quota for a tenant, or nil if none is configured.
func (r *QuotaRepo) Get(ctx context.Context, tenantID string) (*TenantQuota, error) {
	var q TenantQuota
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, max_tokens, max_cost_usd, period_start, period_end,
		       on_limit_action, COALESCE(alert_webhook_url, ''),
		       alerted_80, alerted_90, alerted_100
		FROM tenant_quotas WHERE tenant_id = $1
	`, tenantID).Scan(
		&q.ID, &q.TenantID, &q.MaxTokens, &q.MaxCostUSD,
		&q.PeriodStart, &q.PeriodEnd, &q.OnLimitAction, &q.AlertWebhook,
		&q.Alerted80, &q.Alerted90, &q.Alerted100,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil // no quota configured
		}
		return nil, fmt.Errorf("get quota: %w", err)
	}
	return &q, nil
}

// Upsert creates or updates a tenant's quota configuration.
func (r *QuotaRepo) Upsert(ctx context.Context, q *TenantQuota) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO tenant_quotas (tenant_id, max_tokens, max_cost_usd, period_start, period_end, on_limit_action, alert_webhook_url)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (tenant_id) DO UPDATE SET
			max_tokens = EXCLUDED.max_tokens,
			max_cost_usd = EXCLUDED.max_cost_usd,
			period_start = EXCLUDED.period_start,
			period_end = EXCLUDED.period_end,
			on_limit_action = EXCLUDED.on_limit_action,
			alert_webhook_url = EXCLUDED.alert_webhook_url,
			updated_at = NOW()
	`, q.TenantID, q.MaxTokens, q.MaxCostUSD, q.PeriodStart, q.PeriodEnd, q.OnLimitAction, q.AlertWebhook)
	if err != nil {
		return fmt.Errorf("upsert quota: %w", err)
	}
	return nil
}

// MarkAlerted marks a threshold as alerted for the current period.
func (r *QuotaRepo) MarkAlerted(ctx context.Context, tenantID string, threshold int) error {
	col := ""
	switch threshold {
	case 80:
		col = "alerted_80"
	case 90:
		col = "alerted_90"
	case 100:
		col = "alerted_100"
	default:
		return fmt.Errorf("invalid threshold: %d", threshold)
	}
	_, err := r.pool.Exec(ctx, fmt.Sprintf(`
		UPDATE tenant_quotas SET %s = true, updated_at = NOW() WHERE tenant_id = $1
	`, col), tenantID)
	return err
}

// CheckQuota compares current usage against the tenant's quota limits.
// Returns nil if no quota is configured (unlimited).
func (r *QuotaRepo) CheckQuota(ctx context.Context, tenantID string) (*QuotaStatus, error) {
	q, err := r.Get(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if q == nil {
		return nil, nil // no quota = unlimited
	}

	// Get current period usage.
	usageRepo := NewUsageRepo(r.pool)
	summary, err := usageRepo.SummaryByTenant(ctx, tenantID, q.PeriodStart, q.PeriodEnd)
	if err != nil {
		return nil, fmt.Errorf("check quota usage: %w", err)
	}

	status := &QuotaStatus{
		TenantID:    tenantID,
		MaxTokens:   q.MaxTokens,
		MaxCostUSD:  q.MaxCostUSD,
		UsedTokens:  int64(summary.TotalTokens),
		UsedCostUSD: summary.TotalCostUSD,
		Action:      q.OnLimitAction,
	}

	if q.MaxTokens != nil && *q.MaxTokens > 0 {
		status.PctTokens = float64(summary.TotalTokens) / float64(*q.MaxTokens) * 100
		if status.PctTokens >= 100 {
			status.Exceeded = true
			status.Message = fmt.Sprintf("token limit reached (%d / %d)", summary.TotalTokens, *q.MaxTokens)
		}
	}
	if q.MaxCostUSD != nil && *q.MaxCostUSD > 0 {
		status.PctCost = summary.TotalCostUSD / *q.MaxCostUSD * 100
		if status.PctCost >= 100 {
			status.Exceeded = true
			status.Message = fmt.Sprintf("cost limit reached ($%.2f / $%.2f)", summary.TotalCostUSD, *q.MaxCostUSD)
		}
	}

	return status, nil
}
