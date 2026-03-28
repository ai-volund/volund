package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// MemoryEntry is a long-term memory with embedding.
type MemoryEntry struct {
	ID           string
	TenantID     string
	UserID       string
	Content      string
	Type         string  // fact, preference, learned, entity, observation, reflection
	Importance   float64
	Embedding    []float32
	AccessCount  int
	LastAccessed *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// MemorySearchResult extends MemoryEntry with a similarity score.
type MemorySearchResult struct {
	MemoryEntry
	Similarity float64
}

// MemoryRepo handles memory persistence with pgvector.
type MemoryRepo struct{ pool *Pool }

// NewMemoryRepo creates a MemoryRepo.
func NewMemoryRepo(pool *Pool) *MemoryRepo { return &MemoryRepo{pool: pool} }

// Store inserts a memory entry with its embedding.
// If a near-duplicate exists (cosine similarity > 0.95), the existing entry is
// updated instead of creating a new one (deduplication).
func (r *MemoryRepo) Store(ctx context.Context, m *MemoryEntry) (*MemoryEntry, error) {
	// Dedup: check for near-duplicate before inserting.
	if len(m.Embedding) > 0 {
		var dupID string
		err := r.pool.QueryRow(ctx,
			`SELECT id FROM memories
			 WHERE tenant_id = $1 AND user_id = $2 AND embedding IS NOT NULL
			   AND 1 - (embedding <=> $3::vector) > 0.95
			 ORDER BY embedding <=> $3::vector
			 LIMIT 1`,
			m.TenantID, m.UserID, pgvecString(m.Embedding),
		).Scan(&dupID)
		if err == nil && dupID != "" {
			// Update existing entry instead of creating duplicate.
			row := r.pool.QueryRow(ctx,
				`UPDATE memories SET content = $1, type = $2, importance = $3,
				        embedding = $4::vector, updated_at = NOW(),
				        access_count = access_count + 1, last_accessed = NOW()
				 WHERE id = $5
				 RETURNING id, created_at, updated_at`,
				m.Content, m.Type, m.Importance, pgvecString(m.Embedding), dupID,
			)
			if err := row.Scan(&m.ID, &m.CreatedAt, &m.UpdatedAt); err != nil {
				return nil, fmt.Errorf("update duplicate memory: %w", err)
			}
			return m, nil
		}
	}

	row := r.pool.QueryRow(ctx,
		`INSERT INTO memories (tenant_id, user_id, content, type, importance, embedding)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, created_at, updated_at`,
		m.TenantID, m.UserID, m.Content, m.Type, m.Importance, pgvecString(m.Embedding),
	)
	if err := row.Scan(&m.ID, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, fmt.Errorf("store memory: %w", err)
	}
	return m, nil
}

// SearchSimilar finds the most similar memories using cosine distance with
// time-decay scoring. The effective score is:
//
//	score = cosine_similarity * decay_factor
//
// where decay_factor = 1 / (1 + age_days/30). Frequently accessed memories
// decay slower via a boost: decay_factor * (1 + ln(1 + access_count)/5).
// Results are also marked as accessed (access_count++, last_accessed=NOW()).
func (r *MemoryRepo) SearchSimilar(ctx context.Context, tenantID, userID string, embedding []float32, limit int) ([]*MemorySearchResult, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := r.pool.Query(ctx,
		`WITH ranked AS (
		   SELECT id, tenant_id, user_id, content, type, importance,
		          1 - (embedding <=> $1::vector) AS similarity,
		          access_count, last_accessed, created_at, updated_at,
		          (1 - (embedding <=> $1::vector))
		            * (1.0 / (1.0 + EXTRACT(EPOCH FROM NOW() - created_at) / 2592000.0))
		            * (1.0 + LN(1.0 + COALESCE(access_count, 0)) / 5.0)
		          AS decayed_score
		   FROM memories
		   WHERE tenant_id = $2 AND user_id = $3 AND embedding IS NOT NULL
		   ORDER BY decayed_score DESC
		   LIMIT $4
		 )
		 UPDATE memories m
		 SET access_count = m.access_count + 1, last_accessed = NOW()
		 FROM ranked r
		 WHERE m.id = r.id
		 RETURNING r.id, r.tenant_id, r.user_id, r.content, r.type, r.importance,
		           r.similarity, r.access_count, r.last_accessed, r.created_at, r.updated_at`,
		pgvecString(embedding), tenantID, userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*MemorySearchResult, error) {
		var r MemorySearchResult
		err := row.Scan(&r.ID, &r.TenantID, &r.UserID, &r.Content, &r.Type, &r.Importance,
			&r.Similarity, &r.AccessCount, &r.LastAccessed, &r.CreatedAt, &r.UpdatedAt)
		return &r, err
	})
}

// ListByUser returns all memories for a user (without similarity scoring).
func (r *MemoryRepo) ListByUser(ctx context.Context, tenantID, userID string, limit int) ([]*MemoryEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, tenant_id, user_id, content, type, importance, created_at, updated_at
		 FROM memories
		 WHERE tenant_id = $1 AND user_id = $2
		 ORDER BY importance DESC, created_at DESC
		 LIMIT $3`,
		tenantID, userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*MemoryEntry, error) {
		var m MemoryEntry
		err := row.Scan(&m.ID, &m.TenantID, &m.UserID, &m.Content, &m.Type, &m.Importance,
			&m.CreatedAt, &m.UpdatedAt)
		return &m, err
	})
}

// Delete removes a memory entry.
func (r *MemoryRepo) Delete(ctx context.Context, id, tenantID string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM memories WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete memory: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("memory not found")
	}
	return nil
}

// pgvecString converts a float32 slice to pgvector literal format: [0.1,0.2,...].
func pgvecString(v []float32) string {
	if len(v) == 0 {
		return ""
	}
	s := "["
	for i, f := range v {
		if i > 0 {
			s += ","
		}
		s += fmt.Sprintf("%g", f)
	}
	s += "]"
	return s
}
