package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"
)

// POST /v1/apikeys — create a new API key for the current user.
func (s *Services) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	userID := s.resolveVolundUserID(r.Context(), claims.Subject)

	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Generate a random key.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		writeError(w, http.StatusInternalServerError, "generate key failed")
		return
	}
	key := "vk_" + hex.EncodeToString(raw)

	// Hash for storage.
	hash := sha256.Sum256([]byte(key))
	keyHash := hex.EncodeToString(hash[:])

	if s.Pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	var id string
	err := s.Pool.QueryRow(r.Context(),
		`INSERT INTO api_keys (tenant_id, user_id, name, key_hash) VALUES ($1, $2, $3, $4) RETURNING id`,
		claims.TenantID, userID, in.Name, keyHash).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create key: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":   id,
		"name": in.Name,
		"key":  key, // Only shown once!
	})
}

// GET /v1/apikeys — list API keys for the current user.
func (s *Services) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	userID := s.resolveVolundUserID(r.Context(), claims.Subject)

	if s.Pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	rows, err := s.Pool.Query(r.Context(),
		`SELECT id, name, last_used, created_at FROM api_keys
		 WHERE tenant_id = $1 AND user_id = $2 ORDER BY created_at DESC`,
		claims.TenantID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list keys: "+err.Error())
		return
	}
	defer rows.Close()

	var keys []map[string]any
	for rows.Next() {
		var id, name string
		var lastUsed *time.Time
		var createdAt time.Time
		if err := rows.Scan(&id, &name, &lastUsed, &createdAt); err != nil {
			continue
		}
		m := map[string]any{
			"id":         id,
			"name":       name,
			"created_at": createdAt.Format(time.RFC3339),
		}
		if lastUsed != nil {
			m["last_used"] = lastUsed.Format(time.RFC3339)
		}
		keys = append(keys, m)
	}
	if keys == nil {
		keys = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

// DELETE /v1/apikeys/{id} — revoke an API key.
func (s *Services) handleDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	userID := s.resolveVolundUserID(r.Context(), claims.Subject)
	id := r.PathValue("id")

	if s.Pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database not available")
		return
	}

	_, err := s.Pool.Exec(r.Context(),
		`DELETE FROM api_keys WHERE id = $1 AND tenant_id = $2 AND user_id = $3`,
		id, claims.TenantID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete key: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}
