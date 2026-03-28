package gateway

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nats-io/nats.go"

	"github.com/ai-volund/volund/internal/db"
)

// instanceSyncer subscribes to agent heartbeat events on NATS and upserts
// corresponding rows in the agent_instances DB table. This bridges the gap
// between K8s-managed warm pool pods and the gateway's DB-backed claim system.
type instanceSyncer struct {
	repo *db.AgentRepo
	sub  *nats.Subscription
}

// heartbeatData matches the CloudEvents payload from volund-agent heartbeats.
type heartbeatData struct {
	AgentID     string `json:"agent_id"`
	InstanceID  string `json:"instance_id"`  // pod name
	TenantID    string `json:"tenant_id"`
	Profile     string `json:"profile"`
	ProfileType string `json:"profile_type"`
	Status      string `json:"status"`
}

// cloudEvent is a minimal CloudEvents envelope for JSON decoding.
type cloudEvent struct {
	Data json.RawMessage `json:"data"`
}

// startInstanceSyncer subscribes to agent heartbeat events and syncs instances to the DB.
func startInstanceSyncer(nc *nats.Conn, repo *db.AgentRepo) (*instanceSyncer, error) {
	s := &instanceSyncer{repo: repo}

	sub, err := nc.Subscribe("volund.events.io.volund.agent.heartbeat", s.handleHeartbeat)
	if err != nil {
		return nil, err
	}
	s.sub = sub
	slog.Info("instance syncer: subscribed to agent heartbeats")
	return s, nil
}

func (s *instanceSyncer) handleHeartbeat(msg *nats.Msg) {
	// Parse CloudEvents envelope.
	var ce cloudEvent
	if err := json.Unmarshal(msg.Data, &ce); err != nil {
		slog.Debug("instance syncer: failed to parse CloudEvent", "error", err)
		return
	}

	var hb heartbeatData
	if err := json.Unmarshal(ce.Data, &hb); err != nil {
		slog.Debug("instance syncer: failed to parse heartbeat data", "error", err)
		return
	}

	if hb.InstanceID == "" || hb.TenantID == "" {
		return
	}

	// Resolve profile name → UUID if we have a profile name and a valid tenant UUID.
	var profileID *string
	if hb.Profile != "" {
		p, err := s.repo.GetProfileByName(context.Background(), hb.TenantID, hb.Profile)
		if err == nil && p != nil {
			profileID = &p.ID
		}
	}

	id, err := s.repo.UpsertInstanceByPodName(
		context.Background(),
		hb.TenantID,
		profileID,
		hb.InstanceID, // pod name
		"default",     // namespace
		"warm",        // state — heartbeating pods are warm
	)
	if err != nil {
		slog.Warn("instance syncer: upsert failed", "pod", hb.InstanceID, "error", err)
		return
	}

	slog.Debug("instance syncer: heartbeat synced", "pod", hb.InstanceID, "id", id)
}

func (s *instanceSyncer) Close() {
	if s.sub != nil {
		_ = s.sub.Unsubscribe()
	}
}
