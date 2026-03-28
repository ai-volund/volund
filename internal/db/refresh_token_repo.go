package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// RefreshToken represents a stored refresh token record.
type RefreshToken struct {
	ID        string
	UserID    string
	TenantID  string
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
	RevokedAt *time.Time
}

// RefreshTokenRepo handles refresh token persistence.
type RefreshTokenRepo struct{ pool *Pool }

// NewRefreshTokenRepo creates a RefreshTokenRepo.
func NewRefreshTokenRepo(pool *Pool) *RefreshTokenRepo { return &RefreshTokenRepo{pool: pool} }

// Create stores a new refresh token record. tokenHash is a bcrypt hash of the raw token.
func (r *RefreshTokenRepo) Create(ctx context.Context, userID, tenantID, tokenHash string, ttl time.Duration) (*RefreshToken, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO refresh_tokens (user_id, tenant_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id, user_id, tenant_id, token_hash, expires_at, created_at, revoked_at
	`, userID, tenantID, tokenHash, time.Now().Add(ttl))
	return scanRefreshToken(row)
}

// GetByHash looks up a refresh token by its hash. Returns nil if not found/revoked/expired.
func (r *RefreshTokenRepo) GetByHash(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, user_id, tenant_id, token_hash, expires_at, created_at, revoked_at
		FROM refresh_tokens
		WHERE token_hash = $1
		  AND revoked_at IS NULL
		  AND expires_at > NOW()
	`, tokenHash)
	rt, err := scanRefreshToken(row)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return rt, nil
}

// Revoke marks a refresh token as revoked.
func (r *RefreshTokenRepo) Revoke(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = NOW() WHERE id = $1
	`, id)
	return err
}

// RevokeAllForUser revokes all refresh tokens for a user (e.g., on password change).
func (r *RefreshTokenRepo) RevokeAllForUser(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = NOW()
		WHERE user_id = $1 AND revoked_at IS NULL
	`, userID)
	return err
}

func scanRefreshToken(row pgx.Row) (*RefreshToken, error) {
	rt := &RefreshToken{}
	err := row.Scan(
		&rt.ID, &rt.UserID, &rt.TenantID, &rt.TokenHash,
		&rt.ExpiresAt, &rt.CreatedAt, &rt.RevokedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan refresh token: %w", err)
	}
	return rt, nil
}

// isNotFound returns true if the error is a pgx no-rows error.
func isNotFound(err error) bool {
	return err == pgx.ErrNoRows || fmt.Sprintf("%v", err) == "scan refresh token: no rows in result set"
}
