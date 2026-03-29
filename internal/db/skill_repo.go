package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// RegistrySkill is a skill published to the Forge registry.
type RegistrySkill struct {
	ID          string
	Name        string
	Version     string
	Description string
	Author      string
	Type        string // prompt, mcp, cli
	Tags        []string
	Spec        json.RawMessage
	Readme      string
	Downloads   int64
	Published   bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SkillRepo handles skill registry persistence.
type SkillRepo struct{ pool *Pool }

// NewSkillRepo creates a SkillRepo.
func NewSkillRepo(pool *Pool) *SkillRepo { return &SkillRepo{pool: pool} }

// Create inserts a new skill into the registry.
func (r *SkillRepo) Create(ctx context.Context, s *RegistrySkill) (*RegistrySkill, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO skills (name, version, description, author, type, tags, spec, readme, published)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id, created_at, updated_at`,
		s.Name, s.Version, s.Description, s.Author, s.Type, s.Tags, s.Spec, s.Readme, s.Published,
	)
	if err := row.Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, fmt.Errorf("create skill: %w", err)
	}
	return s, nil
}

// Get returns a single skill by ID.
func (r *SkillRepo) Get(ctx context.Context, id string) (*RegistrySkill, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, name, version, description, author, type, tags, spec, readme,
		        downloads, published, created_at, updated_at
		 FROM skills WHERE id = $1`, id)
	return scanSkill(row)
}

// GetByName returns a single skill by name.
func (r *SkillRepo) GetByName(ctx context.Context, name string) (*RegistrySkill, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, name, version, description, author, type, tags, spec, readme,
		        downloads, published, created_at, updated_at
		 FROM skills WHERE name = $1`, name)
	return scanSkill(row)
}

// List returns all published skills, optionally filtered.
func (r *SkillRepo) List(ctx context.Context, filter SkillFilter) ([]*RegistrySkill, error) {
	query := `SELECT id, name, version, description, author, type, tags, spec, readme,
	                 downloads, published, created_at, updated_at
	          FROM skills WHERE published = true`
	args := []any{}
	argN := 1

	if filter.Type != "" {
		query += fmt.Sprintf(" AND type = $%d", argN)
		args = append(args, filter.Type)
		argN++
	}
	if filter.Author != "" {
		query += fmt.Sprintf(" AND author = $%d", argN)
		args = append(args, filter.Author)
		argN++
	}
	if filter.Tag != "" {
		query += fmt.Sprintf(" AND $%d = ANY(tags)", argN)
		args = append(args, filter.Tag)
		argN++
	}
	if filter.Query != "" {
		query += fmt.Sprintf(" AND (name ILIKE $%d OR description ILIKE $%d)", argN, argN)
		args = append(args, "%"+filter.Query+"%")
		argN++
	}

	query += " ORDER BY downloads DESC, name ASC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	} else {
		query += " LIMIT 100"
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", filter.Offset)
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*RegistrySkill, error) {
		return scanSkillRow(row)
	})
}

// Update modifies an existing skill.
func (r *SkillRepo) Update(ctx context.Context, s *RegistrySkill) (*RegistrySkill, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE skills SET version=$1, description=$2, author=$3, type=$4, tags=$5,
		        spec=$6, readme=$7, published=$8, updated_at=NOW()
		 WHERE id = $9
		 RETURNING updated_at`,
		s.Version, s.Description, s.Author, s.Type, s.Tags, s.Spec, s.Readme, s.Published, s.ID,
	)
	if err := row.Scan(&s.UpdatedAt); err != nil {
		if err.Error() == "no rows in result set" {
			return nil, fmt.Errorf("skill not found")
		}
		return nil, fmt.Errorf("update skill: %w", err)
	}
	return s, nil
}

// Delete removes a skill from the registry.
func (r *SkillRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM skills WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete skill: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("skill not found")
	}
	return nil
}

// IncrementDownloads bumps the download counter.
func (r *SkillRepo) IncrementDownloads(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE skills SET downloads = downloads + 1 WHERE id = $1`, id)
	return err
}

// SkillFilter holds optional filters for skill listing.
type SkillFilter struct {
	Type   string // prompt, mcp, cli
	Author string
	Tag    string
	Query  string // free-text search on name + description
	Limit  int
	Offset int
}

// ── Tenant-level skill installation ──────────────────────────────────────────

// InstallForTenant marks a skill as available for a tenant's users.
func (r *SkillRepo) InstallForTenant(ctx context.Context, tenantID, skillID string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO tenant_skills (tenant_id, skill_id) VALUES ($1, $2)
		 ON CONFLICT DO NOTHING`, tenantID, skillID)
	return err
}

// UninstallForTenant removes a skill from a tenant (also removes user enablements).
func (r *SkillRepo) UninstallForTenant(ctx context.Context, tenantID, skillID string) error {
	// Remove user enablements first, then tenant install.
	_, _ = r.pool.Exec(ctx,
		`DELETE FROM user_skills WHERE tenant_id = $1 AND skill_id = $2`, tenantID, skillID)
	_, err := r.pool.Exec(ctx,
		`DELETE FROM tenant_skills WHERE tenant_id = $1 AND skill_id = $2`, tenantID, skillID)
	return err
}

// ListTenantSkills returns all skills installed for a tenant.
func (r *SkillRepo) ListTenantSkills(ctx context.Context, tenantID string) ([]*RegistrySkill, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT s.id, s.name, s.version, s.description, s.author, s.type, s.tags, s.spec,
		        s.readme, s.downloads, s.published, s.created_at, s.updated_at
		 FROM skills s JOIN tenant_skills ts ON s.id = ts.skill_id
		 WHERE ts.tenant_id = $1 ORDER BY s.name`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list tenant skills: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*RegistrySkill, error) {
		return scanSkillRow(row)
	})
}

// ── User-level skill enablement ─────────────────────────────────────────────

// EnableForUser enables a tenant-installed skill for a specific user.
func (r *SkillRepo) EnableForUser(ctx context.Context, tenantID, userID, skillID string) error {
	// Verify the skill is installed at tenant level first.
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM tenant_skills WHERE tenant_id = $1 AND skill_id = $2)`,
		tenantID, skillID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check tenant install: %w", err)
	}
	if !exists {
		return fmt.Errorf("skill not installed for this tenant")
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO user_skills (tenant_id, user_id, skill_id) VALUES ($1, $2, $3)
		 ON CONFLICT DO NOTHING`, tenantID, userID, skillID)
	return err
}

// DisableForUser disables a skill for a specific user.
func (r *SkillRepo) DisableForUser(ctx context.Context, tenantID, userID, skillID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM user_skills WHERE tenant_id = $1 AND user_id = $2 AND skill_id = $3`,
		tenantID, userID, skillID)
	return err
}

// ListActiveSkills returns skills that are BOTH tenant-installed AND user-enabled.
// This is the dispatch-time query — only these skills get wired into the agent.
func (r *SkillRepo) ListActiveSkills(ctx context.Context, tenantID, userID string) ([]*RegistrySkill, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT s.id, s.name, s.version, s.description, s.author, s.type, s.tags, s.spec,
		        s.readme, s.downloads, s.published, s.created_at, s.updated_at
		 FROM skills s
		 JOIN tenant_skills ts ON s.id = ts.skill_id AND ts.tenant_id = $1
		 JOIN user_skills us ON s.id = us.skill_id AND us.tenant_id = $1 AND us.user_id = $2
		 ORDER BY s.name`, tenantID, userID)
	if err != nil {
		return nil, fmt.Errorf("list active skills: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*RegistrySkill, error) {
		return scanSkillRow(row)
	})
}

// ListAvailableSkills returns tenant-installed skills with an `enabled` flag per user.
// Used by the UI to show all available skills with toggle state.
type AvailableSkill struct {
	*RegistrySkill
	Enabled bool
}

func (r *SkillRepo) ListAvailableSkills(ctx context.Context, tenantID, userID string) ([]*AvailableSkill, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT s.id, s.name, s.version, s.description, s.author, s.type, s.tags, s.spec,
		        s.readme, s.downloads, s.published, s.created_at, s.updated_at,
		        (us.user_id IS NOT NULL) AS enabled
		 FROM skills s
		 JOIN tenant_skills ts ON s.id = ts.skill_id AND ts.tenant_id = $1
		 LEFT JOIN user_skills us ON s.id = us.skill_id AND us.tenant_id = $1 AND us.user_id = $2
		 ORDER BY s.name`, tenantID, userID)
	if err != nil {
		return nil, fmt.Errorf("list available skills: %w", err)
	}
	defer rows.Close()

	var result []*AvailableSkill
	for rows.Next() {
		var s RegistrySkill
		var enabled bool
		err := rows.Scan(&s.ID, &s.Name, &s.Version, &s.Description, &s.Author, &s.Type,
			&s.Tags, &s.Spec, &s.Readme, &s.Downloads, &s.Published, &s.CreatedAt, &s.UpdatedAt,
			&enabled)
		if err != nil {
			return nil, fmt.Errorf("scan available skill: %w", err)
		}
		result = append(result, &AvailableSkill{RegistrySkill: &s, Enabled: enabled})
	}
	return result, nil
}

func scanSkill(row pgx.Row) (*RegistrySkill, error) {
	var s RegistrySkill
	err := row.Scan(&s.ID, &s.Name, &s.Version, &s.Description, &s.Author, &s.Type,
		&s.Tags, &s.Spec, &s.Readme, &s.Downloads, &s.Published, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, fmt.Errorf("skill not found")
		}
		return nil, fmt.Errorf("scan skill: %w", err)
	}
	return &s, nil
}

func scanSkillRow(row pgx.CollectableRow) (*RegistrySkill, error) {
	var s RegistrySkill
	err := row.Scan(&s.ID, &s.Name, &s.Version, &s.Description, &s.Author, &s.Type,
		&s.Tags, &s.Spec, &s.Readme, &s.Downloads, &s.Published, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("scan skill: %w", err)
	}
	return &s, nil
}
