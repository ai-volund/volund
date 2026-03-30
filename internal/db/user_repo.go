package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// User represents a row in the users table.
type User struct {
	ID           string
	Email        string
	DisplayName  string
	PasswordHash *string
	OIDCSubject  *string
	OIDCProvider *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// UserRepo handles user persistence.
type UserRepo struct{ pool *Pool }

// NewUserRepo creates a UserRepo.
func NewUserRepo(pool *Pool) *UserRepo { return &UserRepo{pool: pool} }

func (r *UserRepo) Create(ctx context.Context, email, displayName string) (*User, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO users (email, display_name)
		VALUES ($1, $2)
		RETURNING id, email, display_name, password_hash, oidc_subject, oidc_provider, created_at, updated_at
	`, email, displayName)
	return scanUser(row)
}

func (r *UserRepo) GetByID(ctx context.Context, id string) (*User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, email, display_name, password_hash, oidc_subject, oidc_provider, created_at, updated_at
		FROM users WHERE id = $1
	`, id)
	return scanUser(row)
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, email, display_name, password_hash, oidc_subject, oidc_provider, created_at, updated_at
		FROM users WHERE email = $1
	`, email)
	return scanUser(row)
}

func (r *UserRepo) SetPasswordHash(ctx context.Context, id, hash string) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2`, hash, id)
	return err
}

func (r *UserRepo) AddToTenant(ctx context.Context, tenantID, userID, role string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO org_members (tenant_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (tenant_id, user_id) DO UPDATE SET role = EXCLUDED.role
	`, tenantID, userID, role)
	return err
}

// GetPrimaryMembership returns the first tenant and role for a user.
func (r *UserRepo) GetPrimaryMembership(ctx context.Context, userID string) (tenantID, role string, err error) {
	row := r.pool.QueryRow(ctx, `
		SELECT tenant_id, role FROM org_members WHERE user_id = $1 ORDER BY joined_at LIMIT 1
	`, userID)
	if scanErr := row.Scan(&tenantID, &role); scanErr != nil {
		return "", "member", nil // no membership yet
	}
	return tenantID, role, nil
}

func (r *UserRepo) ListByTenant(ctx context.Context, tenantID string) ([]*User, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT u.id, u.email, u.display_name, u.password_hash, u.oidc_subject, u.oidc_provider,
		       u.created_at, u.updated_at
		FROM users u
		JOIN org_members m ON m.user_id = u.id
		WHERE m.tenant_id = $1
		ORDER BY u.created_at
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("user list by tenant: %w", err)
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*User, error) {
		return scanUser(row)
	})
}

// FindOrCreateByOIDC looks up a user by OIDC provider + subject.
// If no user exists, creates one with the given email and display name.
// Returns the user and whether it was newly created.
func (r *UserRepo) FindOrCreateByOIDC(ctx context.Context, provider, subject, email, displayName string) (*User, bool, error) {
	// Try to find existing user by OIDC identity.
	row := r.pool.QueryRow(ctx, `
		SELECT id, email, display_name, password_hash, oidc_subject, oidc_provider, created_at, updated_at
		FROM users WHERE oidc_provider = $1 AND oidc_subject = $2
	`, provider, subject)
	u, err := scanUser(row)
	if err == nil {
		return u, false, nil
	}

	// Not found by OIDC — check if email already exists (link accounts).
	existing, err := r.GetByEmail(ctx, email)
	if err == nil && existing != nil {
		// Link OIDC identity to existing email-based account.
		_, err = r.pool.Exec(ctx, `
			UPDATE users SET oidc_provider = $1, oidc_subject = $2, updated_at = NOW()
			WHERE id = $3
		`, provider, subject, existing.ID)
		if err != nil {
			return nil, false, fmt.Errorf("link oidc to user: %w", err)
		}
		existing.OIDCProvider = &provider
		existing.OIDCSubject = &subject
		return existing, false, nil
	}

	// Create new user with OIDC identity.
	if displayName == "" {
		displayName = email
	}
	newRow := r.pool.QueryRow(ctx, `
		INSERT INTO users (email, display_name, oidc_provider, oidc_subject)
		VALUES ($1, $2, $3, $4)
		RETURNING id, email, display_name, password_hash, oidc_subject, oidc_provider, created_at, updated_at
	`, email, displayName, provider, subject)
	u, err = scanUser(newRow)
	if err != nil {
		return nil, false, fmt.Errorf("create oidc user: %w", err)
	}
	return u, true, nil
}

func scanUser(row pgx.Row) (*User, error) {
	u := &User{}
	err := row.Scan(
		&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash,
		&u.OIDCSubject, &u.OIDCProvider, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}
	return u, nil
}

// UpdateRole changes a user's role within a tenant.
func (r *UserRepo) UpdateRole(ctx context.Context, tenantID, userID, role string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE org_members SET role = $3 WHERE tenant_id = $1 AND user_id = $2`,
		tenantID, userID, role)
	if err != nil {
		return fmt.Errorf("update role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("membership not found")
	}
	return nil
}

// RemoveFromTenant removes a user's membership in a tenant.
func (r *UserRepo) RemoveFromTenant(ctx context.Context, tenantID, userID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM org_members WHERE tenant_id = $1 AND user_id = $2`,
		tenantID, userID)
	return err
}
