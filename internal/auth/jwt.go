package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
)

// Claims represents the JWT claims used by Volund.
type Claims struct {
	jwt.RegisteredClaims
	TenantID string `json:"tenant_id,omitempty"`
	Role     string `json:"role,omitempty"`
}

// TokenManager handles JWT creation and validation.
// Supports HS256 (shared secret) and JWKS-based asymmetric validation.
type TokenManager struct {
	secret []byte

	// JWKS-based validation (for better-auth issued tokens).
	jwksURL  string
	jwksKeys map[string]any // kid → crypto.PublicKey
	jwksMu   sync.RWMutex
	jwksLast time.Time
}

// NewTokenManager creates a TokenManager with the given signing secret.
func NewTokenManager(secret string) *TokenManager {
	return &TokenManager{secret: []byte(secret)}
}

// SetJWKSURL enables JWKS-based validation by fetching public keys from the
// given URL (e.g. "http://volund-auth:3456/api/auth/jwks").
// Keys are cached and refreshed every 5 minutes.
func (tm *TokenManager) SetJWKSURL(url string) {
	tm.jwksURL = url
	if err := tm.refreshJWKS(); err != nil {
		slog.Warn("initial JWKS fetch failed (will retry on first request)", "url", url, "error", err)
	}
}

// Generate creates a signed HS256 JWT for the given subject, tenant, and role.
// Used for service-to-service tokens and legacy compatibility.
func (tm *TokenManager) Generate(subject, tenantID, role string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			Issuer:    "volund",
		},
		TenantID: tenantID,
		Role:     role,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(tm.secret)
}

// Validate parses and validates a JWT string, returning the claims.
// Tries JWKS-based validation first (if configured), falls back to HS256.
func (tm *TokenManager) Validate(tokenStr string) (*Claims, error) {
	if tm.jwksURL != "" {
		claims, err := tm.validateJWKS(tokenStr)
		if err == nil {
			return claims, nil
		}
		// Fall through to HS256.
	}
	return tm.validateHS256(tokenStr)
}

func (tm *TokenManager) validateHS256(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return tm.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}

func (tm *TokenManager) validateJWKS(tokenStr string) (*Claims, error) {
	// Check if we need to refresh keys.
	tm.jwksMu.RLock()
	needRefresh := tm.jwksKeys == nil || time.Since(tm.jwksLast) > 5*time.Minute
	tm.jwksMu.RUnlock()

	if needRefresh {
		if err := tm.refreshJWKS(); err != nil {
			return nil, fmt.Errorf("jwks refresh: %w", err)
		}
	}

	// Parse the token header to find the kid.
	parser := jwt.NewParser()
	unverified, _, err := parser.ParseUnverified(tokenStr, &Claims{})
	if err != nil {
		return nil, fmt.Errorf("parse token header: %w", err)
	}

	kid, _ := unverified.Header["kid"].(string)
	if kid == "" {
		return nil, fmt.Errorf("token has no kid header")
	}

	tm.jwksMu.RLock()
	key, exists := tm.jwksKeys[kid]
	tm.jwksMu.RUnlock()

	if !exists {
		// Key not found — refresh once and retry.
		if err := tm.refreshJWKS(); err != nil {
			return nil, fmt.Errorf("jwks refresh: %w", err)
		}
		tm.jwksMu.RLock()
		key, exists = tm.jwksKeys[kid]
		tm.jwksMu.RUnlock()
		if !exists {
			return nil, fmt.Errorf("unknown key id: %s", kid)
		}
	}

	// Validate with the public key.
	validated, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		return key, nil
	})
	if err != nil {
		return nil, fmt.Errorf("jwks validate: %w", err)
	}

	claims, ok := validated.Claims.(*Claims)
	if !ok || !validated.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}

// refreshJWKS fetches public keys from the JWKS endpoint.
func (tm *TokenManager) refreshJWKS() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", tm.jwksURL, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read jwks: %w", err)
	}

	// Parse the JWKS using go-jose.
	var jwks jose.JSONWebKeySet
	if err := json.Unmarshal(body, &jwks); err != nil {
		return fmt.Errorf("parse jwks: %w", err)
	}

	keys := make(map[string]any)
	for _, k := range jwks.Keys {
		keys[k.KeyID] = k.Key
	}

	tm.jwksMu.Lock()
	tm.jwksKeys = keys
	tm.jwksLast = time.Now()
	tm.jwksMu.Unlock()

	slog.Info("refreshed JWKS", "url", tm.jwksURL, "keys", len(keys))
	return nil
}
