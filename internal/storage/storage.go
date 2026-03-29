// Package storage provides an abstraction over object storage backends
// for conversation file attachments.
package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Store is the interface for object storage operations.
type Store interface {
	// Put writes data to the given key. Returns the URL for retrieval.
	Put(ctx context.Context, key string, data io.Reader, size int64, contentType string) (string, error)
	// Get retrieves data for the given key.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes the object at the given key.
	Delete(ctx context.Context, key string) error
	// URL returns a URL for accessing the given key.
	URL(key string) string
}

// ── Local filesystem backend ────────────────────────────────────────────────

// LocalStore stores files on the local filesystem. Suitable for development
// and single-node deployments.
type LocalStore struct {
	BasePath string // e.g. "/data/attachments"
	BaseURL  string // e.g. "http://localhost:8080/v1/attachments"
}

// NewLocalStore creates a local filesystem store.
func NewLocalStore(basePath, baseURL string) (*LocalStore, error) {
	if err := os.MkdirAll(basePath, 0o755); err != nil {
		return nil, fmt.Errorf("create storage directory: %w", err)
	}
	return &LocalStore{BasePath: basePath, BaseURL: baseURL}, nil
}

func (s *LocalStore) Put(_ context.Context, key string, data io.Reader, _ int64, _ string) (string, error) {
	path := filepath.Join(s.BasePath, filepath.FromSlash(key))
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create directory: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, data); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return s.URL(key), nil
}

func (s *LocalStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	path := filepath.Join(s.BasePath, filepath.FromSlash(key))
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	return f, nil
}

func (s *LocalStore) Delete(_ context.Context, key string) error {
	path := filepath.Join(s.BasePath, filepath.FromSlash(key))
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete file: %w", err)
	}
	return nil
}

func (s *LocalStore) URL(key string) string {
	return s.BaseURL + "/" + key
}

// ── S3-compatible backend ──────────────────────────────────────────────────

// S3Config holds configuration for S3-compatible object storage.
type S3Config struct {
	Endpoint     string // e.g. "minio:9000"
	Bucket       string // e.g. "volund-attachments"
	AccessKey    string
	SecretKey    string
	Region       string
	UsePathStyle bool // true for MinIO
	UseSSL       bool
}

// S3Store stores files in an S3-compatible backend (AWS S3, MinIO, etc.).
// Uses a minimal HTTP-based client to avoid heavy SDK dependencies.
type S3Store struct {
	cfg     S3Config
	baseURL string
}

// NewS3Store creates an S3 store. The actual HTTP signing is deferred to
// a future implementation — for v2 phase 1 we use the local store. This
// struct is here to establish the interface and configuration.
func NewS3Store(cfg S3Config) *S3Store {
	scheme := "http"
	if cfg.UseSSL {
		scheme = "https"
	}
	var baseURL string
	if cfg.UsePathStyle {
		baseURL = fmt.Sprintf("%s://%s/%s", scheme, cfg.Endpoint, cfg.Bucket)
	} else {
		baseURL = fmt.Sprintf("%s://%s.%s", scheme, cfg.Bucket, cfg.Endpoint)
	}
	return &S3Store{cfg: cfg, baseURL: baseURL}
}

func (s *S3Store) Put(_ context.Context, key string, _ io.Reader, _ int64, _ string) (string, error) {
	// TODO: Implement S3 PUT with AWS Signature V4
	return s.URL(key), fmt.Errorf("S3 storage not yet implemented — use local storage for dev")
}

func (s *S3Store) Get(_ context.Context, _ string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("S3 storage not yet implemented — use local storage for dev")
}

func (s *S3Store) Delete(_ context.Context, _ string) error {
	return fmt.Errorf("S3 storage not yet implemented — use local storage for dev")
}

func (s *S3Store) URL(key string) string {
	return s.baseURL + "/" + key
}

// ── Key generation ─────────────────────────────────────────────────────────

// AttachmentKey generates a storage key for an attachment.
// Format: {tenantID}/{conversationID}/{attachmentID}/{fileName}
func AttachmentKey(tenantID, conversationID, attachmentID, fileName string) string {
	// Sanitize filename — strip path separators and control characters.
	safe := sanitizeFileName(fileName)
	return fmt.Sprintf("%s/%s/%s/%s", tenantID, conversationID, attachmentID, safe)
}

func sanitizeFileName(name string) string {
	// Strip directory components.
	name = filepath.Base(name)
	// Replace potentially dangerous characters.
	replacer := strings.NewReplacer(
		"..", "_",
		"/", "_",
		"\\", "_",
		"\x00", "",
	)
	return replacer.Replace(name)
}
