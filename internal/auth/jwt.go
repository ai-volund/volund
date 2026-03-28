package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims represents the JWT claims used by Volund.
type Claims struct {
	jwt.RegisteredClaims
	TenantID string `json:"tenant_id,omitempty"`
	Role     string `json:"role,omitempty"`
}

// TokenManager handles JWT creation and validation.
type TokenManager struct {
	secret []byte
}

// NewTokenManager creates a TokenManager with the given signing secret.
func NewTokenManager(secret string) *TokenManager {
	return &TokenManager{secret: []byte(secret)}
}

// Generate creates a signed JWT for the given subject, tenant, and role.
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
func (tm *TokenManager) Validate(tokenStr string) (*Claims, error) {
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
