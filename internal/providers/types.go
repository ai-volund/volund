// Package providers implements a generic OAuth2 engine for connecting
// external service accounts. Providers are configured at runtime (not compiled),
// so new OAuth2 services can be added without recompiling the application.
package providers

import "time"

// ProviderConfig defines an OAuth2 provider's connection details.
// Provider definitions come from skill manifests. Admins only supply credentials.
type ProviderConfig struct {
	ID           string   `json:"id"`            // unique key, e.g. "gmail", "slack", "notion"
	DisplayName  string   `json:"display_name"`  // human-readable, e.g. "Gmail", "Slack"
	AuthURL      string   `json:"auth_url"`      // authorization endpoint
	TokenURL     string   `json:"token_url"`     // token exchange endpoint
	ClientID     string   `json:"client_id"`     // OAuth2 client ID (supplied by admin)
	ClientSecret string   `json:"-"`             // OAuth2 client secret (supplied by admin, never serialized)
	Scopes       []string `json:"scopes"`        // default scopes to request
	RedirectPath string   `json:"redirect_path"` // callback path (default: /v1/connect/{id}/callback)

	// Optional fields for providers with non-standard behavior.
	ExtraAuthParams map[string]string `json:"extra_auth_params,omitempty"` // e.g. {"access_type": "offline", "prompt": "consent"}
	ExtraHeaders    map[string]string `json:"extra_headers,omitempty"`     // extra headers for token request

	// Metadata
	IconURL    string `json:"icon_url,omitempty"`
	Category   string `json:"category,omitempty"`    // "email", "calendar", "messaging", etc.
	SkillID    string `json:"skill_id,omitempty"`    // which skill registered this provider
	Configured bool   `json:"configured"`            // true when admin has supplied client_id + client_secret

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AuthState tracks an in-progress OAuth2 authorization flow.
// Stored temporarily to validate callbacks and prevent CSRF.
type AuthState struct {
	State      string // random CSRF token
	ProviderID string
	TenantID   string
	UserID     string
	Scopes     []string // scopes requested for this specific flow
	CreatedAt  time.Time
}

// TokenResponse is the standard OAuth2 token exchange response.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}
