// Package anthropic provides an LLM provider backed by the Anthropic API.
// It supports Chat (Messages API) with streaming and tool use.
// Anthropic does not offer an embeddings API; Embed returns ErrNotSupported.
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	ant "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	antparam "github.com/anthropics/anthropic-sdk-go/packages/param"

	"github.com/ai-volund/volund/internal/llm"
)

// ErrEmbedNotSupported is returned when Embed is called on the Anthropic provider.
var ErrEmbedNotSupported = errors.New("anthropic does not support embeddings")

// Provider implements llm.Provider for Anthropic's Messages API.
type Provider struct {
	client ant.Client
}

// New creates an Anthropic Provider. baseURL is optional and overrides the
// default API endpoint.
func New(apiKey, baseURL string) *Provider {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return &Provider{client: ant.NewClient(opts...)}
}

func (p *Provider) Name() string { return ProviderName }

func (p *Provider) HealthCheck(ctx context.Context) error {
	_, err := p.client.Models.List(ctx, ant.ModelListParams{})
	return err
}

func (p *Provider) ListModels(_ context.Context) ([]*llm.ModelInfo, error) {
	return knownModels, nil
}

// ── Chat ──────────────────────────────────────────────────────────────────────

func (p *Provider) Chat(ctx context.Context, req *llm.ProviderRequest) (*llm.ProviderResponse, error) {
	params, err := toMessageParams(req)
	if err != nil {
		return nil, err
	}
	msg, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic chat: %w", err)
	}
	return fromMessage(msg), nil
}

func (p *Provider) StreamChat(ctx context.Context, req *llm.ProviderRequest) (llm.Stream, error) {
	params, err := toMessageParams(req)
	if err != nil {
		return nil, err
	}
	s := p.client.Messages.NewStreaming(ctx, params)
	return &chatStream{inner: s}, nil
}

// ── Embeddings ────────────────────────────────────────────────────────────────

func (p *Provider) Embed(_ context.Context, _ string, _ []string) ([][]float32, *llm.Usage, error) {
	return nil, nil, ErrEmbedNotSupported
}

// ── chatStream ────────────────────────────────────────────────────────────────

type chatStream struct {
	inner interface {
		Next() bool
		Current() ant.MessageStreamEventUnion
		Err() error
		Close() error
	}
	// Accumulate tool input JSON fragments keyed by block index.
	toolInputs map[int64]string
	toolMeta   map[int64]ant.ContentBlockStartEventContentBlockUnion
	done       bool
}

func (s *chatStream) Recv() (*llm.Chunk, error) {
	if s.done {
		return nil, io.EOF
	}
	if s.toolInputs == nil {
		s.toolInputs = make(map[int64]string)
		s.toolMeta = make(map[int64]ant.ContentBlockStartEventContentBlockUnion)
	}

	for {
		if !s.inner.Next() {
			s.done = true
			if err := s.inner.Err(); err != nil {
				return nil, fmt.Errorf("anthropic stream: %w", err)
			}
			return nil, io.EOF
		}

		evt := s.inner.Current()
		switch evt.Type {
		case "content_block_start":
			cb := evt.AsContentBlockStart()
			s.toolMeta[cb.Index] = cb.ContentBlock

		case "content_block_delta":
			delta := evt.AsContentBlockDelta()
			raw := delta.Delta
			switch raw.Type {
			case "text_delta":
				text := raw.AsTextDelta().Text
				if text != "" {
					return &llm.Chunk{TextDelta: text}, nil
				}
			case "input_json_delta":
				s.toolInputs[delta.Index] += raw.AsInputJSONDelta().PartialJSON
			}

		case "content_block_stop":
			stop := evt.AsContentBlockStop()
			if inputJSON, ok := s.toolInputs[stop.Index]; ok {
				meta := s.toolMeta[stop.Index]
				tu := meta.AsToolUse()
				delete(s.toolInputs, stop.Index)
				delete(s.toolMeta, stop.Index)
				return &llm.Chunk{
					ToolUse: &llm.ToolUseData{
						ID:        tu.ID,
						Name:      tu.Name,
						InputJSON: inputJSON,
					},
				}, nil
			}

		case "message_delta":
			md := evt.AsMessageDelta()
			if md.Usage.OutputTokens > 0 || md.Delta.StopReason != "" {
				// Emit final chunk on message_delta so the caller gets usage info.
				final := &llm.ProviderResponse{
					StopReason: stopReasonToInternal(string(md.Delta.StopReason)),
					Usage: llm.Usage{
						OutputTokens: int32(md.Usage.OutputTokens),
					},
				}
				return &llm.Chunk{Final: final}, nil
			}

		case "message_start":
			ms := evt.AsMessageStart()
			_ = ms // usage will come in message_delta
		}
		// Skip other event types.
	}
}

func (s *chatStream) Close() error {
	return s.inner.Close()
}

// ── Conversion helpers ────────────────────────────────────────────────────────

func toMessageParams(req *llm.ProviderRequest) (ant.MessageNewParams, error) {
	// Separate system messages from the conversation.
	var systemBlocks []ant.TextBlockParam
	var msgs []ant.MessageParam

	for _, m := range req.Messages {
		if m.Role == "system" {
			systemBlocks = append(systemBlocks, ant.TextBlockParam{
				Text: textContent(m.Content),
			})
			continue
		}
		ap, err := toMessageParam(m)
		if err != nil {
			return ant.MessageNewParams{}, err
		}
		msgs = append(msgs, ap)
	}

	p := ant.MessageNewParams{
		Model:    ant.Model(req.Model),
		Messages: msgs,
		MaxTokens: func() int64 {
			if req.MaxTokens > 0 {
				return int64(req.MaxTokens)
			}
			return 8096 // sensible default
		}(),
	}

	if len(systemBlocks) > 0 {
		p.System = systemBlocks
	}
	if req.Temperature > 0 {
		p.Temperature = antparam.NewOpt(req.Temperature)
	}
	if len(req.StopSequences) > 0 {
		p.StopSequences = req.StopSequences
	}
	if len(req.Tools) > 0 {
		tools, err := toToolParams(req.Tools)
		if err != nil {
			return ant.MessageNewParams{}, err
		}
		p.Tools = tools
	}
	return p, nil
}

func toMessageParam(m *llm.Message) (ant.MessageParam, error) {
	var blocks []ant.ContentBlockParamUnion
	for _, b := range m.Content {
		switch b.Type {
		case "text":
			blocks = append(blocks, ant.NewTextBlock(b.Text))
		case "image":
			if b.Image != nil {
				if b.Image.URL != "" {
					blocks = append(blocks, ant.NewImageBlock(ant.URLImageSourceParam{
						URL: b.Image.URL,
					}))
				} else if len(b.Image.Data) > 0 {
					blocks = append(blocks, ant.NewImageBlockBase64(b.Image.MediaType,
						string(b.Image.Data)))
				}
			}
		case "tool_use":
			if b.ToolUse != nil {
				var input any
				if b.ToolUse.InputJSON != "" {
					if err := json.Unmarshal([]byte(b.ToolUse.InputJSON), &input); err != nil {
						input = b.ToolUse.InputJSON
					}
				}
				blocks = append(blocks, ant.NewToolUseBlock(b.ToolUse.ID, input, b.ToolUse.Name))
			}
		case "tool_result":
			if b.ToolResult != nil {
				blocks = append(blocks, ant.NewToolResultBlock(
					b.ToolResult.ToolUseID,
					b.ToolResult.Content,
					b.ToolResult.IsError,
				))
			}
		}
	}

	switch m.Role {
	case "user":
		return ant.NewUserMessage(blocks...), nil
	case "assistant":
		return ant.NewAssistantMessage(blocks...), nil
	default:
		return ant.MessageParam{}, fmt.Errorf("unsupported role %q (use system separately)", m.Role)
	}
}

func toToolParams(tools []*llm.ToolDef) ([]ant.ToolUnionParam, error) {
	out := make([]ant.ToolUnionParam, len(tools))
	for i, t := range tools {
		tp := ant.ToolParam{
			Name:        t.Name,
			InputSchema: ant.ToolInputSchemaParam{},
		}
		if t.Description != "" {
			tp.Description = antparam.NewOpt(t.Description)
		}
		if t.InputSchemaJSON != "" {
			var schema ant.ToolInputSchemaParam
			if err := json.Unmarshal([]byte(t.InputSchemaJSON), &schema); err != nil {
				return nil, fmt.Errorf("invalid tool schema for %q: %w", t.Name, err)
			}
			tp.InputSchema = schema
		}
		out[i] = ant.ToolUnionParam{OfTool: &tp}
	}
	return out, nil
}

func fromMessage(msg *ant.Message) *llm.ProviderResponse {
	if msg == nil {
		return &llm.ProviderResponse{}
	}
	var content []*llm.Block
	for _, cb := range msg.Content {
		switch cb.Type {
		case "text":
			content = append(content, &llm.Block{Type: "text", Text: cb.AsText().Text})
		case "tool_use":
			tu := cb.AsToolUse()
			content = append(content, &llm.Block{
				Type: "tool_use",
				ToolUse: &llm.ToolUseData{
					ID:        tu.ID,
					Name:      tu.Name,
					InputJSON: string(tu.Input),
				},
			})
		}
	}
	return &llm.ProviderResponse{
		ID:         msg.ID,
		Model:      string(msg.Model),
		Content:    content,
		StopReason: stopReasonToInternal(string(msg.StopReason)),
		Usage: llm.Usage{
			InputTokens:      int32(msg.Usage.InputTokens),
			OutputTokens:     int32(msg.Usage.OutputTokens),
			CacheReadTokens:  int32(msg.Usage.CacheReadInputTokens),
			CacheWriteTokens: int32(msg.Usage.CacheCreationInputTokens),
		},
	}
}

func stopReasonToInternal(sr string) string {
	switch sr {
	case "tool_use":
		return "tool_use"
	case "max_tokens":
		return "max_tokens"
	default:
		return "end_turn"
	}
}

func textContent(blocks []*llm.Block) string {
	for _, b := range blocks {
		if b.Type == "text" {
			return b.Text
		}
	}
	return ""
}
