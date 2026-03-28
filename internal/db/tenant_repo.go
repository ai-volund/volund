package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Tenant represents a row in the tenants table.
type Tenant struct {
	ID        string
	Name      string
	Slug      string
	Plan      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TenantRepo handles tenant persistence.
type TenantRepo struct{ pool *Pool }

// NewTenantRepo creates a TenantRepo.
func NewTenantRepo(pool *Pool) *TenantRepo { return &TenantRepo{pool: pool} }

func (r *TenantRepo) Create(ctx context.Context, name, slug, plan string) (*Tenant, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan)
		VALUES ($1, $2, $3)
		RETURNING id, name, slug, plan, created_at, updated_at
	`, name, slug, plan)
	return scanTenant(row)
}

func (r *TenantRepo) GetByID(ctx context.Context, id string) (*Tenant, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, slug, plan, created_at, updated_at
		FROM tenants WHERE id = $1
	`, id)
	return scanTenant(row)
}

func (r *TenantRepo) GetBySlug(ctx context.Context, slug string) (*Tenant, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, slug, plan, created_at, updated_at
		FROM tenants WHERE slug = $1
	`, slug)
	return scanTenant(row)
}

func (r *TenantRepo) Update(ctx context.Context, t *Tenant) (*Tenant, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE tenants SET name=$2, plan=$3, updated_at=NOW()
		WHERE id=$1
		RETURNING id, name, slug, plan, created_at, updated_at
	`, t.ID, t.Name, t.Plan)
	return scanTenant(row)
}

func (r *TenantRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, id)
	return err
}

func (r *TenantRepo) List(ctx context.Context) ([]*Tenant, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, slug, plan, created_at, updated_at
		FROM tenants ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("tenant list: %w", err)
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Tenant, error) {
		return scanTenant(row)
	})
}

func scanTenant(row pgx.Row) (*Tenant, error) {
	t := &Tenant{}
	err := row.Scan(&t.ID, &t.Name, &t.Slug, &t.Plan, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("scan tenant: %w", err)
	}
	return t, nil
}
