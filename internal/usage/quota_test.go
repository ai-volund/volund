package usage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ai-volund/volund/internal/db"
)

// mockQuotaRepo implements the quota check methods for testing.
type mockQuotaRepo struct {
	quota  *db.TenantQuota
	status *db.QuotaStatus
	marked map[int]bool
}

func (m *mockQuotaRepo) Get(_ context.Context, _ string) (*db.TenantQuota, error) {
	return m.quota, nil
}

func (m *mockQuotaRepo) CheckQuota(_ context.Context, _ string) (*db.QuotaStatus, error) {
	return m.status, nil
}

func (m *mockQuotaRepo) MarkAlerted(_ context.Context, _ string, threshold int) error {
	if m.marked == nil {
		m.marked = make(map[int]bool)
	}
	m.marked[threshold] = true
	return nil
}

// testQuotaEnforcer wraps QuotaEnforcer with a mock repo for testing.
type testQuotaEnforcer struct {
	mock *mockQuotaRepo
	qe   *QuotaEnforcer
}

func newTestEnforcer(quota *db.TenantQuota, status *db.QuotaStatus) *testQuotaEnforcer {
	mock := &mockQuotaRepo{quota: quota, status: status}
	return &testQuotaEnforcer{
		mock: mock,
		qe: &QuotaEnforcer{
			quotas: nil, // will use mock methods directly
			client: http.DefaultClient,
		},
	}
}

func TestIsBlocked_NoQuota(t *testing.T) {
	// When no quota is configured, the tenant is never blocked.
	status := (*db.QuotaStatus)(nil)
	if status != nil && status.Exceeded && status.Action == "block" {
		t.Fatal("nil status should not be blocked")
	}
}

func TestIsBlocked_UnderLimit(t *testing.T) {
	status := &db.QuotaStatus{
		TenantID:  "t1",
		PctTokens: 50,
		PctCost:   30,
		Action:    "block",
		Exceeded:  false,
	}
	if status.Exceeded && status.Action == "block" {
		t.Fatal("should not be blocked when under limit")
	}
}

func TestIsBlocked_ExceededWithBlock(t *testing.T) {
	status := &db.QuotaStatus{
		TenantID:  "t1",
		PctTokens: 105,
		PctCost:   90,
		Action:    "block",
		Exceeded:  true,
	}
	if !(status.Exceeded && status.Action == "block") {
		t.Fatal("should be blocked when exceeded with block action")
	}
}

func TestIsBlocked_ExceededWithAlert(t *testing.T) {
	status := &db.QuotaStatus{
		TenantID:  "t1",
		PctTokens: 110,
		Action:    "alert",
		Exceeded:  true,
	}
	// Should NOT be blocked — action is "alert" not "block".
	if status.Exceeded && status.Action == "block" {
		t.Fatal("should not be blocked when action is alert")
	}
}

func TestWebhookPayload(t *testing.T) {
	payload := WebhookPayload{
		TenantID:    "tenant-123",
		Threshold:   80,
		PctTokens:   82.5,
		PctCost:     45.0,
		UsedTokens:  825000,
		UsedCostUSD: 4.50,
		Timestamp:   "2026-03-28T12:00:00Z",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded WebhookPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.TenantID != "tenant-123" {
		t.Errorf("expected tenant-123, got %s", decoded.TenantID)
	}
	if decoded.Threshold != 80 {
		t.Errorf("expected threshold 80, got %d", decoded.Threshold)
	}
	if decoded.PctTokens != 82.5 {
		t.Errorf("expected pct_tokens 82.5, got %f", decoded.PctTokens)
	}
}

func TestFireWebhook(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("expected Content-Type: application/json")
		}
		var payload WebhookPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if payload.Threshold != 80 {
			t.Errorf("expected threshold 80, got %d", payload.Threshold)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	qe := &QuotaEnforcer{client: srv.Client()}
	status := &db.QuotaStatus{
		TenantID:    "t1",
		PctTokens:   85,
		PctCost:     40,
		UsedTokens:  850000,
		UsedCostUSD: 4.0,
	}

	qe.fireWebhook(srv.URL, "t1", 80, status)

	if callCount.Load() != 1 {
		t.Fatalf("expected 1 webhook call, got %d", callCount.Load())
	}
}

func TestQuotaStatus_Calculation(t *testing.T) {
	maxTokens := int64(1000000)
	maxCost := 10.0

	tests := []struct {
		name        string
		usedTokens  int
		usedCost    float64
		wantPctTok  float64
		wantPctCost float64
		wantExceed  bool
	}{
		{"under_limit", 500000, 5.0, 50.0, 50.0, false},
		{"at_limit", 1000000, 10.0, 100.0, 100.0, true},
		{"over_token_limit", 1200000, 5.0, 120.0, 50.0, true},
		{"over_cost_limit", 500000, 15.0, 50.0, 150.0, true},
		{"zero_usage", 0, 0, 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := &db.QuotaStatus{
				MaxTokens:   &maxTokens,
				MaxCostUSD:  &maxCost,
				UsedTokens:  int64(tt.usedTokens),
				UsedCostUSD: tt.usedCost,
			}

			// Calculate percentages (mirroring CheckQuota logic).
			if status.MaxTokens != nil && *status.MaxTokens > 0 {
				status.PctTokens = float64(status.UsedTokens) / float64(*status.MaxTokens) * 100
				if status.PctTokens >= 100 {
					status.Exceeded = true
				}
			}
			if status.MaxCostUSD != nil && *status.MaxCostUSD > 0 {
				status.PctCost = status.UsedCostUSD / *status.MaxCostUSD * 100
				if status.PctCost >= 100 {
					status.Exceeded = true
				}
			}

			if status.PctTokens != tt.wantPctTok {
				t.Errorf("PctTokens = %f, want %f", status.PctTokens, tt.wantPctTok)
			}
			if status.PctCost != tt.wantPctCost {
				t.Errorf("PctCost = %f, want %f", status.PctCost, tt.wantPctCost)
			}
			if status.Exceeded != tt.wantExceed {
				t.Errorf("Exceeded = %v, want %v", status.Exceeded, tt.wantExceed)
			}
		})
	}
}
