package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDCProviderConfig holds settings for a single OIDC identity provider.
type OIDCProviderConfig struct {
	Name         string // e.g. "google", "github", "okta"
	ClientID     string
	ClientSecret string
	Issuer       string // e.g. "https://accounts.google.com"
	RedirectURL  string // e.g. "http://localhost:8080/v1/auth/oidc/google/callback"
	Scopes       []string
}

// OIDCManager manages multiple OIDC providers and handles the OAuth2 flow.
type OIDCManager struct {
	providers map[string]*oidcProvider

	// stateStore is a simple in-memory state→provider map for CSRF protection.
	// In production, this should be backed by Redis/DB with TTL.
	mu     sync.Mutex
	states map[string]string // state → provider name
}

type oidcProvider struct {
	name     string
	verifier *oidc.IDTokenVerifier
	oauth2   oauth2.Config
}

// OIDCClaims holds the identity claims extracted from an OIDC ID token.
type OIDCClaims struct {
	Subject  string // provider's unique user ID
	Email    string
	Name     string
	Provider string
}

// NewOIDCManager creates an OIDCManager from provider configs.
// Performs provider discovery (fetches .well-known/openid-configuration).
func NewOIDCManager(ctx context.Context, configs []OIDCProviderConfig) (*OIDCManager, error) {
	m := &OIDCManager{
		providers: make(map[string]*oidcProvider),
		states:    make(map[string]string),
	}
	for _, cfg := range configs {
		provider, err := oidc.NewProvider(ctx, cfg.Issuer)
		if err != nil {
			return nil, fmt.Errorf("oidc: discover %s (%s): %w", cfg.Name, cfg.Issuer, err)
		}

		scopes := cfg.Scopes
		if len(scopes) == 0 {
			scopes = []string{oidc.ScopeOpenID, "profile", "email"}
		}

		m.providers[cfg.Name] = &oidcProvider{
			name: cfg.Name,
			verifier: provider.Verifier(&oidc.Config{
				ClientID: cfg.ClientID,
			}),
			oauth2: oauth2.Config{
				ClientID:     cfg.ClientID,
				ClientSecret: cfg.ClientSecret,
				Endpoint:     provider.Endpoint(),
				RedirectURL:  cfg.RedirectURL,
				Scopes:       scopes,
			},
		}
	}
	return m, nil
}

// HasProvider returns true if the named provider is configured.
func (m *OIDCManager) HasProvider(name string) bool {
	_, ok := m.providers[name]
	return ok
}

// Providers returns the list of configured provider names.
func (m *OIDCManager) Providers() []string {
	names := make([]string, 0, len(m.providers))
	for name := range m.providers {
		names = append(names, name)
	}
	return names
}

// AuthURL generates the OAuth2 authorization URL for the given provider.
// Returns the URL and a CSRF state token that must be verified on callback.
func (m *OIDCManager) AuthURL(provider string) (string, string, error) {
	p, ok := m.providers[provider]
	if !ok {
		return "", "", fmt.Errorf("oidc: unknown provider %q", provider)
	}

	state, err := generateState()
	if err != nil {
		return "", "", err
	}

	m.mu.Lock()
	m.states[state] = provider
	m.mu.Unlock()

	url := p.oauth2.AuthCodeURL(state)
	return url, state, nil
}

// Exchange handles the OAuth2 callback: validates state, exchanges the auth code
// for tokens, verifies the ID token, and returns the user's identity claims.
func (m *OIDCManager) Exchange(ctx context.Context, provider, code, state string) (*OIDCClaims, error) {
	// Verify state.
	m.mu.Lock()
	expectedProvider, ok := m.states[state]
	if ok {
		delete(m.states, state)
	}
	m.mu.Unlock()

	if !ok || expectedProvider != provider {
		return nil, fmt.Errorf("oidc: invalid state parameter")
	}

	p, ok := m.providers[provider]
	if !ok {
		return nil, fmt.Errorf("oidc: unknown provider %q", provider)
	}

	// Exchange authorization code for tokens.
	token, err := p.oauth2.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("oidc: token exchange for %s: %w", provider, err)
	}

	// Extract and verify the ID token.
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("oidc: no id_token in response from %s", provider)
	}

	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("oidc: verify id_token from %s: %w", provider, err)
	}

	// Extract claims.
	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("oidc: extract claims from %s: %w", provider, err)
	}

	return &OIDCClaims{
		Subject:  idToken.Subject,
		Email:    claims.Email,
		Name:     claims.Name,
		Provider: provider,
	}, nil
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
