package llm

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

// Router manages LLM providers and routes requests to the right one.
// It supports fallback chains (try A, then B on failure) and circuit breakers
// (stop sending to a consistently failing provider).
type Router struct {
	mu              sync.RWMutex
	providers       map[string]Provider
	circuits        map[string]*CircuitBreaker
	fallbackChains  map[string][]string // provider name → ordered fallback list
	defaultProvider string
}

// NewRouter creates a new Router with no providers registered.
func NewRouter() *Router {
	return &Router{
		providers:      make(map[string]Provider),
		circuits:       make(map[string]*CircuitBreaker),
		fallbackChains: make(map[string][]string),
	}
}

// Register adds a provider. The first registered provider becomes the default.
func (r *Router) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
	r.circuits[p.Name()] = NewCircuitBreaker()
	if r.defaultProvider == "" {
		r.defaultProvider = p.Name()
	}
}

// SetFallbackChain configures fallback providers for a given primary provider.
// Example: SetFallbackChain("anthropic", "openai", "ollama") — if Anthropic
// fails, try OpenAI, then Ollama.
func (r *Router) SetFallbackChain(primary string, fallbacks ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fallbackChains[primary] = fallbacks
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

// getWithFallbacks returns the provider chain: primary + fallbacks.
func (r *Router) getWithFallbacks(name string) ([]Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if name == "" {
		name = r.defaultProvider
	}
	if name == "" {
		return nil, fmt.Errorf("no LLM providers configured")
	}

	primary, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown LLM provider %q", name)
	}

	chain := []Provider{primary}
	if fallbacks, ok := r.fallbackChains[name]; ok {
		for _, fb := range fallbacks {
			if p, ok := r.providers[fb]; ok {
				chain = append(chain, p)
			}
		}
	}
	return chain, nil
}

func (r *Router) circuit(name string) *CircuitBreaker {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.circuits[name]
}

// Chat routes a non-streaming chat request with fallback + circuit breaker support.
func (r *Router) Chat(ctx context.Context, providerName string, req *ProviderRequest) (*ProviderResponse, error) {
	chain, err := r.getWithFallbacks(providerName)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, p := range chain {
		cb := r.circuit(p.Name())
		if cb != nil && !cb.Allow() {
			slog.Debug("circuit open, skipping provider", "provider", p.Name())
			lastErr = fmt.Errorf("provider %q circuit open", p.Name())
			continue
		}

		resp, err := p.Chat(ctx, req)
		if err != nil {
			if cb != nil {
				cb.RecordFailure()
			}
			slog.Warn("provider failed, trying fallback", "provider", p.Name(), "error", err)
			lastErr = err
			continue
		}

		if cb != nil {
			cb.RecordSuccess()
		}
		return resp, nil
	}

	return nil, fmt.Errorf("all providers failed: %w", lastErr)
}

// StreamChat routes a streaming chat request with fallback + circuit breaker support.
func (r *Router) StreamChat(ctx context.Context, providerName string, req *ProviderRequest) (Stream, error) {
	chain, err := r.getWithFallbacks(providerName)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, p := range chain {
		cb := r.circuit(p.Name())
		if cb != nil && !cb.Allow() {
			lastErr = fmt.Errorf("provider %q circuit open", p.Name())
			continue
		}

		s, err := p.StreamChat(ctx, req)
		if err != nil {
			if cb != nil {
				cb.RecordFailure()
			}
			slog.Warn("stream provider failed, trying fallback", "provider", p.Name(), "error", err)
			lastErr = err
			continue
		}

		if cb != nil {
			cb.RecordSuccess()
		}
		return s, nil
	}

	return nil, fmt.Errorf("all providers failed: %w", lastErr)
}

// Embed routes an embedding request to the appropriate provider.
func (r *Router) Embed(ctx context.Context, providerName, model string, texts []string) ([][]float32, *Usage, error) {
	chain, err := r.getWithFallbacks(providerName)
	if err != nil {
		return nil, nil, err
	}

	var lastErr error
	for _, p := range chain {
		cb := r.circuit(p.Name())
		if cb != nil && !cb.Allow() {
			lastErr = fmt.Errorf("provider %q circuit open", p.Name())
			continue
		}

		vecs, usage, err := p.Embed(ctx, model, texts)
		if err != nil {
			if cb != nil {
				cb.RecordFailure()
			}
			lastErr = err
			continue
		}
		if cb != nil {
			cb.RecordSuccess()
		}
		return vecs, usage, nil
	}

	return nil, nil, fmt.Errorf("all providers failed: %w", lastErr)
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
	resp, err := r.Chat(ctx, providerName, responseToChat(req))
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
	s, err := r.StreamChat(ctx, providerName, responseToChat(req))
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

// ProviderHealth returns the circuit breaker state for each provider.
func (r *Router) ProviderHealth() map[string]CircuitState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	health := make(map[string]CircuitState, len(r.circuits))
	for name, cb := range r.circuits {
		health[name] = cb.State()
	}
	return health
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
