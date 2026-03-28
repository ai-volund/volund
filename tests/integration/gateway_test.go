//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ai-volund/volund/internal/auth"
	"github.com/ai-volund/volund/internal/db"
	"github.com/ai-volund/volund/internal/gateway/api"
)

func TestGatewayAPI(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	pool := db.WrapPool(env.PgPool)
	tm := auth.NewTokenManager("test-jwt-secret-32bytes-long!!!")

	svc := &api.Services{
		Auth:    auth.NewService(tm, db.NewUserRepo(pool), db.NewTenantRepo(pool), db.NewRefreshTokenRepo(pool)),
		TM:      tm,
		Users:   db.NewUserRepo(pool),
		Tenants: db.NewTenantRepo(pool),
		Agents:  db.NewAgentRepo(pool),
		Convos:  db.NewConversationRepo(pool),
		Invites: db.NewInviteRepo(pool),
		Tasks:   db.NewTaskRepo(pool),
		Skills:  db.NewSkillRepo(pool),
		Memories: db.NewMemoryRepo(pool),
		Credentials: db.NewCredentialRepo(pool),
		Usage:   db.NewUsageRepo(pool),
	}

	mux := http.NewServeMux()
	api.Register(mux, svc)
	handler := auth.HTTPMiddleware(tm, mux)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := ts.Client()

	// --- Register ---
	t.Run("register", func(t *testing.T) {
		body := `{"email":"test@volund.dev","password":"strongpassword123","display_name":"Test User"}`
		resp, err := client.Post(ts.URL+"/v1/auth/register", "application/json", bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("register: expected 2xx, got %d: %s", resp.StatusCode, string(b))
		}
	})

	// --- Login ---
	var accessToken string
	t.Run("login", func(t *testing.T) {
		body := `{"email":"test@volund.dev","password":"strongpassword123"}`
		resp, err := client.Post(ts.URL+"/v1/auth/login", "application/json", bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("login: expected 200, got %d: %s", resp.StatusCode, string(b))
		}
		var result struct {
			AccessToken string `json:"access_token"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		accessToken = result.AccessToken
		if accessToken == "" {
			t.Fatal("login: no access_token returned")
		}
	})

	authReq := func(method, url string, body string) *http.Request {
		var bodyReader io.Reader
		if body != "" {
			bodyReader = bytes.NewBufferString(body)
		}
		req, _ := http.NewRequest(method, url, bodyReader)
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		return req
	}

	// --- Get profile ---
	t.Run("me", func(t *testing.T) {
		resp, err := client.Do(authReq("GET", ts.URL+"/v1/auth/me", ""))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("me: expected 200, got %d: %s", resp.StatusCode, string(b))
		}
	})

	// --- List tenants ---
	var tenantID string
	t.Run("list_tenants", func(t *testing.T) {
		resp, err := client.Do(authReq("GET", ts.URL+"/v1/tenants", ""))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("list_tenants: expected 200, got %d: %s", resp.StatusCode, string(b))
		}
		var result struct {
			Tenants []struct {
				ID string `json:"id"`
			} `json:"tenants"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		if len(result.Tenants) == 0 {
			t.Fatal("expected at least one tenant after registration")
		}
		tenantID = result.Tenants[0].ID
	})

	// --- Create agent ---
	var agentID string
	t.Run("create_agent", func(t *testing.T) {
		body := fmt.Sprintf(`{"tenant_id":%q,"name":"test-agent","profile_type":"specialist","provider":"anthropic","model":"claude-sonnet-4-5"}`, tenantID)
		resp, err := client.Do(authReq("POST", ts.URL+"/v1/agents", body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("create_agent: expected 2xx, got %d: %s", resp.StatusCode, string(b))
		}
		var result struct {
			ID string `json:"id"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		agentID = result.ID
	})

	// --- List agents ---
	t.Run("list_agents", func(t *testing.T) {
		resp, err := client.Do(authReq("GET", ts.URL+"/v1/agents", ""))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("list_agents: expected 200, got %d: %s", resp.StatusCode, string(b))
		}
	})

	// --- Create conversation ---
	var convID string
	t.Run("create_conversation", func(t *testing.T) {
		body := fmt.Sprintf(`{"tenant_id":%q,"agent_id":%q,"title":"Integration Test Conv"}`, tenantID, agentID)
		resp, err := client.Do(authReq("POST", ts.URL+"/v1/conversations", body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("create_conversation: expected 2xx, got %d: %s", resp.StatusCode, string(b))
		}
		var result struct {
			ID string `json:"id"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		convID = result.ID
		_ = convID
	})

	// --- Usage summary (empty) ---
	t.Run("usage_summary", func(t *testing.T) {
		resp, err := client.Do(authReq("GET", ts.URL+"/v1/usage/summary", ""))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("usage_summary: expected 200, got %d: %s", resp.StatusCode, string(b))
		}
	})

	// --- Forge skills (public, empty) ---
	t.Run("list_skills", func(t *testing.T) {
		resp, err := client.Get(ts.URL + "/v1/forge/skills")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("list_skills: expected 200, got %d: %s", resp.StatusCode, string(b))
		}
	})
}
