package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"time"

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
// It wraps the DB agent repo and delegates the conv→instance routing cache
// to a RoutingTable (Redis for multi-gateway, in-memory for single-gateway).
type ClaimManager struct {
	repo  AgentInstanceRepo
	route RoutingTable
}

// NewClaimManager creates a ClaimManager backed by the given repo and routing table.
func NewClaimManager(repo AgentInstanceRepo, route RoutingTable) *ClaimManager {
	return &ClaimManager{
		repo:  repo,
		route: route,
	}
}

// EnsureInstance returns the instance for a conversation, claiming a warm
// pod if one is not already assigned. Returns (nil, nil) when no warm pod is
// available — callers should fall back to pool dispatch.
func (cm *ClaimManager) EnsureInstance(ctx context.Context, convID, tenantID, profileID string) (*api.ClaimResult, error) {
	start := time.Now()

	// Fast path: check routing table (Redis or in-memory).
	if id := cm.route.Get(convID); id != "" {
		dur := time.Since(start).Seconds()
		votel.ClaimDuration.Record(ctx, dur, votel.ClaimRecordAttrs("hit", "cache_hit"))
		return &api.ClaimResult{InstanceID: id}, nil
	}

	// Slow path: claim from DB.
	inst, err := cm.repo.ClaimInstance(ctx, tenantID, profileID)
	if err != nil {
		dur := time.Since(start).Seconds()
		votel.ClaimDuration.Record(ctx, dur, votel.ClaimRecordAttrs("error", "db_claim"))
		votel.ClaimTotal.Add(ctx, 1, votel.ClaimAttrs("error"))
		return nil, fmt.Errorf("claim instance for conv %s: %w", convID, err)
	}
	if inst == nil {
		// No warm pods available — caller should fall back to pool dispatch.
		dur := time.Since(start).Seconds()
		votel.ClaimDuration.Record(ctx, dur, votel.ClaimRecordAttrs("unavailable", "db_claim"))
		votel.ClaimTotal.Add(ctx, 1, votel.ClaimAttrs("unavailable"))
		return nil, nil
	}

	cm.route.Set(convID, inst.ID, 0) // default TTL

	dur := time.Since(start).Seconds()
	votel.ClaimDuration.Record(ctx, dur, votel.ClaimRecordAttrs("claimed", "db_claim"))
	votel.ClaimTotal.Add(ctx, 1, votel.ClaimAttrs("claimed"))
	votel.ActiveInstances.Add(ctx, 1)

	slog.Info("pod claimed for conversation",
		"conv_id", convID,
		"instance_id", inst.ID,
		"pod_name", inst.PodName,
		"tenant_id", tenantID,
		"profile_id", profileID,
		"claim_duration_ms", time.Since(start).Milliseconds(),
	)
	return &api.ClaimResult{InstanceID: inst.ID, PodName: inst.PodName}, nil
}

// Release releases the instance for a conversation back to the warm pool.
// No-op if the conversation has no active instance.
func (cm *ClaimManager) Release(ctx context.Context, convID string) error {
	instanceID := cm.route.Get(convID)
	if instanceID == "" {
		return nil
	}
	cm.route.Delete(convID)

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
// or "" if none is assigned. Reads from the shared routing table.
func (cm *ClaimManager) ActiveInstance(convID string) string {
	return cm.route.Get(convID)
}

// SetActiveInstance records an instance assignment (e.g. from an agent_start
// event) without going through the claim flow.
func (cm *ClaimManager) SetActiveInstance(convID, instanceID string) {
	cm.route.Set(convID, instanceID, 0)
}

// ClearActiveInstance removes the routing entry. Used when agent_end is
// observed but the instance should not be released back to the pool (e.g.
// the watcher handles release separately).
func (cm *ClaimManager) ClearActiveInstance(convID string) {
	cm.route.Delete(convID)
}
