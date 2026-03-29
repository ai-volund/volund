package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ai-volund/volund/internal/db"
)

// QuotaEnforcer checks tenant usage against configured quotas and fires
// webhook alerts at 80%, 90%, and 100% thresholds. Used by the usage
// tracker after recording events and by the gateway before dispatching tasks.
type QuotaEnforcer struct {
	quotas *db.QuotaRepo
	client *http.Client
}

// NewQuotaEnforcer creates a QuotaEnforcer.
func NewQuotaEnforcer(quotas *db.QuotaRepo) *QuotaEnforcer {
	return &QuotaEnforcer{
		quotas: quotas,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// CheckAndAlert checks the tenant's current usage against their quota and
// fires webhook alerts for any newly crossed thresholds.
// Returns the quota status (nil if no quota configured).
func (qe *QuotaEnforcer) CheckAndAlert(ctx context.Context, tenantID string) (*db.QuotaStatus, error) {
	status, err := qe.quotas.CheckQuota(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("check quota: %w", err)
	}
	if status == nil {
		return nil, nil // no quota
	}

	// Determine the highest percentage across all dimensions.
	pct := status.PctTokens
	if status.PctCost > pct {
		pct = status.PctCost
	}

	// Load quota to check which thresholds have already been alerted.
	quota, err := qe.quotas.Get(ctx, tenantID)
	if err != nil || quota == nil {
		return status, nil
	}

	// Fire alerts for newly crossed thresholds.
	thresholds := []struct {
		level   int
		alerted bool
	}{
		{80, quota.Alerted80},
		{90, quota.Alerted90},
		{100, quota.Alerted100},
	}

	for _, t := range thresholds {
		if pct >= float64(t.level) && !t.alerted {
			slog.Warn("quota threshold crossed",
				"tenant_id", tenantID,
				"threshold", t.level,
				"pct_tokens", status.PctTokens,
				"pct_cost", status.PctCost,
			)
			if quota.AlertWebhook != "" {
				go qe.fireWebhook(quota.AlertWebhook, tenantID, t.level, status)
			}
			if err := qe.quotas.MarkAlerted(ctx, tenantID, t.level); err != nil {
				slog.Warn("failed to mark threshold alerted", "error", err)
			}
		}
	}

	return status, nil
}

// IsBlocked returns true if the tenant has exceeded their quota and the
// enforcement action is "block". Used before dispatching tasks.
func (qe *QuotaEnforcer) IsBlocked(ctx context.Context, tenantID string) (bool, error) {
	status, err := qe.quotas.CheckQuota(ctx, tenantID)
	if err != nil {
		return false, err
	}
	if status == nil {
		return false, nil // no quota = not blocked
	}
	return status.Exceeded && status.Action == "block", nil
}

// WebhookPayload is the JSON payload sent to alert webhooks.
type WebhookPayload struct {
	TenantID    string  `json:"tenant_id"`
	Threshold   int     `json:"threshold_percent"`
	PctTokens   float64 `json:"usage_pct_tokens"`
	PctCost     float64 `json:"usage_pct_cost"`
	UsedTokens  int64   `json:"used_tokens"`
	UsedCostUSD float64 `json:"used_cost_usd"`
	Timestamp   string  `json:"timestamp"`
}

func (qe *QuotaEnforcer) fireWebhook(url, tenantID string, threshold int, status *db.QuotaStatus) {
	payload := WebhookPayload{
		TenantID:    tenantID,
		Threshold:   threshold,
		PctTokens:   status.PctTokens,
		PctCost:     status.PctCost,
		UsedTokens:  status.UsedTokens,
		UsedCostUSD: status.UsedCostUSD,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("quota webhook: marshal failed", "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		slog.Warn("quota webhook: request failed", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := qe.client.Do(req)
	if err != nil {
		slog.Warn("quota webhook: delivery failed", "url", url, "error", err)
		return
	}
	resp.Body.Close()

	slog.Info("quota webhook delivered",
		"tenant_id", tenantID,
		"threshold", threshold,
		"status", resp.StatusCode,
	)
}
