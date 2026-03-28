package dispatch

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go"
)

// StreamEvent mirrors the lightweight JSON events published by agent pods
// to volund.conv.{convId}.stream.
type StreamEvent struct {
	Type        string `json:"type"`
	TurnID      string `json:"turn_id,omitempty"`
	AgentID     string `json:"agent_id,omitempty"`
	InstanceID  string `json:"instance_id,omitempty"`
	ConvID      string `json:"conv_id,omitempty"`
	ProfileType string `json:"profile_type,omitempty"`
	Content     string `json:"content,omitempty"`
	ToolName    string `json:"tool_name,omitempty"`
	Args        string `json:"args,omitempty"`
	Result      string `json:"result,omitempty"`
	IsError     bool   `json:"is_error,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
	Message     string `json:"message,omitempty"`
	Fatal       bool   `json:"fatal,omitempty"`
}

// Bridge subscribes to a conversation stream and forwards events to a channel.
// Call Close to unsubscribe.
type Bridge struct {
	sub *nats.Subscription
	Ch  <-chan StreamEvent
}

// Subscribe creates a Bridge that receives stream events for the given conversation.
// Returns a no-op bridge (nil channel) if conn is nil.
func Subscribe(conn *nats.Conn, convID string) (*Bridge, error) {
	if conn == nil {
		return &Bridge{Ch: make(chan StreamEvent)}, nil
	}
	ch := make(chan StreamEvent, 512)
	subject := ConvStreamSubject(convID)
	sub, err := conn.Subscribe(subject, func(msg *nats.Msg) {
		var evt StreamEvent
		if err := json.Unmarshal(msg.Data, &evt); err != nil {
			slog.Warn("bridge: failed to parse stream event", "subject", subject, "error", err)
			return
		}
		select {
		case ch <- evt:
		default:
			slog.Warn("bridge: channel full, dropping event", "type", evt.Type)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("bridge: subscribe %s: %w", subject, err)
	}
	return &Bridge{sub: sub, Ch: ch}, nil
}

// Close unsubscribes from the NATS subject and drains the channel.
func (b *Bridge) Close() {
	if b.sub != nil {
		_ = b.sub.Unsubscribe()
	}
}
