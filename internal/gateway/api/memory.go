package api

import (
	"net/http"
	"time"

	"github.com/ai-volund/volund/internal/db"
)

// handleStoreMemory stores a memory entry with embedding generation.
// POST /v1/memory
func (s *Services) handleStoreMemory(w http.ResponseWriter, r *http.Request) {
	if s.Memories == nil {
		writeError(w, http.StatusServiceUnavailable, "memory service not available")
		return
	}

	claims := claimsFromReq(r)

	var in struct {
		Content    string  `json:"content"`
		Type       string  `json:"type"`
		Importance float64 `json:"importance"`
		TenantID   string  `json:"tenant_id"`
		UserID     string  `json:"user_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	// Use claims tenant if not specified (agents pass these explicitly).
	tenantID := in.TenantID
	if tenantID == "" {
		tenantID = claims.TenantID
	}
	userID := in.UserID
	if userID == "" {
		userID = claims.Subject
	}
	if in.Type == "" {
		in.Type = "fact"
	}
	if in.Importance <= 0 {
		in.Importance = 0.5
	}

	// Generate embedding via the LLM router.
	var embedding []float32
	if s.EmbedFn != nil {
		vecs, err := s.EmbedFn(r.Context(), []string{in.Content})
		if err == nil && len(vecs) > 0 {
			embedding = vecs[0]
		}
		// Non-fatal: store without embedding if generation fails.
	}

	mem := &db.MemoryEntry{
		TenantID:   tenantID,
		UserID:     userID,
		Content:    in.Content,
		Type:       in.Type,
		Importance: in.Importance,
		Embedding:  embedding,
	}

	mem, err := s.Memories.Store(r.Context(), mem)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store memory: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         mem.ID,
		"content":    mem.Content,
		"type":       mem.Type,
		"importance": mem.Importance,
		"created_at": mem.CreatedAt.Format(time.RFC3339),
	})
}

// handleSearchMemory searches memories by semantic similarity.
// POST /v1/memory/search
func (s *Services) handleSearchMemory(w http.ResponseWriter, r *http.Request) {
	if s.Memories == nil {
		writeError(w, http.StatusServiceUnavailable, "memory service not available")
		return
	}

	claims := claimsFromReq(r)

	var in struct {
		Query    string `json:"query"`
		Limit    int    `json:"limit"`
		TenantID string `json:"tenant_id"`
		UserID   string `json:"user_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	tenantID := in.TenantID
	if tenantID == "" {
		tenantID = claims.TenantID
	}
	userID := in.UserID
	if userID == "" {
		userID = claims.Subject
	}

	// Embed the query.
	if s.EmbedFn == nil {
		writeError(w, http.StatusServiceUnavailable, "embedding service not available")
		return
	}

	vecs, err := s.EmbedFn(r.Context(), []string{in.Query})
	if err != nil || len(vecs) == 0 {
		writeError(w, http.StatusInternalServerError, "generate query embedding: "+err.Error())
		return
	}

	results, err := s.Memories.SearchSimilar(r.Context(), tenantID, userID, vecs[0], in.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search memories: "+err.Error())
		return
	}

	out := make([]map[string]any, 0, len(results))
	for _, r := range results {
		out = append(out, map[string]any{
			"id":         r.ID,
			"content":    r.Content,
			"type":       r.Type,
			"importance": r.Importance,
			"similarity": r.Similarity,
			"created_at": r.CreatedAt.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, out)
}

// handleListMemories returns all memories for the authenticated user.
// GET /v1/memory
func (s *Services) handleListMemories(w http.ResponseWriter, r *http.Request) {
	if s.Memories == nil {
		writeError(w, http.StatusServiceUnavailable, "memory service not available")
		return
	}

	claims := claimsFromReq(r)
	mems, err := s.Memories.ListByUser(r.Context(), claims.TenantID, claims.Subject, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list memories: "+err.Error())
		return
	}

	out := make([]map[string]any, 0, len(mems))
	for _, m := range mems {
		out = append(out, map[string]any{
			"id":         m.ID,
			"content":    m.Content,
			"type":       m.Type,
			"importance": m.Importance,
			"created_at": m.CreatedAt.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, out)
}

// handleDeleteMemory removes a memory entry.
// DELETE /v1/memory/{id}
func (s *Services) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	if s.Memories == nil {
		writeError(w, http.StatusServiceUnavailable, "memory service not available")
		return
	}

	claims := claimsFromReq(r)
	id := r.PathValue("id")
	if err := s.Memories.Delete(r.Context(), id, claims.TenantID); err != nil {
		writeError(w, http.StatusNotFound, "memory not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
