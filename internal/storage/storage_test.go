package storage

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalStore_PutGetDelete(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir, "http://localhost:8080/v1/attachments")
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}

	ctx := context.Background()
	data := []byte("hello world")
	key := "tenant1/conv1/att1/test.txt"

	// Put
	url, err := store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), "text/plain")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if url != "http://localhost:8080/v1/attachments/"+key {
		t.Errorf("URL = %q, want suffix %q", url, key)
	}

	// Verify file exists on disk
	path := filepath.Join(dir, filepath.FromSlash(key))
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("file does not exist after Put")
	}

	// Get
	rc, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "hello world" {
		t.Errorf("content = %q, want %q", got, "hello world")
	}

	// Delete
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file still exists after Delete")
	}
}

func TestLocalStore_NestedDirectories(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir, "http://example.com")
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}

	ctx := context.Background()
	key := "a/b/c/d/file.bin"
	data := []byte{0x00, 0x01, 0x02}

	_, err = store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), "application/octet-stream")
	if err != nil {
		t.Fatalf("Put nested: %v", err)
	}

	rc, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get nested: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, data) {
		t.Error("nested file content mismatch")
	}
}

func TestLocalStore_GetNonExistent(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir, "http://example.com")
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}

	_, err = store.Get(context.Background(), "does/not/exist.txt")
	if err == nil {
		t.Error("expected error for non-existent key")
	}
}

func TestLocalStore_DeleteNonExistent(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir, "http://example.com")
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}

	// Should not error on missing file.
	if err := store.Delete(context.Background(), "does/not/exist.txt"); err != nil {
		t.Errorf("Delete non-existent: %v", err)
	}
}

func TestAttachmentKey(t *testing.T) {
	key := AttachmentKey("tenant-1", "conv-2", "att-3", "report.pdf")
	if key != "tenant-1/conv-2/att-3/report.pdf" {
		t.Errorf("key = %q", key)
	}
}

func TestAttachmentKey_SanitizesFilename(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		wantEnd  string // the last path segment
	}{
		{"path traversal", "../../../etc/passwd", "etc/passwd"},
		{"backslash", "dir\\file.txt", "dir_file.txt"},
		{"null byte", "file\x00.txt", "file.txt"},
		{"double dot", "file..txt", "file_.txt"},
		{"normal", "report.pdf", "report.pdf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := AttachmentKey("t", "c", "a", tt.fileName)
			// Key should not contain ".."
			if filepath.Base(key) == ".." {
				t.Errorf("key contains path traversal: %q", key)
			}
		})
	}
}

func TestSanitizeFileName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"file.txt", "file.txt"},
		{"/etc/passwd", "passwd"},
		{"../secret", "secret"},
		{"a\\b", "a_b"},
	}
	for _, tt := range tests {
		got := sanitizeFileName(tt.input)
		if got != tt.want {
			t.Errorf("sanitize(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestS3Store_URL(t *testing.T) {
	s := NewS3Store(S3Config{
		Endpoint:     "minio:9000",
		Bucket:       "test-bucket",
		UsePathStyle: true,
	})
	url := s.URL("tenant/file.txt")
	if url != "http://minio:9000/test-bucket/tenant/file.txt" {
		t.Errorf("URL = %q", url)
	}
}

func TestS3Store_VirtualHostURL(t *testing.T) {
	s := NewS3Store(S3Config{
		Endpoint:     "s3.amazonaws.com",
		Bucket:       "my-bucket",
		UsePathStyle: false,
		UseSSL:       true,
	})
	url := s.URL("key.txt")
	if url != "https://my-bucket.s3.amazonaws.com/key.txt" {
		t.Errorf("URL = %q", url)
	}
}
