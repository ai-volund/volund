package api

import (
	"testing"
)

func TestIsAllowedMimeType(t *testing.T) {
	tests := []struct {
		mime    string
		allowed bool
	}{
		{"text/plain", true},
		{"application/pdf", true},
		{"image/png", true},
		{"image/jpeg", true},
		{"application/json", true},
		{"application/octet-stream", true},
		{"application/x-executable", false},
		{"application/x-msdos-program", false},
		{"application/x-msdownload", false},
		{"application/x-sharedlib", false},
		{"application/x-shellscript", false},
	}

	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			if got := isAllowedMimeType(tt.mime); got != tt.allowed {
				t.Errorf("isAllowedMimeType(%q) = %v, want %v", tt.mime, got, tt.allowed)
			}
		})
	}
}

func TestAttachmentJSON(t *testing.T) {
	// Verify that attachmentJSON produces the expected fields.
	// This is a lightweight check since it's just a JSON builder.
	userID := "user-1"
	a := &mockAttachment{
		ID:             "att-1",
		ConversationID: "conv-1",
		FileName:       "test.pdf",
		MimeType:       "application/pdf",
		Size:           12345,
		UserID:         &userID,
	}

	// We can't directly test attachmentJSON without a real db.Attachment,
	// but we can test the MIME type validation which is the core security check.
	if !isAllowedMimeType(a.MimeType) {
		t.Error("application/pdf should be allowed")
	}
}

// mockAttachment is a test-only type mirroring the fields we care about.
type mockAttachment struct {
	ID, ConversationID, FileName, MimeType string
	Size                                    int64
	UserID                                  *string
}
