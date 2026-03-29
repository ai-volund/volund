package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ai-volund/volund/internal/db"
)

// Registry manages OAuth2 provider configurations stored in the database.
type Registry struct {
	pool *db.Pool
}

// NewRegistry creates a provider registry backed by PostgreSQL.
func NewRegistry(pool *db.Pool) *Registry {
	return &Registry{pool: pool}
}

// RegisterFromSkill upserts a provider definition from a skill manifest.
// This sets everything except client_id/client_secret (admin provides those later).
// If the provider already exists and is configured, only metadata fields are updated.
func (r *Registry) RegisterFromSkill(ctx context.Context, cfg *ProviderConfig) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO oauth_providers
		  (id, display_name, auth_url, token_url, client_id, client_secret,
		   scopes, redirect_path, extra_auth_params, extra_headers,
		   icon_url, category, skill_id, configured)
		VALUES ($1,$2,$3,$4,'',$5,$6,$7,$8,$9,$10,$11,$12,false)
		ON CONFLICT (id) DO UPDATE SET
		  display_name=$2, auth_url=$3, token_url=$4,
		  scopes=$6, redirect_path=$7, extra_auth_params=$8, extra_headers=$9,
		  icon_url=$10, category=$11, skill_id=$12, updated_at=NOW()
	`, cfg.ID, cfg.DisplayName, cfg.AuthURL, cfg.TokenURL,
		"", // client_secret stays empty
		cfg.Scopes, cfg.RedirectPath, jsonOrNull(cfg.ExtraAuthParams), jsonOrNull(cfg.ExtraHeaders),
		cfg.IconURL, cfg.Category, cfg.SkillID)
	return err
}

// SetCredentials sets the client_id and client_secret for a provider and marks it configured.
// This is the only admin action needed — everything else comes from the skill manifest.
func (r *Registry) SetCredentials(ctx context.Context, id, clientID, clientSecret string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE oauth_providers
		SET client_id=$2, client_secret=$3, configured=true, updated_at=NOW()
		WHERE id=$1
	`, id, clientID, clientSecret)
	if err != nil {
		return fmt.Errorf("set credentials: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("provider %q not found", id)
	}
	return nil
}

// Register adds or updates an OAuth2 provider configuration (full registration, for custom providers).
func (r *Registry) Register(ctx context.Context, cfg *ProviderConfig) error {
	configured := cfg.ClientID != "" && cfg.ClientSecret != ""
	_, err := r.pool.Exec(ctx, `
		INSERT INTO oauth_providers
		  (id, display_name, auth_url, token_url, client_id, client_secret,
		   scopes, redirect_path, extra_auth_params, extra_headers,
		   icon_url, category, skill_id, configured)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (id) DO UPDATE SET
		  display_name=$2, auth_url=$3, token_url=$4, client_id=$5, client_secret=$6,
		  scopes=$7, redirect_path=$8, extra_auth_params=$9, extra_headers=$10,
		  icon_url=$11, category=$12, skill_id=$13, configured=$14, updated_at=NOW()
	`, cfg.ID, cfg.DisplayName, cfg.AuthURL, cfg.TokenURL, cfg.ClientID, cfg.ClientSecret,
		cfg.Scopes, cfg.RedirectPath, jsonOrNull(cfg.ExtraAuthParams), jsonOrNull(cfg.ExtraHeaders),
		cfg.IconURL, cfg.Category, cfg.SkillID, configured)
	return err
}

// Get returns a single provider by ID.
func (r *Registry) Get(ctx context.Context, id string) (*ProviderConfig, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, display_name, auth_url, token_url, client_id, client_secret,
		       scopes, redirect_path, extra_auth_params, extra_headers,
		       icon_url, category, skill_id, configured, created_at, updated_at
		FROM oauth_providers WHERE id = $1
	`, id)
	return scanProvider(row)
}

// List returns all registered providers.
func (r *Registry) List(ctx context.Context) ([]*ProviderConfig, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, display_name, auth_url, token_url, client_id, '',
		       scopes, redirect_path, extra_auth_params, extra_headers,
		       icon_url, category, skill_id, configured, created_at, updated_at
		FROM oauth_providers ORDER BY display_name
	`)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*ProviderConfig, error) {
		return scanProvider(row)
	})
}

// ListConfigured returns only providers where admin has supplied credentials.
// These are the providers users can actually connect to.
func (r *Registry) ListConfigured(ctx context.Context) ([]*ProviderConfig, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, display_name, auth_url, token_url, client_id, '',
		       scopes, redirect_path, extra_auth_params, extra_headers,
		       icon_url, category, skill_id, configured, created_at, updated_at
		FROM oauth_providers WHERE configured = true ORDER BY display_name
	`)
	if err != nil {
		return nil, fmt.Errorf("list configured providers: %w", err)
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*ProviderConfig, error) {
		return scanProvider(row)
	})
}

// Delete removes a provider configuration.
func (r *Registry) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM oauth_providers WHERE id = $1`, id)
	return err
}

func scanProvider(row pgx.Row) (*ProviderConfig, error) {
	p := &ProviderConfig{}
	var scopes []string
	var extraAuth, extraHeaders json.RawMessage
	var skillID *string

	err := row.Scan(
		&p.ID, &p.DisplayName, &p.AuthURL, &p.TokenURL, &p.ClientID, &p.ClientSecret,
		&scopes, &p.RedirectPath, &extraAuth, &extraHeaders,
		&p.IconURL, &p.Category, &skillID, &p.Configured, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan provider: %w", err)
	}

	p.Scopes = scopes
	if skillID != nil {
		p.SkillID = *skillID
	}
	if extraAuth != nil {
		json.Unmarshal(extraAuth, &p.ExtraAuthParams)
	}
	if extraHeaders != nil {
		json.Unmarshal(extraHeaders, &p.ExtraHeaders)
	}

	return p, nil
}

func jsonOrNull(m map[string]string) any {
	if len(m) == 0 {
		return nil
	}
	data, _ := json.Marshal(m)
	return data
}

// ── In-memory state store for pending OAuth flows ────────────────────────────
// For production, this should be backed by Redis with TTL.

// StateStore manages pending OAuth2 authorization states.
type StateStore struct {
	states map[string]*AuthState
}

// NewStateStore creates a new in-memory state store.
func NewStateStore() *StateStore {
	return &StateStore{states: make(map[string]*AuthState)}
}

// Save stores an auth state, keyed by the CSRF state parameter.
func (s *StateStore) Save(state *AuthState) {
	s.states[state.State] = state
}

// Pop retrieves and removes an auth state. Returns nil if not found or expired (10 min TTL).
func (s *StateStore) Pop(stateKey string) *AuthState {
	st, ok := s.states[stateKey]
	if !ok {
		return nil
	}
	delete(s.states, stateKey)

	// Expire after 10 minutes.
	if time.Since(st.CreatedAt) > 10*time.Minute {
		return nil
	}
	return st
}
