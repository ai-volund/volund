package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// UserCredential is an encrypted credential stored for a user in a tenant.
type UserCredential struct {
	ID             string
	TenantID       string
	UserID         string
	Provider       string
	EncryptedToken []byte
	Scopes         []string
	TokenType      string // oauth2, api_key, pat
	ExpiresAt      *time.Time
	RefreshToken   []byte
	Metadata       []byte // JSON
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CredentialAuditEntry records a credential access event.
type CredentialAuditEntry struct {
	ID              string   `json:"id"`
	TenantID        string   `json:"tenant_id"`
	UserID          string   `json:"user_id"`
	Provider        string   `json:"provider"`
	AgentInstanceID string   `json:"agent_instance_id,omitempty"`
	ConversationID  string   `json:"conversation_id,omitempty"`
	SkillName       string   `json:"skill_name,omitempty"`
	Action          string   `json:"action"`
	ScopesRequested []string `json:"scopes_requested,omitempty"`
	ScopesGranted   []string `json:"scopes_granted,omitempty"`
	IPAddress       string   `json:"ip_address,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

// CredentialRepo handles credential persistence.
type CredentialRepo struct{ pool *Pool }

// NewCredentialRepo creates a CredentialRepo.
func NewCredentialRepo(pool *Pool) *CredentialRepo { return &CredentialRepo{pool: pool} }

// Store inserts or updates a user credential (upsert by tenant+user+provider).
func (r *CredentialRepo) Store(ctx context.Context, c *UserCredential) (*UserCredential, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO user_credentials (tenant_id, user_id, provider, encrypted_token, scopes, token_type, expires_at, refresh_token, metadata)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (tenant_id, user_id, provider)
		 DO UPDATE SET encrypted_token = $4, scopes = $5, token_type = $6, expires_at = $7,
		              refresh_token = $8, metadata = $9, updated_at = NOW()
		 RETURNING id, created_at, updated_at`,
		c.TenantID, c.UserID, c.Provider, c.EncryptedToken, c.Scopes,
		c.TokenType, c.ExpiresAt, c.RefreshToken, c.Metadata,
	)
	if err := row.Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, fmt.Errorf("store credential: %w", err)
	}
	return c, nil
}

// Get retrieves a credential by tenant, user, and provider.
func (r *CredentialRepo) Get(ctx context.Context, tenantID, userID, provider string) (*UserCredential, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, tenant_id, user_id, provider, encrypted_token, scopes, token_type,
		        expires_at, refresh_token, metadata, created_at, updated_at
		 FROM user_credentials
		 WHERE tenant_id = $1 AND user_id = $2 AND provider = $3`,
		tenantID, userID, provider,
	)
	return scanCredential(row)
}

// ListByUser returns all credentials for a user within a tenant.
func (r *CredentialRepo) ListByUser(ctx context.Context, tenantID, userID string) ([]*UserCredential, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, tenant_id, user_id, provider, encrypted_token, scopes, token_type,
		        expires_at, refresh_token, metadata, created_at, updated_at
		 FROM user_credentials
		 WHERE tenant_id = $1 AND user_id = $2
		 ORDER BY provider`,
		tenantID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*UserCredential, error) {
		return scanCredentialRow(row)
	})
}

// Delete removes a credential.
func (r *CredentialRepo) Delete(ctx context.Context, tenantID, userID, provider string) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM user_credentials WHERE tenant_id = $1 AND user_id = $2 AND provider = $3`,
		tenantID, userID, provider,
	)
	if err != nil {
		return fmt.Errorf("delete credential: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("credential not found")
	}
	return nil
}

// AuditLog records a credential access event.
func (r *CredentialRepo) AuditLog(ctx context.Context, e *CredentialAuditEntry) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO credential_audit_log
		 (tenant_id, user_id, provider, agent_instance_id, conversation_id, skill_name,
		  action, scopes_requested, scopes_granted, ip_address)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		e.TenantID, e.UserID, e.Provider, e.AgentInstanceID, nilIfEmpty(e.ConversationID),
		e.SkillName, e.Action, e.ScopesRequested, e.ScopesGranted, e.IPAddress,
	)
	if err != nil {
		return fmt.Errorf("audit log: %w", err)
	}
	return nil
}

// ListAuditLog returns recent audit entries for a tenant.
func (r *CredentialRepo) ListAuditLog(ctx context.Context, tenantID string, limit int) ([]*CredentialAuditEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, tenant_id, user_id, provider, agent_instance_id, conversation_id,
		        skill_name, action, scopes_requested, scopes_granted, ip_address, created_at
		 FROM credential_audit_log
		 WHERE tenant_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2`,
		tenantID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list audit log: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*CredentialAuditEntry, error) {
		var e CredentialAuditEntry
		var convID *string
		err := row.Scan(&e.ID, &e.TenantID, &e.UserID, &e.Provider, &e.AgentInstanceID,
			&convID, &e.SkillName, &e.Action, &e.ScopesRequested, &e.ScopesGranted,
			&e.IPAddress, &e.CreatedAt)
		if convID != nil {
			e.ConversationID = *convID
		}
		return &e, err
	})
}

func scanCredential(row pgx.Row) (*UserCredential, error) {
	var c UserCredential
	err := row.Scan(&c.ID, &c.TenantID, &c.UserID, &c.Provider, &c.EncryptedToken,
		&c.Scopes, &c.TokenType, &c.ExpiresAt, &c.RefreshToken, &c.Metadata,
		&c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, fmt.Errorf("credential not found")
		}
		return nil, fmt.Errorf("scan credential: %w", err)
	}
	return &c, nil
}

func scanCredentialRow(row pgx.CollectableRow) (*UserCredential, error) {
	var c UserCredential
	err := row.Scan(&c.ID, &c.TenantID, &c.UserID, &c.Provider, &c.EncryptedToken,
		&c.Scopes, &c.TokenType, &c.ExpiresAt, &c.RefreshToken, &c.Metadata,
		&c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("scan credential: %w", err)
	}
	return &c, nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
