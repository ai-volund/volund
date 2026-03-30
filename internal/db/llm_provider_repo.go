package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// LLMProvider represents a configured LLM provider in the database.
type LLMProvider struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Type      string          `json:"type"`       // "openai", "anthropic", "ollama", "openai-compatible"
	APIKey    string          `json:"api_key"`     // may be empty if not set
	BaseURL   string          `json:"base_url"`
	Config    json.RawMessage `json:"config"`
	Enabled   bool            `json:"enabled"`
	Priority  int             `json:"priority"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// LLMProviderRepo handles CRUD for the llm_providers table.
type LLMProviderRepo struct {
	pool *Pool
}

// NewLLMProviderRepo creates a new repo.
func NewLLMProviderRepo(pool *Pool) *LLMProviderRepo {
	return &LLMProviderRepo{pool: pool}
}

// Create inserts a new LLM provider.
func (r *LLMProviderRepo) Create(ctx context.Context, p *LLMProvider) (*LLMProvider, error) {
	if p.Config == nil {
		p.Config = json.RawMessage("{}")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO llm_providers (name, type, api_key, base_url, config, enabled, priority)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at`,
		p.Name, p.Type, p.APIKey, p.BaseURL, p.Config, p.Enabled, p.Priority)
	if err := row.Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, fmt.Errorf("create llm provider: %w", err)
	}
	return p, nil
}

// Get retrieves a provider by ID.
func (r *LLMProviderRepo) Get(ctx context.Context, id string) (*LLMProvider, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, type, api_key, base_url, config, enabled, priority, created_at, updated_at
		FROM llm_providers WHERE id = $1`, id)
	return scanLLMProvider(row)
}

// GetByName retrieves a provider by name.
func (r *LLMProviderRepo) GetByName(ctx context.Context, name string) (*LLMProvider, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, type, api_key, base_url, config, enabled, priority, created_at, updated_at
		FROM llm_providers WHERE name = $1`, name)
	return scanLLMProvider(row)
}

// List returns all providers ordered by priority desc.
func (r *LLMProviderRepo) List(ctx context.Context) ([]*LLMProvider, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, type, api_key, base_url, config, enabled, priority, created_at, updated_at
		FROM llm_providers ORDER BY priority DESC, name`)
	if err != nil {
		return nil, fmt.Errorf("list llm providers: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*LLMProvider, error) {
		return scanLLMProvider(row)
	})
}

// ListEnabled returns only enabled providers.
func (r *LLMProviderRepo) ListEnabled(ctx context.Context) ([]*LLMProvider, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, type, api_key, base_url, config, enabled, priority, created_at, updated_at
		FROM llm_providers WHERE enabled = true ORDER BY priority DESC, name`)
	if err != nil {
		return nil, fmt.Errorf("list enabled llm providers: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*LLMProvider, error) {
		return scanLLMProvider(row)
	})
}

// Update modifies an existing provider.
func (r *LLMProviderRepo) Update(ctx context.Context, p *LLMProvider) (*LLMProvider, error) {
	if p.Config == nil {
		p.Config = json.RawMessage("{}")
	}
	row := r.pool.QueryRow(ctx, `
		UPDATE llm_providers
		SET name = $2, type = $3, api_key = $4, base_url = $5, config = $6, enabled = $7, priority = $8, updated_at = NOW()
		WHERE id = $1
		RETURNING id, name, type, api_key, base_url, config, enabled, priority, created_at, updated_at`,
		p.ID, p.Name, p.Type, p.APIKey, p.BaseURL, p.Config, p.Enabled, p.Priority)
	return scanLLMProvider(row)
}

// Delete removes a provider.
func (r *LLMProviderRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM llm_providers WHERE id = $1`, id)
	return err
}

func scanLLMProvider(row pgx.Row) (*LLMProvider, error) {
	var p LLMProvider
	err := row.Scan(&p.ID, &p.Name, &p.Type, &p.APIKey, &p.BaseURL, &p.Config,
		&p.Enabled, &p.Priority, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
