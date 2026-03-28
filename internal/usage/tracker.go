package usage

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nats-io/nats.go"

	"github.com/ai-volund/volund/internal/db"
)

// Tracker subscribes to usage events on NATS and persists them to the DB.
type Tracker struct {
	repo *db.UsageRepo
	sub  *nats.Subscription
}

// UsageEventData is the CloudEvent data payload for token usage.
type UsageEventData struct {
	TenantID       string `json:"tenant_id"`
	ConversationID string `json:"conversation_id,omitempty"`
	TaskID         string `json:"task_id,omitempty"`
	InstanceID     string `json:"instance_id,omitempty"`
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	InputTokens    int    `json:"input_tokens"`
	OutputTokens   int    `json:"output_tokens"`
}

type cloudEvent struct {
	Data json.RawMessage `json:"data"`
}

const usageSubject = "volund.events.io.volund.usage.tokens"

// StartTracker begins listening for usage events on NATS.
func StartTracker(nc *nats.Conn, repo *db.UsageRepo) (*Tracker, error) {
	t := &Tracker{repo: repo}
	sub, err := nc.Subscribe(usageSubject, t.handleEvent)
	if err != nil {
		return nil, err
	}
	t.sub = sub
	slog.Info("usage tracker: subscribed to usage events", "subject", usageSubject)
	return t, nil
}

func (t *Tracker) handleEvent(msg *nats.Msg) {
	var ce cloudEvent
	if err := json.Unmarshal(msg.Data, &ce); err != nil {
		slog.Debug("usage tracker: failed to parse CloudEvent", "error", err)
		return
	}
	var data UsageEventData
	if err := json.Unmarshal(ce.Data, &data); err != nil {
		slog.Debug("usage tracker: failed to parse usage data", "error", err)
		return
	}
	if data.TenantID == "" || data.Model == "" {
		return
	}

	costUSD := CalculateCost(data.Model, data.InputTokens, data.OutputTokens)

	var convID *string
	if data.ConversationID != "" {
		convID = &data.ConversationID
	}

	event := &db.UsageEvent{
		TenantID:       data.TenantID,
		ConversationID: convID,
		TaskID:         data.TaskID,
		InstanceID:     data.InstanceID,
		Provider:       data.Provider,
		Model:          data.Model,
		InputTokens:    data.InputTokens,
		OutputTokens:   data.OutputTokens,
		CostUSD:        costUSD,
	}

	if err := t.repo.Record(context.Background(), event); err != nil {
		slog.Warn("usage tracker: failed to persist event", "error", err)
		return
	}
	slog.Debug("usage tracker: recorded",
		"model", data.Model,
		"input", data.InputTokens,
		"output", data.OutputTokens,
		"tenant", data.TenantID)
}

// Close stops the subscription.
func (t *Tracker) Close() {
	if t.sub != nil {
		_ = t.sub.Unsubscribe()
	}
}
