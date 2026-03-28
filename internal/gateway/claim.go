package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/ai-volund/volund/internal/gateway/api"
	votel "github.com/ai-volund/volund/internal/otel"
)

// AgentInstanceRepo is the interface for agent instance DB operations needed
// by the ClaimManager. Satisfied by *db.AgentRepo.
type AgentInstanceRepo interface {
	ClaimInstance(ctx context.Context, tenantID, profileID string) (*AgentInstanceInfo, error)
	ReleaseInstance(ctx context.Context, id string) error
}

// AgentInstanceInfo is the minimal instance data returned by ClaimInstance.
// Kept separate from db.AgentInstance so the gateway package does not depend
// on the full DB type.
type AgentInstanceInfo struct {
	ID        string
	PodName   string // K8s pod name — used as NATS dispatch subject
	ProfileID string
	State     string
}

// ClaimManager handles pod claim/release for conversations.
// It wraps the DB agent repo and provides conversation-scoped instance
// assignment with an in-memory cache for fast lookups.
type ClaimManager struct {
	repo AgentInstanceRepo
	mu   sync.RWMutex
	// active maps conversation_id → instance_id.
	active map[string]string
}

// NewClaimManager creates a ClaimManager backed by the given repo.
func NewClaimManager(repo AgentInstanceRepo) *ClaimManager {
	return &ClaimManager{
		repo:   repo,
		active: make(map[string]string),
	}
}

// EnsureInstance returns the instance for a conversation, claiming a warm
// pod if one is not already assigned. Returns (nil, nil) when no warm pod is
// available — callers should fall back to pool dispatch.
func (cm *ClaimManager) EnsureInstance(ctx context.Context, convID, tenantID, profileID string) (*api.ClaimResult, error) {
	// Fast path: check in-memory cache.
	cm.mu.RLock()
	if id, ok := cm.active[convID]; ok {
		cm.mu.RUnlock()
		// For cached entries we don't have the pod name; return ID only.
		return &api.ClaimResult{InstanceID: id}, nil
	}
	cm.mu.RUnlock()

	// Slow path: claim from DB.
	inst, err := cm.repo.ClaimInstance(ctx, tenantID, profileID)
	if err != nil {
		return nil, fmt.Errorf("claim instance for conv %s: %w", convID, err)
	}
	if inst == nil {
		// No warm pods available — caller should fall back to pool dispatch.
		return nil, nil
	}

	cm.mu.Lock()
	cm.active[convID] = inst.ID
	cm.mu.Unlock()

	votel.ClaimTotal.Add(ctx, 1, votel.ClaimAttrs("claimed"))
	votel.ActiveInstances.Add(ctx, 1)

	slog.Info("pod claimed for conversation",
		"conv_id", convID,
		"instance_id", inst.ID,
		"pod_name", inst.PodName,
		"tenant_id", tenantID,
		"profile_id", profileID,
	)
	return &api.ClaimResult{InstanceID: inst.ID, PodName: inst.PodName}, nil
}

// Release releases the instance for a conversation back to the warm pool.
// No-op if the conversation has no active instance.
func (cm *ClaimManager) Release(ctx context.Context, convID string) error {
	cm.mu.Lock()
	instanceID, ok := cm.active[convID]
	if !ok {
		cm.mu.Unlock()
		return nil
	}
	delete(cm.active, convID)
	cm.mu.Unlock()

	if err := cm.repo.ReleaseInstance(ctx, instanceID); err != nil {
		return fmt.Errorf("release instance %s for conv %s: %w", instanceID, convID, err)
	}

	votel.ActiveInstances.Add(ctx, -1)

	slog.Info("pod released for conversation",
		"conv_id", convID,
		"instance_id", instanceID,
	)
	return nil
}

// ActiveInstance returns the currently assigned instance for a conversation,
// or "" if none is assigned.
func (cm *ClaimManager) ActiveInstance(convID string) string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.active[convID]
}

// SetActiveInstance records an instance assignment (e.g. from an agent_start
// event) without going through the claim flow.
func (cm *ClaimManager) SetActiveInstance(convID, instanceID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.active[convID] = instanceID
}

// ClearActiveInstance removes the routing entry. Used when agent_end is
// observed but the instance should not be released back to the pool (e.g.
// the watcher handles release separately).
func (cm *ClaimManager) ClearActiveInstance(convID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.active, convID)
}
