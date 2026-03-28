// Package ollama implements the LLM Provider interface for local Ollama models.
// Ollama exposes an OpenAI-compatible API at /v1/, so this provider uses the
// openai-go SDK pointed at the Ollama base URL.
package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ai-volund/volund/internal/llm"
	ogai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/shared"
)

// Provider connects to a local Ollama instance via its OpenAI-compatible API.
type Provider struct {
	client  ogai.Client
	baseURL string
}

// New creates an Ollama provider. baseURL is the Ollama server URL
// (e.g., "http://localhost:11434").
func New(baseURL string) *Provider {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	apiURL := strings.TrimRight(baseURL, "/") + "/v1/"

	client := ogai.NewClient(
		option.WithBaseURL(apiURL),
		option.WithAPIKey("ollama"), // Ollama doesn't require a real key
	)

	return &Provider{client: client, baseURL: baseURL}
}

func (p *Provider) Name() string { return "ollama" }

func (p *Provider) Chat(ctx context.Context, req *llm.ProviderRequest) (*llm.ProviderResponse, error) {
	params, err := p.buildParams(req)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("ollama chat: %w", err)
	}
	return p.parseResponse(*resp), nil
}

func (p *Provider) StreamChat(ctx context.Context, req *llm.ProviderRequest) (llm.Stream, error) {
	params, err := p.buildParams(req)
	if err != nil {
		return nil, err
	}
	s := p.client.Chat.Completions.NewStreaming(ctx, params)
	return &ollamaStream{inner: s}, nil
}

func (p *Provider) Embed(ctx context.Context, model string, texts []string) ([][]float32, *llm.Usage, error) {
	if model == "" {
		model = "nomic-embed-text"
	}
	resp, err := p.client.Embeddings.New(ctx, ogai.EmbeddingNewParams{
		Model: model,
		Input: ogai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: texts},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("ollama embed: %w", err)
	}

	vecs := make([][]float32, len(resp.Data))
	for i, e := range resp.Data {
		v := make([]float32, len(e.Embedding))
		for j, f := range e.Embedding {
			v[j] = float32(f)
		}
		vecs[i] = v
	}
	return vecs, &llm.Usage{InputTokens: int32(resp.Usage.PromptTokens)}, nil
}

func (p *Provider) ListModels(ctx context.Context) ([]*llm.ModelInfo, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(p.baseURL + "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("ollama list models: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name    string `json:"name"`
			Details struct {
				ParameterSize string `json:"parameter_size"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode models: %w", err)
	}

	models := make([]*llm.ModelInfo, len(result.Models))
	for i, m := range result.Models {
		models[i] = &llm.ModelInfo{
			Provider:         "ollama",
			ID:               m.Name,
			DisplayName:      m.Name,
			ContextWindow:    8192,
			MaxOutputTokens:  4096,
			SupportsVision:   strings.Contains(m.Name, "llava") || strings.Contains(m.Name, "vision"),
			SupportsToolUse:  true,
			SupportsStreaming: true,
		}
	}
	return models, nil
}

func (p *Provider) HealthCheck(ctx context.Context) error {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(p.baseURL + "/api/tags")
	if err != nil {
		return fmt.Errorf("ollama health: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama health: HTTP %d", resp.StatusCode)
	}
	return nil
}

// --- Internal ---

func (p *Provider) buildParams(req *llm.ProviderRequest) (ogai.ChatCompletionNewParams, error) {
	msgs := toMessages(req.Messages)
	params := ogai.ChatCompletionNewParams{
		Model:    req.Model,
		Messages: msgs,
	}
	if req.MaxTokens > 0 {
		params.MaxCompletionTokens = param.NewOpt(int64(req.MaxTokens))
	}
	if req.Temperature > 0 {
		params.Temperature = param.NewOpt(req.Temperature)
	}
	if len(req.Tools) > 0 {
		tools := make([]ogai.ChatCompletionToolParam, len(req.Tools))
		for i, t := range req.Tools {
			var fp shared.FunctionParameters
			if t.InputSchemaJSON != "" {
				if err := json.Unmarshal([]byte(t.InputSchemaJSON), &fp); err != nil {
					return ogai.ChatCompletionNewParams{}, fmt.Errorf("invalid tool schema for %q: %w", t.Name, err)
				}
			}
			tools[i] = ogai.ChatCompletionToolParam{
				Function: shared.FunctionDefinitionParam{
					Name:        t.Name,
					Description: param.NewOpt(t.Description),
					Parameters:  fp,
				},
			}
		}
		params.Tools = tools
	}
	return params, nil
}

func toMessages(msgs []*llm.Message) []ogai.ChatCompletionMessageParamUnion {
	var out []ogai.ChatCompletionMessageParamUnion
	for _, m := range msgs {
		text := textContent(m.Content)
		switch m.Role {
		case "system":
			out = append(out, ogai.SystemMessage(text))
		case "user":
			out = append(out, ogai.UserMessage(text))
		case "assistant":
			var toolCalls []ogai.ChatCompletionMessageToolCallParam
			for _, b := range m.Content {
				if b.Type == "tool_use" && b.ToolUse != nil {
					toolCalls = append(toolCalls, ogai.ChatCompletionMessageToolCallParam{
						ID:   b.ToolUse.ID,
						Type: "function",
						Function: ogai.ChatCompletionMessageToolCallFunctionParam{
							Name:      b.ToolUse.Name,
							Arguments: b.ToolUse.InputJSON,
						},
					})
				}
			}
			if len(toolCalls) > 0 {
				out = append(out, ogai.ChatCompletionMessageParamUnion{
					OfAssistant: &ogai.ChatCompletionAssistantMessageParam{
						Content:   ogai.ChatCompletionAssistantMessageParamContentUnion{OfString: ogai.String(text)},
						ToolCalls: toolCalls,
					},
				})
			} else {
				out = append(out, ogai.AssistantMessage(text))
			}
		case "tool":
			for _, b := range m.Content {
				if b.Type == "tool_result" && b.ToolResult != nil {
					out = append(out, ogai.ToolMessage(b.ToolResult.Content, b.ToolResult.ToolUseID))
				}
			}
			if len(m.Content) == 0 || m.Content[0].Type != "tool_result" {
				out = append(out, ogai.ToolMessage(text, m.ToolCallID))
			}
		}
	}
	return out
}

func textContent(blocks []*llm.Block) string {
	for _, b := range blocks {
		if b.Type == "text" {
			return b.Text
		}
	}
	return ""
}

func (p *Provider) parseResponse(resp ogai.ChatCompletion) *llm.ProviderResponse {
	pr := &llm.ProviderResponse{
		ID:    resp.ID,
		Model: resp.Model,
		Usage: llm.Usage{
			InputTokens:  int32(resp.Usage.PromptTokens),
			OutputTokens: int32(resp.Usage.CompletionTokens),
		},
	}
	if len(resp.Choices) == 0 {
		return pr
	}
	choice := resp.Choices[0]

	pr.StopReason = string(choice.FinishReason)
	if pr.StopReason == "stop" {
		pr.StopReason = "end_turn"
	}

	if choice.Message.Content != "" {
		pr.Content = append(pr.Content, &llm.Block{Type: "text", Text: choice.Message.Content})
	}
	for _, tc := range choice.Message.ToolCalls {
		pr.Content = append(pr.Content, &llm.Block{
			Type: "tool_use",
			ToolUse: &llm.ToolUseData{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				InputJSON: tc.Function.Arguments,
			},
		})
		pr.StopReason = "tool_use"
	}
	return pr
}

// --- Streaming ---

type ollamaStream struct {
	inner interface {
		Next() bool
		Current() ogai.ChatCompletionChunk
		Err() error
		Close() error
	}
	toolCalls map[int]*llm.ToolUseData
}

func (s *ollamaStream) Recv() (*llm.Chunk, error) {
	if !s.inner.Next() {
		if err := s.inner.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}

	chunk := s.inner.Current()
	out := &llm.Chunk{}

	if len(chunk.Choices) > 0 {
		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			out.TextDelta = delta.Content
		}
		for _, tc := range delta.ToolCalls {
			if s.toolCalls == nil {
				s.toolCalls = make(map[int]*llm.ToolUseData)
			}
			idx := int(tc.Index)
			if _, ok := s.toolCalls[idx]; !ok {
				s.toolCalls[idx] = &llm.ToolUseData{ID: tc.ID, Name: tc.Function.Name}
			}
			s.toolCalls[idx].InputJSON += tc.Function.Arguments
		}

		if chunk.Choices[0].FinishReason == "tool_calls" || chunk.Choices[0].FinishReason == "stop" {
			if len(s.toolCalls) > 0 {
				for _, tc := range s.toolCalls {
					out.ToolUse = tc
					break
				}
			}
		}
	}

	return out, nil
}

func (s *ollamaStream) Close() error {
	return s.inner.Close()
}
