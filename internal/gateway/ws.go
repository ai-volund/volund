package gateway

// WebSocket handler for real-time agent streaming.
//
// Clients connect to WS /ws/conv/{convId} and receive a stream of events
// forwarded from the agent pod via NATS. The client can also send messages
// which are persisted and dispatched to the agent pool.
//
// Server → Client frames mirror the lightweight NATS stream events:
//
//	{"type":"agent_start", "agent_id":"...", "profile_type":"..."}
//	{"type":"turn_start",  "turn_id":"..."}
//	{"type":"delta",       "turn_id":"...", "content":"..."}
//	{"type":"tool_start",  "turn_id":"...", "tool_name":"...", "args":"..."}
//	{"type":"tool_end",    "turn_id":"...", "tool_name":"...", "result":"...", "is_error":false}
//	{"type":"turn_end",    "turn_id":"...", "stop_reason":"end_turn"}
//	{"type":"agent_end",   "agent_id":"..."}
//	{"type":"error",       "message":"...", "fatal":false}
//
// Client → Server frames (optional mid-run steering):
//
//	{"type":"steer", "content":"Actually, use Python instead"}

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/ai-volund/volund/internal/auth"
	"github.com/ai-volund/volund/internal/db"
	"github.com/ai-volund/volund/internal/dispatch"
)

// wsClientFrame is a message the client can send over WebSocket.
type wsClientFrame struct {
	Type    string `json:"type"`    // "steer" (future: "ping", etc.)
	Content string `json:"content"` // steer content
}

// handleConvStream upgrades the connection for conversation_id from the URL
// path, subscribes to the NATS conv stream, and bridges events to the client.
// Path: GET /ws/conv/{id}
func (s *Server) handleConvStream(w http.ResponseWriter, r *http.Request) {
	convID := r.PathValue("id")
	if convID == "" {
		http.Error(w, "conversation_id required", http.StatusBadRequest)
		return
	}

	// Validate the conversation exists and belongs to the authenticated tenant.
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if s.pool != nil {
		convo, err := db.NewConversationRepo(s.pool).Get(r.Context(), convID)
		if err != nil || convo.TenantID != claims.TenantID {
			http.Error(w, "conversation not found", http.StatusNotFound)
			return
		}
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // CORS handled by middleware; tighten in prod
	})
	if err != nil {
		slog.Error("ws accept", "error", err)
		return
	}
	defer conn.CloseNow() //nolint:errcheck

	ctx := r.Context()

	// Subscribe to the conversation NATS stream.
	bridge, err := dispatch.Subscribe(s.natsConn, convID)
	if err != nil {
		slog.Error("ws: failed to subscribe to conv stream", "conv_id", convID, "error", err)
		_ = wsjson.Write(ctx, conn, dispatch.StreamEvent{Type: "error", Message: "stream unavailable", Fatal: true})
		return
	}
	defer bridge.Close()

	slog.Info("ws: client connected", "conv_id", convID, "tenant_id", claims.TenantID)

	// Pump NATS events to the WebSocket and read client frames in parallel.
	clientFrames := make(chan wsClientFrame, 8)
	go func() {
		defer close(clientFrames)
		for {
			var frame wsClientFrame
			if err := wsjson.Read(ctx, conn, &frame); err != nil {
				if !errors.Is(err, context.Canceled) && websocket.CloseStatus(err) == -1 {
					slog.Debug("ws read", "conv_id", convID, "error", err)
				}
				return
			}
			select {
			case clientFrames <- frame:
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return

		case evt, ok := <-bridge.Ch:
			if !ok {
				return
			}
			if err := wsjson.Write(ctx, conn, evt); err != nil {
				slog.Debug("ws write", "conv_id", convID, "error", err)
				return
			}
			// Close cleanly once the agent signals it's done.
			// Instance release is handled by watchConvStream to avoid double-release.
			if evt.Type == "agent_end" {
				_ = conn.Close(websocket.StatusNormalClosure, "agent done")
				return
			}

		case frame, ok := <-clientFrames:
			if !ok {
				return
			}
			s.handleClientFrame(ctx, frame, convID, claims)
		}
	}
}

// handleClientFrame processes a message sent by the WebSocket client.
func (s *Server) handleClientFrame(ctx context.Context, frame wsClientFrame, convID string, _ *auth.Claims) {
	switch frame.Type {
	case "steer":
		if s.dispatcher == nil || frame.Content == "" {
			return
		}
		// Steering targets the active instance for this conversation.
		// For v1 we use a well-known instance lookup; v2 will use the routing table.
		instanceID := s.activeInstance(convID)
		if instanceID == "" {
			slog.Warn("ws steer: no active instance for conversation", "conv_id", convID)
			return
		}
		if err := s.dispatcher.Steer(ctx, instanceID, frame.Content); err != nil {
			slog.Warn("ws steer: dispatch failed", "conv_id", convID, "error", err)
		}
	default:
		slog.Debug("ws: unknown client frame type", "type", frame.Type)
	}
}

// activeInstance returns the currently active agent instance ID for a conversation.
// Delegates to ClaimManager when available, falls back to the shared routing table.
func (s *Server) activeInstance(convID string) string {
	if s.claimMgr != nil {
		return s.claimMgr.ActiveInstance(convID)
	}
	return s.route.Get(convID)
}

// setActiveInstance records which agent instance is handling a conversation.
func (s *Server) setActiveInstance(convID, instanceID string) {
	if s.claimMgr != nil {
		s.claimMgr.SetActiveInstance(convID, instanceID)
		return
	}
	s.route.Set(convID, instanceID, 0)
}

// clearActiveInstance removes the routing entry when an agent finishes.
func (s *Server) clearActiveInstance(convID string) {
	if s.claimMgr != nil {
		s.claimMgr.ClearActiveInstance(convID)
		return
	}
	s.route.Delete(convID)
}

// watchConvStream watches a conversation's NATS stream to:
//   - Set the routing table when the agent announces its instance_id (agent_start).
//   - Accumulate streamed content and persist assistant messages on turn_end.
//   - Clear the routing table when the agent finishes (agent_end).
//
// Called in a goroutine after task dispatch.
func (s *Server) watchConvStream(ctx context.Context, convID string, taskID string) {
	bridge, err := dispatch.Subscribe(s.natsConn, convID)
	if err != nil {
		return
	}
	defer bridge.Close()

	var content strings.Builder
	var agentName string

	timeout := time.After(30 * time.Minute) // safety valve
	for {
		select {
		case <-ctx.Done():
			return
		case <-timeout:
			s.clearActiveInstance(convID)
			return
		case evt, ok := <-bridge.Ch:
			if !ok {
				return
			}
			switch evt.Type {
			case "agent_start":
				agentName = evt.AgentID
				content.Reset()

				// Resolve pod name → DB instance UUID for routing and task tracking.
				var instanceUUID string
				if evt.InstanceID != "" && s.pool != nil {
					inst, lookupErr := db.NewAgentRepo(s.pool).GetInstanceByPodName(ctx, evt.InstanceID)
					if lookupErr != nil {
						slog.Debug("ws: instance lookup by pod name failed",
							"pod_name", evt.InstanceID, "error", lookupErr)
					} else {
						instanceUUID = inst.ID
					}
				}
				if instanceUUID != "" {
					s.setActiveInstance(convID, instanceUUID)
				}

				// Update task status to running with the resolved instance UUID.
				if taskID != "" && s.pool != nil && instanceUUID != "" {
					if err := db.NewTaskRepo(s.pool).SetRunning(ctx, taskID, instanceUUID); err != nil {
						slog.Warn("task: failed to set running", "task_id", taskID, "error", err)
					}
				}

			case "delta":
				content.WriteString(evt.Content)

			case "turn_end":
				// Persist the accumulated assistant message to the database.
				if s.pool != nil && content.Len() > 0 {
					s.persistAssistantMessage(ctx, convID, agentName, content.String())
					content.Reset()
				}

			case "agent_end":
				// Persist any remaining content (in case turn_end was missed).
				if s.pool != nil && content.Len() > 0 {
					s.persistAssistantMessage(ctx, convID, agentName, content.String())
				}
				// Mark task as completed.
				if taskID != "" && s.pool != nil {
					resultJSON, _ := json.Marshal(map[string]string{"status": "done"})
					if err := db.NewTaskRepo(s.pool).Complete(ctx, taskID, resultJSON); err != nil {
						slog.Warn("task: failed to complete", "task_id", taskID, "error", err)
					}
				}
				// Release the claimed pod back to the warm pool.
				if s.claimMgr != nil {
					if err := s.claimMgr.Release(ctx, convID); err != nil {
						slog.Warn("release instance on agent_end failed", "conv_id", convID, "error", err)
					}
				}
				s.clearActiveInstance(convID)
				return

			case "error":
				if evt.Fatal && taskID != "" && s.pool != nil {
					errMsg := evt.Content
					if errMsg == "" {
						errMsg = evt.Message
					}
					if err := db.NewTaskRepo(s.pool).Fail(ctx, taskID, errMsg); err != nil {
						slog.Warn("task: failed to mark failed", "task_id", taskID, "error", err)
					}
				}
			}
		}
	}
}

// persistAssistantMessage saves the agent's streamed response to the database.
func (s *Server) persistAssistantMessage(ctx context.Context, convID, agentName, text string) {
	contentJSON, _ := json.Marshal([]map[string]string{
		{"type": "text", "text": text},
	})

	var name *string
	if agentName != "" {
		name = &agentName
	}

	repo := db.NewConversationRepo(s.pool)
	if _, err := repo.AddMessage(ctx, &db.Message{
		ConversationID: convID,
		Role:           "assistant",
		AuthorType:     "agent",
		AgentName:      name,
		Content:        contentJSON,
	}); err != nil {
		slog.Error("persist assistant message failed", "conv_id", convID, "error", err)
	} else {
		slog.Info("assistant message persisted", "conv_id", convID, "length", len(text))
	}
}

// legacyHandleChat is kept for backwards compatibility with the old direct-LLM
// WebSocket path. Deprecated: use /ws/conv/{id} instead.
func (s *Server) legacyHandleChat(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "deprecated: use /ws/conv/{id}", http.StatusGone)
}

