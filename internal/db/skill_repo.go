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
