package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/ai-volund/volund/internal/gateway/api"
)

// mockRepo is a test double for AgentInstanceRepo.
type mockRepo struct {
	claimFn   func(ctx context.Context, tenantID, profileID string) (*AgentInstanceInfo, error)
	releaseFn func(ctx context.Context, id string) error

	claimCalls   int
	releaseCalls int
	lastRelaseID string
}

func (m *mockRepo) ClaimInstance(ctx context.Context, tenantID, profileID string) (*AgentInstanceInfo, error) {
	m.claimCalls++
	if m.claimFn != nil {
		return m.claimFn(ctx, tenantID, profileID)
	}
	return &AgentInstanceInfo{ID: "inst-001", PodName: "pod-001", ProfileID: profileID, State: "active"}, nil
}

func (m *mockRepo) ReleaseInstance(ctx context.Context, id string) error {
	m.releaseCalls++
	m.lastRelaseID = id
	if m.releaseFn != nil {
		return m.releaseFn(ctx, id)
	}
	return nil
}

func TestClaimManager_EnsureInstance_ClaimNew(t *testing.T) {
	repo := &mockRepo{}
	cm := NewClaimManager(repo)

	result, err := cm.EnsureInstance(context.Background(), "conv-1", "tenant-1", "profile-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.InstanceID != "inst-001" {
		t.Fatalf("expected inst-001, got %+v", result)
	}
	if repo.claimCalls != 1 {
		t.Fatalf("expected 1 claim call, got %d", repo.claimCalls)
	}
	// Verify it was cached.
	if cm.ActiveInstance("conv-1") != "inst-001" {
		t.Fatal("expected active instance to be cached")
	}
}

func TestClaimManager_EnsureInstance_ReturnsCached(t *testing.T) {
	repo := &mockRepo{}
	cm := NewClaimManager(repo)

	// First call claims from DB.
	_, _ = cm.EnsureInstance(context.Background(), "conv-1", "tenant-1", "profile-1")

	// Second call should return from cache without hitting the repo.
	result, err := cm.EnsureInstance(context.Background(), "conv-1", "tenant-1", "profile-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.InstanceID != "inst-001" {
		t.Fatalf("expected inst-001, got %+v", result)
	}
	if repo.claimCalls != 1 {
		t.Fatalf("expected 1 claim call (cached), got %d", repo.claimCalls)
	}
}

func TestClaimManager_Release(t *testing.T) {
	repo := &mockRepo{}
	cm := NewClaimManager(repo)

	// Claim first.
	_, _ = cm.EnsureInstance(context.Background(), "conv-1", "tenant-1", "profile-1")

	// Release.
	if err := cm.Release(context.Background(), "conv-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.releaseCalls != 1 {
		t.Fatalf("expected 1 release call, got %d", repo.releaseCalls)
	}
	if repo.lastRelaseID != "inst-001" {
		t.Fatalf("expected release of inst-001, got %s", repo.lastRelaseID)
	}
	// Verify cache is cleared.
	if cm.ActiveInstance("conv-1") != "" {
		t.Fatal("expected active instance to be cleared after release")
	}
}

func TestClaimManager_Release_Unknown(t *testing.T) {
	repo := &mockRepo{}
	cm := NewClaimManager(repo)

	// Release a conversation that was never claimed — should be a no-op.
	if err := cm.Release(context.Background(), "conv-unknown"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.releaseCalls != 0 {
		t.Fatalf("expected 0 release calls for unknown conv, got %d", repo.releaseCalls)
	}
}

func TestClaimManager_EnsureInstance_ClaimFails(t *testing.T) {
	repo := &mockRepo{
		claimFn: func(_ context.Context, _, _ string) (*AgentInstanceInfo, error) {
			return nil, errors.New("db connection lost")
		},
	}
	cm := NewClaimManager(repo)

	result, err := cm.EnsureInstance(context.Background(), "conv-1", "tenant-1", "profile-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result != nil {
		t.Fatalf("expected nil result on error, got %+v", result)
	}
	// Verify nothing was cached.
	if cm.ActiveInstance("conv-1") != "" {
		t.Fatal("expected no active instance after claim failure")
	}
}

func TestClaimManager_EnsureInstance_NoWarmPods(t *testing.T) {
	repo := &mockRepo{
		claimFn: func(_ context.Context, _, _ string) (*AgentInstanceInfo, error) {
			return nil, nil // no warm pods
		},
	}
	cm := NewClaimManager(repo)

	result, err := cm.EnsureInstance(context.Background(), "conv-1", "tenant-1", "profile-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil when no warm pods, got %+v", result)
	}
}

// Ensure ClaimManager satisfies the api.InstanceClaimer interface.
var _ api.InstanceClaimer = (*ClaimManager)(nil)
