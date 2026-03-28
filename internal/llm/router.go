package llm

import (
	"context"
	"fmt"
	"io"
	"sync"
)

// Router manages LLM providers and routes requests to the right one.
type Router struct {
	mu              sync.RWMutex
	providers       map[string]Provider
	defaultProvider string
}

// NewRouter creates a new Router with no providers registered.
func NewRouter() *Router {
	return &Router{
		providers: make(map[string]Provider),
	}
}

// Register adds a provider. The first registered provider becomes the default.
func (r *Router) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
	if r.defaultProvider == "" {
		r.defaultProvider = p.Name()
	}
}

func (r *Router) get(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if name == "" {
		name = r.defaultProvider
	}
	if name == "" {
		return nil, fmt.Errorf("no LLM providers configured")
	}
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown LLM provider %q", name)
	}
	return p, nil
}

// Chat routes a non-streaming chat request to the appropriate provider.
func (r *Router) Chat(ctx context.Context, providerName string, req *ProviderRequest) (*ProviderResponse, error) {
	p, err := r.get(providerName)
	if err != nil {
		return nil, err
	}
	return p.Chat(ctx, req)
}

// StreamChat routes a streaming chat request to the appropriate provider.
func (r *Router) StreamChat(ctx context.Context, providerName string, req *ProviderRequest) (Stream, error) {
	p, err := r.get(providerName)
	if err != nil {
		return nil, err
	}
	return p.StreamChat(ctx, req)
}

// Embed routes an embedding request to the appropriate provider.
func (r *Router) Embed(ctx context.Context, providerName, model string, texts []string) ([][]float32, *Usage, error) {
	p, err := r.get(providerName)
	if err != nil {
		return nil, nil, err
	}
	return p.Embed(ctx, model, texts)
}

// Respond routes a Responses API request. Providers that implement
// ResponderProvider handle it natively; others fall back to Chat.
func (r *Router) Respond(ctx context.Context, providerName string, req *ResponseRequest) (*ResponseResult, error) {
	p, err := r.get(providerName)
	if err != nil {
		return nil, err
	}
	if rp, ok := p.(ResponderProvider); ok {
		return rp.Respond(ctx, req)
	}
	resp, err := p.Chat(ctx, responseToChat(req))
	if err != nil {
		return nil, err
	}
	return chatToResponseResult(resp), nil
}

// StreamRespond routes a streaming Responses API request.
func (r *Router) StreamRespond(ctx context.Context, providerName string, req *ResponseRequest) (ResponseStream, error) {
	p, err := r.get(providerName)
	if err != nil {
		return nil, err
	}
	if rp, ok := p.(ResponderProvider); ok {
		return rp.StreamRespond(ctx, req)
	}
	s, err := p.StreamChat(ctx, responseToChat(req))
	if err != nil {
		return nil, err
	}
	return &chatToResponseStream{stream: s}, nil
}

// ListModels returns models from the named provider, or all providers if empty.
func (r *Router) ListModels(ctx context.Context, providerName string) ([]*ModelInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if providerName != "" {
		p, ok := r.providers[providerName]
		if !ok {
			return nil, fmt.Errorf("unknown provider %q", providerName)
		}
		return p.ListModels(ctx)
	}
	var all []*ModelInfo
	for _, p := range r.providers {
		models, _ := p.ListModels(ctx) // best effort
		all = append(all, models...)
	}
	return all, nil
}

// Providers returns the names of all registered providers.
func (r *Router) Providers() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// responseToChat converts a ResponseRequest to a ProviderRequest for fallback.
func responseToChat(req *ResponseRequest) *ProviderRequest {
	pr := &ProviderRequest{
		Model:       req.Model,
		Tools:       req.Tools,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	if req.Instructions != "" {
		pr.Messages = append(pr.Messages, &Message{
			Role:    "system",
			Content: []*Block{{Type: "text", Text: req.Instructions}},
		})
	}
	switch v := req.Input.(type) {
	case string:
		pr.Messages = append(pr.Messages, &Message{
			Role:    "user",
			Content: []*Block{{Type: "text", Text: v}},
		})
	case []*InputItem:
		for _, item := range v {
			if item.Type == "message" {
				msg := &Message{Role: item.Role}
				if s, ok := item.Content.(string); ok {
					msg.Content = []*Block{{Type: "text", Text: s}}
				}
				pr.Messages = append(pr.Messages, msg)
			}
		}
	}
	return pr
}

// chatToResponseResult wraps a ProviderResponse as a ResponseResult.
func chatToResponseResult(resp *ProviderResponse) *ResponseResult {
	return &ResponseResult{
		ID:     resp.ID,
		Model:  resp.Model,
		Output: []*OutputItem{{Type: "message", Role: "assistant", Content: resp.Content}},
		Usage:  resp.Usage,
		Status: "completed",
	}
}

// chatToResponseStream wraps a chat Stream as a ResponseStream.
type chatToResponseStream struct {
	stream Stream
}

func (s *chatToResponseStream) Recv() (*ResponseEvent, error) {
	chunk, err := s.stream.Recv()
	if err != nil {
		return nil, err
	}
	if chunk == nil {
		return nil, io.EOF
	}
	evt := &ResponseEvent{
		Type:      "response.output_text.delta",
		TextDelta: chunk.TextDelta,
	}
	if chunk.Final != nil {
		evt.Type = "response.completed"
		evt.Response = chatToResponseResult(chunk.Final)
	}
	return evt, nil
}

func (s *chatToResponseStream) Close() error {
	return s.stream.Close()
}
