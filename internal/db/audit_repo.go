package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// AuditEntry represents a row in the audit_log table.
type AuditEntry struct {
	ID         string          `json:"id"`
	TenantID   string          `json:"tenant_id"`
	UserID     string          `json:"user_id"`
	Action     string          `json:"action"`
	Resource   string          `json:"resource"`
	ResourceID string          `json:"resource_id"`
	Detail     json.RawMessage `json:"detail"`
	IPAddress  string          `json:"ip_address"`
	UserAgent  string          `json:"user_agent"`
	CreatedAt  time.Time       `json:"created_at"`
}

// AuditRepo handles the audit_log table.
type AuditRepo struct{ pool *Pool }

// NewAuditRepo creates a new repo.
func NewAuditRepo(pool *Pool) *AuditRepo { return &AuditRepo{pool: pool} }

// Log writes an audit entry.
func (r *AuditRepo) Log(ctx context.Context, e *AuditEntry) error {
	if e.Detail == nil {
		e.Detail = json.RawMessage("{}")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO audit_log (tenant_id, user_id, action, resource, resource_id, detail, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		e.TenantID, e.UserID, e.Action, e.Resource, e.ResourceID, e.Detail, e.IPAddress, e.UserAgent)
	return err
}

// List returns recent audit entries, optionally filtered.
func (r *AuditRepo) List(ctx context.Context, limit int, tenantID string) ([]*AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows pgx.Rows
	var err error
	if tenantID != "" {
		rows, err = r.pool.Query(ctx, `
			SELECT id, tenant_id, user_id, action, resource, resource_id, detail, ip_address, user_agent, created_at
			FROM audit_log WHERE tenant_id = $1 ORDER BY created_at DESC LIMIT $2`, tenantID, limit)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT id, tenant_id, user_id, action, resource, resource_id, detail, ip_address, user_agent, created_at
			FROM audit_log ORDER BY created_at DESC LIMIT $1`, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*AuditEntry, error) {
		var e AuditEntry
		err := row.Scan(&e.ID, &e.TenantID, &e.UserID, &e.Action, &e.Resource, &e.ResourceID,
			&e.Detail, &e.IPAddress, &e.UserAgent, &e.CreatedAt)
		return &e, err
	})
}
