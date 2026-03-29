package providers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Engine is the generic OAuth2 engine that works with any registered provider.
type Engine struct {
	Registry *Registry
	States   *StateStore
	BaseURL  string // platform base URL for building redirect URIs
	client   *http.Client
}

// NewEngine creates a generic OAuth2 engine.
func NewEngine(registry *Registry, baseURL string) *Engine {
	return &Engine{
		Registry: registry,
		States:   NewStateStore(),
		BaseURL:  strings.TrimRight(baseURL, "/"),
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// StartAuth builds the authorization URL for a provider and returns it.
// The caller should redirect the user to this URL.
func (e *Engine) StartAuth(ctx context.Context, providerID, tenantID, userID string, extraScopes []string) (string, error) {
	cfg, err := e.Registry.Get(ctx, providerID)
	if err != nil {
		return "", fmt.Errorf("provider %q not found: %w", providerID, err)
	}

	// Generate CSRF state token.
	state, err := randomState()
	if err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}

	// Merge default scopes with any extra scopes.
	scopes := cfg.Scopes
	if len(extraScopes) > 0 {
		scopeSet := make(map[string]bool)
		for _, s := range cfg.Scopes {
			scopeSet[s] = true
		}
		for _, s := range extraScopes {
			if !scopeSet[s] {
				scopes = append(scopes, s)
			}
		}
	}

	// Store pending state.
	e.States.Save(&AuthState{
		State:      state,
		ProviderID: providerID,
		TenantID:   tenantID,
		UserID:     userID,
		Scopes:     scopes,
		CreatedAt:  time.Now(),
	})

	// Build redirect URI.
	redirectPath := cfg.RedirectPath
	if redirectPath == "" {
		redirectPath = fmt.Sprintf("/v1/connect/%s/callback", providerID)
	}
	redirectURI := e.BaseURL + redirectPath

	// Build authorization URL.
	params := url.Values{}
	params.Set("client_id", cfg.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", "code")
	params.Set("state", state)
	params.Set("scope", strings.Join(scopes, " "))

	// Add any provider-specific extra params (e.g. access_type=offline for Google).
	for k, v := range cfg.ExtraAuthParams {
		params.Set(k, v)
	}

	return fmt.Sprintf("%s?%s", cfg.AuthURL, params.Encode()), nil
}

// HandleCallback processes the OAuth2 callback, exchanges the code for tokens,
// and returns the token response. The caller is responsible for storing the
// tokens in the credential broker.
func (e *Engine) HandleCallback(ctx context.Context, stateKey, code string) (*CallbackResult, error) {
	// Validate and consume the state.
	authState := e.States.Pop(stateKey)
	if authState == nil {
		return nil, fmt.Errorf("invalid or expired state parameter")
	}

	cfg, err := e.Registry.Get(ctx, authState.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("provider %q not found: %w", authState.ProviderID, err)
	}

	// Build redirect URI (must match what was used in StartAuth).
	redirectPath := cfg.RedirectPath
	if redirectPath == "" {
		redirectPath = fmt.Sprintf("/v1/connect/%s/callback", authState.ProviderID)
	}
	redirectURI := e.BaseURL + redirectPath

	// Exchange authorization code for tokens.
	tokens, err := e.exchangeCode(cfg, code, redirectURI)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}

	// Parse granted scopes.
	grantedScopes := authState.Scopes
	if tokens.Scope != "" {
		grantedScopes = strings.Split(tokens.Scope, " ")
	}

	return &CallbackResult{
		ProviderID:   authState.ProviderID,
		TenantID:     authState.TenantID,
		UserID:       authState.UserID,
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresIn:    tokens.ExpiresIn,
		Scopes:       grantedScopes,
	}, nil
}

// RefreshToken uses a refresh token to obtain a new access token.
func (e *Engine) RefreshToken(ctx context.Context, providerID, refreshToken string) (*TokenResponse, error) {
	cfg, err := e.Registry.Get(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("provider %q not found: %w", providerID, err)
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", cfg.ClientID)
	data.Set("client_secret", cfg.ClientSecret)

	req, err := http.NewRequest("POST", cfg.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	for k, v := range cfg.ExtraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh failed (HTTP %d): %s", resp.StatusCode, truncate(string(body), 300))
	}

	var tokens TokenResponse
	if err := json.Unmarshal(body, &tokens); err != nil {
		return nil, fmt.Errorf("parse refresh response: %w", err)
	}

	return &tokens, nil
}

// CallbackResult contains the results of a successful OAuth2 callback.
type CallbackResult struct {
	ProviderID   string
	TenantID     string
	UserID       string
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	Scopes       []string
}

// ── Internal helpers ─────────────────────────────────────────────────────────

func (e *Engine) exchangeCode(cfg *ProviderConfig, code, redirectURI string) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)
	data.Set("client_id", cfg.ClientID)
	data.Set("client_secret", cfg.ClientSecret)

	req, err := http.NewRequest("POST", cfg.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	for k, v := range cfg.ExtraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed (HTTP %d): %s", resp.StatusCode, truncate(string(body), 300))
	}

	var tokens TokenResponse
	if err := json.Unmarshal(body, &tokens); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	return &tokens, nil
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
