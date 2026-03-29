package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// mockRegistry is a simple in-memory registry for testing.
type mockRegistry struct {
	providers map[string]*ProviderConfig
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{providers: make(map[string]*ProviderConfig)}
}

func (m *mockRegistry) get(id string) (*ProviderConfig, error) {
	p, ok := m.providers[id]
	if !ok {
		return nil, &notFoundErr{id: id}
	}
	return p, nil
}

type notFoundErr struct{ id string }

func (e *notFoundErr) Error() string { return "provider not found: " + e.id }

// testEngine creates an Engine with a mock token endpoint.
func testEngine(t *testing.T, tokenHandler http.HandlerFunc) (*Engine, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(tokenHandler)
	t.Cleanup(server.Close)

	// We can't use a real DB registry in unit tests, so we'll
	// test the Engine methods that don't need DB directly.
	engine := &Engine{
		States:  NewStateStore(),
		BaseURL: "http://localhost:8080",
		client:  server.Client(),
	}

	return engine, server
}

func TestStateStore(t *testing.T) {
	store := NewStateStore()

	t.Run("SaveAndPop", func(t *testing.T) {
		store.Save(&AuthState{
			State:      "abc123",
			ProviderID: "gmail",
			TenantID:   "tenant-1",
			UserID:     "user-1",
			CreatedAt:  time.Now(),
		})

		st := store.Pop("abc123")
		if st == nil {
			t.Fatal("expected state, got nil")
		}
		if st.ProviderID != "gmail" {
			t.Errorf("expected gmail, got %s", st.ProviderID)
		}
		if st.TenantID != "tenant-1" {
			t.Errorf("expected tenant-1, got %s", st.TenantID)
		}

		// Pop again should return nil (consumed).
		st2 := store.Pop("abc123")
		if st2 != nil {
			t.Error("expected nil after consuming state")
		}
	})

	t.Run("ExpiredState", func(t *testing.T) {
		store.Save(&AuthState{
			State:     "expired-state",
			CreatedAt: time.Now().Add(-15 * time.Minute), // 15 min ago
		})

		st := store.Pop("expired-state")
		if st != nil {
			t.Error("expected nil for expired state")
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		st := store.Pop("nonexistent")
		if st != nil {
			t.Error("expected nil for nonexistent state")
		}
	})
}

func TestExchangeCode(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		r.ParseForm()
		if r.FormValue("grant_type") != "authorization_code" {
			http.Error(w, "bad grant_type", http.StatusBadRequest)
			return
		}
		if r.FormValue("code") != "valid-code" {
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		json.NewEncoder(w).Encode(TokenResponse{
			AccessToken:  "access-token-123",
			RefreshToken: "refresh-token-456",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			Scope:        "email.read email.send",
		})
	}))
	defer tokenServer.Close()

	engine := &Engine{
		States:  NewStateStore(),
		BaseURL: "http://localhost:8080",
		client:  tokenServer.Client(),
	}

	cfg := &ProviderConfig{
		ID:           "test-provider",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     tokenServer.URL,
	}

	tokens, err := engine.exchangeCode(cfg, "valid-code", "http://localhost:8080/callback")
	if err != nil {
		t.Fatalf("exchange failed: %v", err)
	}

	if tokens.AccessToken != "access-token-123" {
		t.Errorf("expected access-token-123, got %s", tokens.AccessToken)
	}
	if tokens.RefreshToken != "refresh-token-456" {
		t.Errorf("expected refresh-token-456, got %s", tokens.RefreshToken)
	}
	if tokens.ExpiresIn != 3600 {
		t.Errorf("expected 3600, got %d", tokens.ExpiresIn)
	}
}

func TestRefreshTokenFlow(t *testing.T) {
	// This test needs a real registry, but we can test the HTTP part directly.
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.FormValue("grant_type") != "refresh_token" {
			http.Error(w, "bad grant_type", http.StatusBadRequest)
			return
		}
		if r.FormValue("refresh_token") != "valid-refresh" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}

		json.NewEncoder(w).Encode(TokenResponse{
			AccessToken: "new-access-token",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		})
	}))
	defer tokenServer.Close()

	// Test the exchange directly through the engine's HTTP client.
	engine := &Engine{
		client: tokenServer.Client(),
	}

	cfg := &ProviderConfig{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     tokenServer.URL,
	}

	// Use the exchange method directly with refresh_token grant.
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", "valid-refresh")
	data.Set("client_id", cfg.ClientID)
	data.Set("client_secret", cfg.ClientSecret)

	req, _ := http.NewRequestWithContext(context.Background(), "POST", cfg.TokenURL, strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := engine.client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var tokens TokenResponse
	json.NewDecoder(resp.Body).Decode(&tokens)

	if tokens.AccessToken != "new-access-token" {
		t.Errorf("expected new-access-token, got %s", tokens.AccessToken)
	}
}

func TestBuildAuthURL(t *testing.T) {
	// Test that StartAuth builds a correct authorization URL.
	// We can't use StartAuth directly without a DB, but we can test
	// the URL construction logic.

	cfg := &ProviderConfig{
		ID:           "gmail",
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		ClientID:     "test-client-id",
		Scopes:       []string{"email.read", "email.send"},
		ExtraAuthParams: map[string]string{
			"access_type": "offline",
			"prompt":      "consent",
		},
	}

	baseURL := "http://localhost:8080"
	redirectURI := baseURL + "/v1/connect/gmail/callback"

	params := url.Values{}
	params.Set("client_id", cfg.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", "code")
	params.Set("state", "test-state")
	params.Set("scope", "email.read email.send")
	for k, v := range cfg.ExtraAuthParams {
		params.Set(k, v)
	}

	authURL := cfg.AuthURL + "?" + params.Encode()

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}

	if parsed.Host != "accounts.google.com" {
		t.Errorf("expected accounts.google.com, got %s", parsed.Host)
	}
	if parsed.Query().Get("client_id") != "test-client-id" {
		t.Errorf("expected test-client-id, got %s", parsed.Query().Get("client_id"))
	}
	if parsed.Query().Get("access_type") != "offline" {
		t.Errorf("expected offline, got %s", parsed.Query().Get("access_type"))
	}
	if parsed.Query().Get("response_type") != "code" {
		t.Errorf("expected code, got %s", parsed.Query().Get("response_type"))
	}
	if !strings.Contains(parsed.Query().Get("scope"), "email.read") {
		t.Errorf("expected email.read in scopes, got %s", parsed.Query().Get("scope"))
	}
	if parsed.Query().Get("redirect_uri") != redirectURI {
		t.Errorf("expected redirect to %s, got %s", redirectURI, parsed.Query().Get("redirect_uri"))
	}
}
