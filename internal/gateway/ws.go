package gateway

// WebSocket handler for real-time LLM streaming.
//
// Clients connect to WS /ws/chat and send JSON frames; the server streams
// LLM tokens back as they arrive.
//
// Client → Server frame:
//
//	{
//	  "conversation_id": "...",   // optional, empty = new conversation
//	  "message": "User text",
//	  "provider": "openai",       // optional, router picks default
//	  "model":    "gpt-4o",       // optional
//	  "temperature": 0.7,         // optional
//	  "max_tokens": 4096          // optional
//	}
//
// Server → Client frames (one per token / event):
//
//	{"type":"delta",    "text":"..."}           – token delta
//	{"type":"tool_use", "id":"...", "name":"...", "input":"..."} – tool call
//	{"type":"done",     "stop_reason":"end_turn", "usage":{...}} – stream end
//	{"type":"error",    "message":"..."}        – error

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/ai-volund/volund/internal/llm"
)

// wsIncoming is the frame the client sends.
type wsIncoming struct {
	ConversationID string  `json:"conversation_id"`
	Message        string  `json:"message"`
	Provider       string  `json:"provider"`
	Model          string  `json:"model"`
	Temperature    float64 `json:"temperature"`
	MaxTokens      int32   `json:"max_tokens"`
}

// wsOutgoing is a frame the server sends.
type wsOutgoing struct {
	Type       string     `json:"type"`
	Text       string     `json:"text,omitempty"`
	ID         string     `json:"id,omitempty"`
	Name       string     `json:"name,omitempty"`
	Input      string     `json:"input,omitempty"`
	StopReason string     `json:"stop_reason,omitempty"`
	Usage      *wsUsage   `json:"usage,omitempty"`
	Message    string     `json:"message,omitempty"` // error message
}

type wsUsage struct {
	InputTokens  int32 `json:"input_tokens"`
	OutputTokens int32 `json:"output_tokens"`
}

// handleChat upgrades the connection and streams LLM tokens back.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // CORS handled by middleware; tighten in prod
	})
	if err != nil {
		slog.Error("ws accept", "error", err)
		return
	}
	defer conn.CloseNow() //nolint:errcheck

	ctx := conn.CloseRead(r.Context())

	for {
		var in wsIncoming
		if err := wsjson.Read(ctx, conn, &in); err != nil {
			if websocket.CloseStatus(err) != -1 {
				return // clean close
			}
			slog.Debug("ws read", "error", err)
			return
		}

		if err := s.streamResponse(ctx, conn, &in); err != nil {
			slog.Error("ws stream", "error", err)
			_ = wsjson.Write(ctx, conn, wsOutgoing{Type: "error", Message: err.Error()})
			return
		}
	}
}

func (s *Server) streamResponse(ctx context.Context, conn *websocket.Conn, in *wsIncoming) error {
	model := in.Model
	if model == "" {
		model = "gpt-4o" // fallback; router will use its default provider
	}

	req := &llm.ProviderRequest{
		Model:       model,
		Temperature: in.Temperature,
		MaxTokens:   in.MaxTokens,
		Messages: []*llm.Message{
			{
				Role: "user",
				Content: []*llm.Block{
					{Type: "text", Text: in.Message},
				},
			},
		},
	}

	stream, err := s.router.StreamChat(ctx, in.Provider, req)
	if err != nil {
		return err
	}
	defer stream.Close() //nolint:errcheck

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		var frame wsOutgoing
		switch {
		case chunk.Final != nil:
			frame = wsOutgoing{
				Type:       "done",
				StopReason: chunk.Final.StopReason,
				Usage: &wsUsage{
					InputTokens:  chunk.Final.Usage.InputTokens,
					OutputTokens: chunk.Final.Usage.OutputTokens,
				},
			}
		case chunk.ToolUse != nil:
			frame = wsOutgoing{
				Type:  "tool_use",
				ID:    chunk.ToolUse.ID,
				Name:  chunk.ToolUse.Name,
				Input: chunk.ToolUse.InputJSON,
			}
		default:
			if chunk.TextDelta == "" {
				continue
			}
			frame = wsOutgoing{Type: "delta", Text: chunk.TextDelta}
		}

		if err := wsjson.Write(ctx, conn, frame); err != nil {
			return err
		}
	}
	return nil
}

