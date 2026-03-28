// Package openai provides an LLM provider implementation backed by the OpenAI
// API. It supports Chat Completions (stateless) and the Responses API
// (stateful, with previous_response_id chaining).
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	ogai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"

	"github.com/ai-volund/volund/internal/llm"
)

// Provider implements llm.Provider and llm.ResponderProvider for OpenAI.
type Provider struct {
	client ogai.Client
}

// New creates a Provider. If baseURL is empty the default OpenAI API endpoint
// is used, which makes this compatible with any OpenAI-compatible backend
// (Azure, local models via LM Studio, etc.).
func New(apiKey, baseURL string) *Provider {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return &Provider{client: ogai.NewClient(opts...)}
}

func (p *Provider) Name() string { return ProviderName }

func (p *Provider) HealthCheck(ctx context.Context) error {
	_, err := p.client.Models.List(ctx)
	return err
}

func (p *Provider) ListModels(_ context.Context) ([]*llm.ModelInfo, error) {
	return knownModels, nil
}

// ── Chat Completions ──────────────────────────────────────────────────────────

func (p *Provider) Chat(ctx context.Context, req *llm.ProviderRequest) (*llm.ProviderResponse, error) {
	params, err := toChatParams(req)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("openai chat: %w", err)
	}
	return fromChatCompletion(resp), nil
}

func (p *Provider) StreamChat(ctx context.Context, req *llm.ProviderRequest) (llm.Stream, error) {
	params, err := toChatParams(req)
	if err != nil {
		return nil, err
	}
	s := p.client.Chat.Completions.NewStreaming(ctx, params)
	return &chatStream{inner: s}, nil
}

// chatStream wraps the SDK's SSE stream in the llm.Stream interface.
type chatStream struct {
	inner interface {
		Next() bool
		Current() ogai.ChatCompletionChunk
		Err() error
		Close() error
	}
	acc     ogai.ChatCompletionAccumulator
	done    bool
	lastErr error
}

func (s *chatStream) Recv() (*llm.Chunk, error) {
	if s.done {
		return nil, io.EOF
	}
	for {
		if !s.inner.Next() {
			s.done = true
			if err := s.inner.Err(); err != nil {
				return nil, fmt.Errorf("openai stream: %w", err)
			}
			// Emit the final chunk with the fully assembled response.
			final := fromChatCompletion(&s.acc.ChatCompletion)
			return &llm.Chunk{Final: final}, nil
		}
		chunk := s.inner.Current()
		s.acc.AddChunk(chunk)

		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta

		// Emit a tool call as soon as it is fully assembled.
		if tc, ok := s.acc.JustFinishedToolCall(); ok {
			return &llm.Chunk{
				ToolUse: &llm.ToolUseData{
					ID:        tc.ID,
					Name:      tc.Name,
					InputJSON: tc.Arguments,
				},
			}, nil
		}

		if delta.Content != "" {
			return &llm.Chunk{TextDelta: delta.Content}, nil
		}
		// Skip empty delta frames.
	}
}

func (s *chatStream) Close() error {
	return s.inner.Close()
}

// ── Responses API ─────────────────────────────────────────────────────────────

func (p *Provider) Respond(ctx context.Context, req *llm.ResponseRequest) (*llm.ResponseResult, error) {
	params, err := toResponseParams(req)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("openai responses: %w", err)
	}
	return fromResponse(resp), nil
}

func (p *Provider) StreamRespond(ctx context.Context, req *llm.ResponseRequest) (llm.ResponseStream, error) {
	params, err := toResponseParams(req)
	if err != nil {
		return nil, err
	}
	s := p.client.Responses.NewStreaming(ctx, params)
	return &responseStream{inner: s}, nil
}

// responseStream wraps the Responses API SSE stream.
type responseStream struct {
	inner interface {
		Next() bool
		Current() responses.ResponseStreamEventUnion
		Err() error
		Close() error
	}
	done bool
}

func (s *responseStream) Recv() (*llm.ResponseEvent, error) {
	if s.done {
		return nil, io.EOF
	}
	for {
		if !s.inner.Next() {
			s.done = true
			if err := s.inner.Err(); err != nil {
				return nil, fmt.Errorf("openai response stream: %w", err)
			}
			return nil, io.EOF
		}
		evt := s.inner.Current()
		switch evt.Type {
		case "response.output_text.delta":
			d := evt.AsResponseOutputTextDelta()
			if d.Delta != "" {
				return &llm.ResponseEvent{
					Type:      "response.output_text.delta",
					TextDelta: d.Delta,
				}, nil
			}
		case "response.completed":
			c := evt.AsResponseCompleted()
			return &llm.ResponseEvent{
				Type:     "response.completed",
				Response: fromResponse(&c.Response),
			}, nil
		}
		// Skip other event types (created, rate_limits, etc.)
	}
}

func (s *responseStream) Close() error {
	return s.inner.Close()
}

// ── Embeddings ────────────────────────────────────────────────────────────────

func (p *Provider) Embed(ctx context.Context, model string, texts []string) ([][]float32, *llm.Usage, error) {
	resp, err := p.client.Embeddings.New(ctx, ogai.EmbeddingNewParams{
		Model: model,
		Input: ogai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: texts},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("openai embed: %w", err)
	}
	vecs := make([][]float32, len(resp.Data))
	for i, e := range resp.Data {
		v := make([]float32, len(e.Embedding))
		for j, f := range e.Embedding {
			v[j] = float32(f)
		}
		vecs[i] = v
	}
	usage := &llm.Usage{InputTokens: int32(resp.Usage.PromptTokens)}
	return vecs, usage, nil
}

// ── Conversion helpers ────────────────────────────────────────────────────────

func toChatParams(req *llm.ProviderRequest) (ogai.ChatCompletionNewParams, error) {
	msgs, err := toMessages(req.Messages)
	if err != nil {
		return ogai.ChatCompletionNewParams{}, err
	}
	p := ogai.ChatCompletionNewParams{
		Model:    req.Model,
		Messages: msgs,
	}
	if req.Temperature > 0 {
		p.Temperature = param.NewOpt(req.Temperature)
	}
	if req.MaxTokens > 0 {
		p.MaxCompletionTokens = param.NewOpt(int64(req.MaxTokens))
	}
	if len(req.Tools) > 0 {
		tools := make([]ogai.ChatCompletionToolParam, len(req.Tools))
		for i, t := range req.Tools {
			var params shared.FunctionParameters
			if t.InputSchemaJSON != "" {
				if err := json.Unmarshal([]byte(t.InputSchemaJSON), &params); err != nil {
					return ogai.ChatCompletionNewParams{}, fmt.Errorf("invalid tool schema for %q: %w", t.Name, err)
				}
			}
			tools[i] = ogai.ChatCompletionToolParam{
				Function: shared.FunctionDefinitionParam{
					Name:        t.Name,
					Description: param.NewOpt(t.Description),
					Parameters:  params,
				},
			}
		}
		p.Tools = tools
	}
	return p, nil
}

func toMessages(msgs []*llm.Message) ([]ogai.ChatCompletionMessageParamUnion, error) {
	out := make([]ogai.ChatCompletionMessageParamUnion, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "system":
			out = append(out, ogai.SystemMessage(textContent(m.Content)))
		case "user":
			out = append(out, ogai.UserMessage(textContent(m.Content)))
		case "tool":
			out = append(out, ogai.ToolMessage(textContent(m.Content), m.ToolCallID))
		case "assistant":
			toolCalls := extractToolCalls(m.Content)
			if len(toolCalls) > 0 {
				ap := ogai.ChatCompletionAssistantMessageParam{
					ToolCalls: toolCalls,
				}
				if t := textContent(m.Content); t != "" {
					ap.Content = ogai.ChatCompletionAssistantMessageParamContentUnion{
						OfString: param.NewOpt(t),
					}
				}
				out = append(out, ogai.ChatCompletionMessageParamUnion{OfAssistant: &ap})
			} else {
				out = append(out, ogai.AssistantMessage(textContent(m.Content)))
			}
		default:
			return nil, fmt.Errorf("unknown message role %q", m.Role)
		}
	}
	return out, nil
}

func textContent(blocks []*llm.Block) string {
	for _, b := range blocks {
		if b.Type == "text" {
			return b.Text
		}
	}
	return ""
}

func extractToolCalls(blocks []*llm.Block) []ogai.ChatCompletionMessageToolCallParam {
	var calls []ogai.ChatCompletionMessageToolCallParam
	for _, b := range blocks {
		if b.Type == "tool_use" && b.ToolUse != nil {
			calls = append(calls, ogai.ChatCompletionMessageToolCallParam{
				ID:   b.ToolUse.ID,
				Type: "function",
				Function: ogai.ChatCompletionMessageToolCallFunctionParam{
					Name:      b.ToolUse.Name,
					Arguments: b.ToolUse.InputJSON,
				},
			})
		}
	}
	return calls
}

func fromChatCompletion(resp *ogai.ChatCompletion) *llm.ProviderResponse {
	if resp == nil || len(resp.Choices) == 0 {
		return &llm.ProviderResponse{}
	}
	msg := resp.Choices[0].Message
	var content []*llm.Block

	if msg.Content != "" {
		content = append(content, &llm.Block{Type: "text", Text: msg.Content})
	}
	for _, tc := range msg.ToolCalls {
		content = append(content, &llm.Block{
			Type: "tool_use",
			ToolUse: &llm.ToolUseData{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				InputJSON: tc.Function.Arguments,
			},
		})
	}

	stopReason := finishReasonToStop(resp.Choices[0].FinishReason)
	return &llm.ProviderResponse{
		ID:         resp.ID,
		Model:      resp.Model,
		Content:    content,
		StopReason: stopReason,
		Usage: llm.Usage{
			InputTokens:  int32(resp.Usage.PromptTokens),
			OutputTokens: int32(resp.Usage.CompletionTokens),
		},
	}
}

func finishReasonToStop(fr string) string {
	switch fr {
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return "end_turn"
	}
}

func toResponseParams(req *llm.ResponseRequest) (responses.ResponseNewParams, error) {
	p := responses.ResponseNewParams{
		Model: shared.ResponsesModel(req.Model),
	}
	switch v := req.Input.(type) {
	case string:
		p.Input = responses.ResponseNewParamsInputUnion{
			OfString: param.NewOpt(v),
		}
	case []*llm.InputItem:
		// Convert to Responses API input item list.
		// For simplicity, pass as a string summary for non-string inputs.
		if len(v) == 1 {
			if s, ok := v[0].Content.(string); ok {
				p.Input = responses.ResponseNewParamsInputUnion{
					OfString: param.NewOpt(s),
				}
			}
		}
	}
	if req.PreviousResponseID != "" {
		p.PreviousResponseID = param.NewOpt(req.PreviousResponseID)
	}
	if req.Instructions != "" {
		p.Instructions = param.NewOpt(req.Instructions)
	}
	if req.MaxTokens > 0 {
		p.MaxOutputTokens = param.NewOpt(int64(req.MaxTokens))
	}
	return p, nil
}

func fromResponse(resp *responses.Response) *llm.ResponseResult {
	if resp == nil {
		return &llm.ResponseResult{}
	}
	var output []*llm.OutputItem
	for _, item := range resp.Output {
		msg := item.AsMessage()
		var content []*llm.Block
		for _, part := range msg.Content {
			txt := part.AsOutputText()
			if txt.Text != "" {
				content = append(content, &llm.Block{Type: "text", Text: txt.Text})
			}
		}
		if len(content) > 0 {
			output = append(output, &llm.OutputItem{
				Type:    "message",
				Role:    string(msg.Role),
				Content: content,
			})
		}
	}
	return &llm.ResponseResult{
		ID:     resp.ID,
		Model:  string(resp.Model),
		Output: output,
		Usage: llm.Usage{
			InputTokens:  int32(resp.Usage.InputTokens),
			OutputTokens: int32(resp.Usage.OutputTokens),
		},
		Status: string(resp.Status),
	}
}
