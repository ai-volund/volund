package api

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ai-volund/volund/internal/db"
	"github.com/ai-volund/volund/internal/storage"
)

// POST /v1/conversations/{id}/attachments — upload a file to a conversation.
// Accepts multipart/form-data with a "file" field.
func (s *Services) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	convo, err := s.Convos.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	if convo.TenantID != claims.TenantID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	if s.Attachments == nil || s.Store == nil {
		writeError(w, http.StatusNotImplemented, "file upload not configured")
		return
	}

	// Parse multipart form — limit to MaxUploadSize.
	maxSize := s.MaxUploadSize
	if maxSize <= 0 {
		maxSize = 100 * 1024 * 1024 // 100MB default
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSize)
	if err := r.ParseMultipartForm(32 << 20); err != nil { // 32MB buffer
		writeError(w, http.StatusBadRequest, "file too large or invalid multipart form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing 'file' field in multipart form")
		return
	}
	defer file.Close()

	// Validate file type.
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	if !isAllowedMimeType(mimeType) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("file type %q is not allowed", mimeType))
		return
	}

	// Create the DB record first to get the attachment ID.
	userID := s.resolveVolundUserID(r.Context(), claims.Subject)
	att, err := s.Attachments.Create(r.Context(), &db.Attachment{
		ConversationID: convo.ID,
		TenantID:       convo.TenantID,
		UserID:         &userID,
		FileName:       header.Filename,
		MimeType:       mimeType,
		Size:           header.Size,
		StorageKey:     "", // will be set after upload
		Metadata:       map[string]any{},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create attachment record failed")
		return
	}

	// Generate storage key and upload.
	key := storage.AttachmentKey(convo.TenantID, convo.ID, att.ID, header.Filename)
	url, err := s.Store.Put(r.Context(), key, file, header.Size, mimeType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "file upload failed")
		return
	}

	// Update the storage key (we could do this in a single step with a generated ID,
	// but this keeps the flow simple and the key depends on the DB-generated ID).
	att.StorageKey = key

	writeJSON(w, http.StatusCreated, attachmentJSON(att, url))
}

// GET /v1/attachments/{id} — retrieve attachment metadata (and optionally the file).
func (s *Services) handleGetAttachment(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	if s.Attachments == nil {
		writeError(w, http.StatusNotImplemented, "file upload not configured")
		return
	}

	att, err := s.Attachments.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	if att.TenantID != claims.TenantID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	// If ?download=true, stream the file content.
	if r.URL.Query().Get("download") == "true" && s.Store != nil {
		rc, err := s.Store.Get(r.Context(), att.StorageKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "file retrieval failed")
			return
		}
		defer rc.Close()

		w.Header().Set("Content-Type", att.MimeType)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", att.FileName))
		io.Copy(w, rc)
		return
	}

	url := ""
	if s.Store != nil {
		url = s.Store.URL(att.StorageKey)
	}
	writeJSON(w, http.StatusOK, attachmentJSON(att, url))
}

// GET /v1/conversations/{id}/attachments — list all attachments for a conversation.
func (s *Services) handleListAttachments(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromReq(r)
	convo, err := s.Convos.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	if convo.TenantID != claims.TenantID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	if s.Attachments == nil {
		writeJSON(w, http.StatusOK, map[string]any{"attachments": []any{}})
		return
	}

	atts, err := s.Attachments.ListByConversation(r.Context(), convo.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list attachments failed")
		return
	}

	out := make([]any, len(atts))
	for i, a := range atts {
		url := ""
		if s.Store != nil {
			url = s.Store.URL(a.StorageKey)
		}
		out[i] = attachmentJSON(a, url)
	}
	writeJSON(w, http.StatusOK, map[string]any{"attachments": out})
}

func attachmentJSON(a *db.Attachment, url string) map[string]any {
	out := map[string]any{
		"id":              a.ID,
		"conversation_id": a.ConversationID,
		"file_name":       a.FileName,
		"mime_type":       a.MimeType,
		"size":            a.Size,
		"created_at":      a.CreatedAt.Format(time.RFC3339),
	}
	if url != "" {
		out["url"] = url
	}
	if a.UserID != nil {
		out["user_id"] = *a.UserID
	}
	return out
}

// isAllowedMimeType checks if a MIME type is allowed for upload.
// Blocks executables and other potentially dangerous file types.
func isAllowedMimeType(mimeType string) bool {
	blocked := []string{
		"application/x-executable",
		"application/x-msdos-program",
		"application/x-msdownload",
		"application/x-sharedlib",
		"application/x-shellscript",
	}
	for _, b := range blocked {
		if mimeType == b {
			return false
		}
	}
	return true
}
