package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ai-volund/volund/internal/db"
)

const (
	AccessTokenTTL  = 15 * time.Minute
	RefreshTokenTTL = 30 * 24 * time.Hour // 30 days
	refreshTokenLen = 32                  // bytes of entropy
)

// ErrInvalidCredentials is returned when email/password don't match.
var ErrInvalidCredentials = errors.New("invalid email or password")

// ErrInvalidRefreshToken is returned when a refresh token is invalid or expired.
var ErrInvalidRefreshToken = errors.New("invalid or expired refresh token")

// TokenPair holds an access token + refresh token.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// Service handles authentication: register, login, refresh, logout.
type Service struct {
	tm           *TokenManager
	users        *db.UserRepo
	tenants      *db.TenantRepo
	refreshTokens *db.RefreshTokenRepo
}

// NewService creates a new auth Service.
func NewService(tm *TokenManager, users *db.UserRepo, tenants *db.TenantRepo, rt *db.RefreshTokenRepo) *Service {
	return &Service{tm: tm, users: users, tenants: tenants, refreshTokens: rt}
}

// RegisterInput holds the data needed to register a new user + org.
type RegisterInput struct {
	Email       string
	Password    string
	DisplayName string
	OrgName     string // name of the new organization/tenant
}

// Register creates a new user and a new tenant org, adds the user as owner.
func (s *Service) Register(ctx context.Context, in RegisterInput) (*TokenPair, *db.User, error) {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.Email == "" || in.Password == "" {
		return nil, nil, fmt.Errorf("email and password are required")
	}
	if len(in.Password) < 8 {
		return nil, nil, fmt.Errorf("password must be at least 8 characters")
	}

	// Check email not already taken.
	existing, err := s.users.GetByEmail(ctx, in.Email)
	if err == nil && existing != nil {
		return nil, nil, fmt.Errorf("email already in use")
	}

	hash, err := HashPassword(in.Password)
	if err != nil {
		return nil, nil, err
	}

	// Create user.
	if in.DisplayName == "" {
		in.DisplayName = strings.Split(in.Email, "@")[0]
	}
	user, err := s.users.Create(ctx, in.Email, in.DisplayName)
	if err != nil {
		return nil, nil, fmt.Errorf("create user: %w", err)
	}
	if err := s.users.SetPasswordHash(ctx, user.ID, hash); err != nil {
		return nil, nil, fmt.Errorf("set password: %w", err)
	}
	user.PasswordHash = &hash

	// Create tenant.
	orgName := in.OrgName
	if orgName == "" {
		orgName = in.DisplayName + "'s Org"
	}
	slug := slugify(orgName)
	tenant, err := s.tenants.Create(ctx, orgName, slug, "free")
	if err != nil {
		return nil, nil, fmt.Errorf("create tenant: %w", err)
	}

	// Add user as owner.
	if err := s.users.AddToTenant(ctx, tenant.ID, user.ID, "owner"); err != nil {
		return nil, nil, fmt.Errorf("add owner: %w", err)
	}

	pair, err := s.issuePair(ctx, user.ID, tenant.ID, "owner")
	if err != nil {
		return nil, nil, err
	}
	return pair, user, nil
}

// Login validates credentials and returns tokens.
func (s *Service) Login(ctx context.Context, email, password string) (*TokenPair, *db.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		return nil, nil, ErrInvalidCredentials
	}

	if user.PasswordHash == nil || !CheckPassword(*user.PasswordHash, password) {
		return nil, nil, ErrInvalidCredentials
	}

	// Get the user's primary tenant and role.
	tenantID, role, err := s.primaryTenant(ctx, user.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve tenant: %w", err)
	}

	pair, err := s.issuePair(ctx, user.ID, tenantID, role)
	if err != nil {
		return nil, nil, err
	}
	return pair, user, nil
}

// RefreshToken rotates a refresh token and returns a new pair.
func (s *Service) RefreshToken(ctx context.Context, rawRefreshToken string) (*TokenPair, error) {
	// The raw token is stored directly — it's 256 bits from crypto/rand,
	// which is equivalent to a strong password for this use case.
	rt, err := s.refreshTokens.GetByHash(ctx, rawRefreshToken)
	if err != nil {
		return nil, fmt.Errorf("lookup refresh token: %w", err)
	}
	if rt == nil {
		return nil, ErrInvalidRefreshToken
	}

	// Revoke old token.
	if err := s.refreshTokens.Revoke(ctx, rt.ID); err != nil {
		return nil, fmt.Errorf("revoke old token: %w", err)
	}

	// Issue a new pair.
	pair, err := s.issuePair(ctx, rt.UserID, rt.TenantID, "member")
	if err != nil {
		return nil, err
	}
	return pair, nil
}

// LoginOIDC authenticates a user via OIDC claims. Creates the user and org if
// they don't exist yet. Returns tokens and user info.
func (s *Service) LoginOIDC(ctx context.Context, claims *OIDCClaims) (*TokenPair, *db.User, error) {
	if claims.Email == "" {
		return nil, nil, fmt.Errorf("OIDC provider did not return an email")
	}

	user, created, err := s.users.FindOrCreateByOIDC(ctx, claims.Provider, claims.Subject, claims.Email, claims.Name)
	if err != nil {
		return nil, nil, fmt.Errorf("find or create OIDC user: %w", err)
	}

	// If the user was just created, create a default org for them.
	if created {
		orgName := claims.Name + "'s Org"
		if claims.Name == "" {
			orgName = strings.Split(claims.Email, "@")[0] + "'s Org"
		}
		tenant, err := s.tenants.Create(ctx, orgName, slugify(orgName), "free")
		if err != nil {
			return nil, nil, fmt.Errorf("create tenant for OIDC user: %w", err)
		}
		if err := s.users.AddToTenant(ctx, tenant.ID, user.ID, "owner"); err != nil {
			return nil, nil, fmt.Errorf("add OIDC user to tenant: %w", err)
		}
	}

	tenantID, role, err := s.primaryTenant(ctx, user.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve tenant for OIDC user: %w", err)
	}

	pair, err := s.issuePair(ctx, user.ID, tenantID, role)
	if err != nil {
		return nil, nil, err
	}
	return pair, user, nil
}

// Logout revokes all refresh tokens for the user whose refresh token is given.
func (s *Service) Logout(ctx context.Context, rawRefreshToken string) error {
	rt, err := s.refreshTokens.GetByHash(ctx, rawRefreshToken)
	if err != nil || rt == nil {
		return nil // already gone — treat as success
	}
	return s.refreshTokens.Revoke(ctx, rt.ID)
}

// issuePair creates an access token + refresh token for the given user.
func (s *Service) issuePair(ctx context.Context, userID, tenantID, role string) (*TokenPair, error) {
	expiresAt := time.Now().Add(AccessTokenTTL)

	accessToken, err := s.tm.Generate(userID, tenantID, role, AccessTokenTTL)
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", err)
	}

	rawRefresh, err := GenerateToken(refreshTokenLen)
	if err != nil {
		return nil, err
	}

	// Store the raw refresh token directly (it has 256 bits of entropy — no need
	// to bcrypt it; bcrypt is for user passwords, not random tokens).
	if _, err := s.refreshTokens.Create(ctx, userID, tenantID, rawRefresh, RefreshTokenTTL); err != nil {
		return nil, fmt.Errorf("store refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
		ExpiresAt:    expiresAt,
	}, nil
}

// primaryTenant returns the first tenant the user belongs to, with their role.
func (s *Service) primaryTenant(ctx context.Context, userID string) (string, string, error) {
	return s.users.GetPrimaryMembership(ctx, userID)
}

// slugify converts a name to a URL-safe slug.
func slugify(name string) string {
	s := strings.ToLower(name)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '_' {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
