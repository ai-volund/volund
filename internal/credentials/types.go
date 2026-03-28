package credentials

import "time"

// StoreRequest is the input for storing a credential.
type StoreRequest struct {
	TenantID     string
	UserID       string
	Provider     string     // github, slack, jira, etc.
	Token        string     // raw token (will be encrypted)
	RefreshToken string     // optional OAuth2 refresh token
	Scopes       []string
	TokenType    string     // oauth2, api_key, pat
	ExpiresAt    *time.Time // nil if non-expiring
}

// TokenRequest is the input for issuing a short-lived token to an agent.
type TokenRequest struct {
	TenantID        string
	UserID          string   // authorization identity — whose credential to use
	Provider        string
	Scopes          []string // requested scopes (intersected with stored)
	AgentInstanceID string   // execution identity — which agent pod
	ConversationID  string
	SkillName       string   // which skill is requesting the credential
}

// TokenResponse is the short-lived token returned to the agent.
type TokenResponse struct {
	Token     string   `json:"token"`
	ExpiresIn int      `json:"expires_in"` // seconds
	Scopes    []string `json:"scopes"`
}

// ProviderInfo is a non-sensitive summary of a connected provider.
type ProviderInfo struct {
	Provider  string     `json:"provider"`
	TokenType string     `json:"token_type"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}
