package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Invite represents an org_invites row.
type Invite struct {
	ID         string
	TenantID   string
	Email      string
	Role       string
	Token      string
	ExpiresAt  time.Time
	AcceptedAt *time.Time
	InvitedBy  *string
	CreatedAt  time.Time
}

// InviteRepo handles org invite persistence.
type InviteRepo struct{ pool *Pool }

// NewInviteRepo creates an InviteRepo.
func NewInviteRepo(pool *Pool) *InviteRepo { return &InviteRepo{pool: pool} }

// Create inserts a new invite.
func (r *InviteRepo) Create(ctx context.Context, tenantID, email, role, token string, ttl time.Duration, invitedBy *string) (*Invite, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO org_invites (tenant_id, email, role, token, expires_at, invited_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, tenant_id, email, role, token, expires_at, accepted_at, invited_by, created_at
	`, tenantID, email, role, token, time.Now().Add(ttl), invitedBy)
	return scanInvite(row)
}

// GetByToken looks up an invite by its token. Returns nil if not found/expired/accepted.
func (r *InviteRepo) GetByToken(ctx context.Context, token string) (*Invite, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, email, role, token, expires_at, accepted_at, invited_by, created_at
		FROM org_invites
		WHERE token = $1
		  AND accepted_at IS NULL
		  AND expires_at > NOW()
	`, token)
	inv, err := scanInvite(row)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return inv, nil
}

// Accept marks the invite as accepted.
func (r *InviteRepo) Accept(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE org_invites SET accepted_at = NOW() WHERE id = $1
	`, id)
	return err
}

// ListByTenant returns all pending invites for a tenant.
func (r *InviteRepo) ListByTenant(ctx context.Context, tenantID string) ([]*Invite, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, email, role, token, expires_at, accepted_at, invited_by, created_at
		FROM org_invites
		WHERE tenant_id = $1
		ORDER BY created_at DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Invite, error) {
		return scanInvite(row)
	})
}

func scanInvite(row pgx.Row) (*Invite, error) {
	i := &Invite{}
	err := row.Scan(
		&i.ID, &i.TenantID, &i.Email, &i.Role, &i.Token,
		&i.ExpiresAt, &i.AcceptedAt, &i.InvitedBy, &i.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan invite: %w", err)
	}
	return i, nil
}
