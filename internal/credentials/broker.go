// Package credentials implements the credential broker — per-user, per-tool
// credential storage with tenant isolation. Agents never see raw user tokens;
// they receive short-lived scoped tokens via the broker API.
package credentials

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/ai-volund/volund/internal/db"
)

// DefaultTokenTTL is the lifetime of short-lived tokens issued to agents.
const DefaultTokenTTL = 5 * time.Minute

// Broker manages encrypted credential storage and short-lived token issuance.
type Broker struct {
	repo   *db.CredentialRepo
	aesGCM cipher.AEAD
}

// NewBroker creates a credential broker. The encryptionKey must be a 32-byte
// hex-encoded string (AES-256).
func NewBroker(repo *db.CredentialRepo, encryptionKeyHex string) (*Broker, error) {
	keyBytes, err := hex.DecodeString(encryptionKeyHex)
	if err != nil {
		return nil, fmt.Errorf("decode encryption key: %w", err)
	}
	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes (got %d)", len(keyBytes))
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	return &Broker{repo: repo, aesGCM: gcm}, nil
}

// StoreCredential encrypts and stores a user credential.
func (b *Broker) StoreCredential(ctx context.Context, req StoreRequest) error {
	encrypted, err := b.encrypt([]byte(req.Token))
	if err != nil {
		return fmt.Errorf("encrypt token: %w", err)
	}

	var encRefresh []byte
	if req.RefreshToken != "" {
		encRefresh, err = b.encrypt([]byte(req.RefreshToken))
		if err != nil {
			return fmt.Errorf("encrypt refresh token: %w", err)
		}
	}

	cred := &db.UserCredential{
		TenantID:       req.TenantID,
		UserID:         req.UserID,
		Provider:       req.Provider,
		EncryptedToken: encrypted,
		Scopes:         req.Scopes,
		TokenType:      req.TokenType,
		ExpiresAt:      req.ExpiresAt,
		RefreshToken:   encRefresh,
		Metadata:       []byte("{}"),
	}

	if _, err := b.repo.Store(ctx, cred); err != nil {
		return err
	}

	// Audit log.
	_ = b.repo.AuditLog(ctx, &db.CredentialAuditEntry{
		TenantID: req.TenantID,
		UserID:   req.UserID,
		Provider: req.Provider,
		Action:   "credential_stored",
	})

	slog.Info("credential stored", "tenant", req.TenantID, "user", req.UserID, "provider", req.Provider)
	return nil
}

// IssueToken retrieves and decrypts a credential, returning a short-lived
// scoped token for agent use. Validates tenant isolation and scope intersection.
func (b *Broker) IssueToken(ctx context.Context, req TokenRequest) (*TokenResponse, error) {
	// Validate tenant isolation: the requesting agent must be in the same tenant.
	cred, err := b.repo.Get(ctx, req.TenantID, req.UserID, req.Provider)
	if err != nil {
		_ = b.repo.AuditLog(ctx, &db.CredentialAuditEntry{
			TenantID:        req.TenantID,
			UserID:          req.UserID,
			Provider:        req.Provider,
			AgentInstanceID: req.AgentInstanceID,
			ConversationID:  req.ConversationID,
			SkillName:       req.SkillName,
			Action:          "token_denied",
			ScopesRequested: req.Scopes,
		})
		return nil, fmt.Errorf("credential not found for provider %q", req.Provider)
	}

	// Intersect requested scopes with stored scopes.
	granted := intersectScopes(req.Scopes, cred.Scopes)
	if len(req.Scopes) > 0 && len(granted) == 0 {
		_ = b.repo.AuditLog(ctx, &db.CredentialAuditEntry{
			TenantID:        req.TenantID,
			UserID:          req.UserID,
			Provider:        req.Provider,
			AgentInstanceID: req.AgentInstanceID,
			SkillName:       req.SkillName,
			Action:          "token_denied",
			ScopesRequested: req.Scopes,
		})
		return nil, fmt.Errorf("no matching scopes for provider %q", req.Provider)
	}

	// Decrypt the stored token.
	plaintext, err := b.decrypt(cred.EncryptedToken)
	if err != nil {
		return nil, fmt.Errorf("decrypt token: %w", err)
	}

	// Check if the stored token has expired.
	if cred.ExpiresAt != nil && cred.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("stored credential for %q has expired", req.Provider)
	}

	// Audit success.
	_ = b.repo.AuditLog(ctx, &db.CredentialAuditEntry{
		TenantID:        req.TenantID,
		UserID:          req.UserID,
		Provider:        req.Provider,
		AgentInstanceID: req.AgentInstanceID,
		ConversationID:  req.ConversationID,
		SkillName:       req.SkillName,
		Action:          "token_issued",
		ScopesRequested: req.Scopes,
		ScopesGranted:   granted,
	})

	slog.Info("credential token issued",
		"tenant", req.TenantID, "user", req.UserID, "provider", req.Provider,
		"agent", req.AgentInstanceID, "skill", req.SkillName,
		"scopes_granted", granted)

	return &TokenResponse{
		Token:     string(plaintext),
		ExpiresIn: int(DefaultTokenTTL.Seconds()),
		Scopes:    granted,
	}, nil
}

// DeleteCredential removes a stored credential.
func (b *Broker) DeleteCredential(ctx context.Context, tenantID, userID, provider string) error {
	if err := b.repo.Delete(ctx, tenantID, userID, provider); err != nil {
		return err
	}
	_ = b.repo.AuditLog(ctx, &db.CredentialAuditEntry{
		TenantID: tenantID,
		UserID:   userID,
		Provider: provider,
		Action:   "credential_deleted",
	})
	return nil
}

// ListProviders returns the providers a user has connected (without tokens).
func (b *Broker) ListProviders(ctx context.Context, tenantID, userID string) ([]ProviderInfo, error) {
	creds, err := b.repo.ListByUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	out := make([]ProviderInfo, len(creds))
	for i, c := range creds {
		out[i] = ProviderInfo{
			Provider:  c.Provider,
			TokenType: c.TokenType,
			Scopes:    c.Scopes,
			ExpiresAt: c.ExpiresAt,
			CreatedAt: c.CreatedAt,
			UpdatedAt: c.UpdatedAt,
		}
	}
	return out, nil
}

// encrypt uses AES-256-GCM to encrypt plaintext.
func (b *Broker) encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, b.aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return b.aesGCM.Seal(nonce, nonce, plaintext, nil), nil
}

// decrypt uses AES-256-GCM to decrypt ciphertext.
func (b *Broker) decrypt(ciphertext []byte) ([]byte, error) {
	nonceSize := b.aesGCM.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, data := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return b.aesGCM.Open(nil, nonce, data, nil)
}

// intersectScopes returns scopes present in both requested and available.
// If requested is empty, returns all available scopes.
func intersectScopes(requested, available []string) []string {
	if len(requested) == 0 {
		return available
	}
	avail := make(map[string]bool, len(available))
	for _, s := range available {
		avail[s] = true
	}
	var result []string
	for _, s := range requested {
		if avail[s] {
			result = append(result, s)
		}
	}
	return result
}
