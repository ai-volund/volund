package llm

import "context"

// Provider is the interface every LLM backend must implement.
type Provider interface {
	Name() string
	Chat(ctx context.Context, req *ProviderRequest) (*ProviderResponse, error)
	StreamChat(ctx context.Context, req *ProviderRequest) (Stream, error)
	Embed(ctx context.Context, model string, texts []string) ([][]float32, *Usage, error)
	ListModels(ctx context.Context) ([]*ModelInfo, error)
	HealthCheck(ctx context.Context) error
}

// ResponderProvider is an optional extension for providers that natively
// support the stateful Responses API.
type ResponderProvider interface {
	Provider
	Respond(ctx context.Context, req *ResponseRequest) (*ResponseResult, error)
	StreamRespond(ctx context.Context, req *ResponseRequest) (ResponseStream, error)
}

// Stream is a server-side streaming interface for chat completions.
// Recv returns (nil, io.EOF) when the stream ends successfully.
type Stream interface {
	Recv() (*Chunk, error)
	Close() error
}

// ResponseStream is a streaming interface for the Responses API.
type ResponseStream interface {
	Recv() (*ResponseEvent, error)
	Close() error
}

// ProviderRequest is the internal, provider-agnostic chat request.
type ProviderRequest struct {
	Model         string
	Messages      []*Message
	Temperature   float64
	MaxTokens     int32
	StopSequences []string
	Tools         []*ToolDef
}

// ResponseRequest is a request for the stateful Responses API.
type ResponseRequest struct {
	Model              string
	Input              interface{} // string or []*InputItem
	Instructions       string
	PreviousResponseID string
	Tools              []*ToolDef
	Temperature        float64
	MaxTokens          int32
}

// InputItem is a single input item for the Responses API.
type InputItem struct {
	Type    string      // "message", "function_call_output"
	Role    string      // "user", "assistant", "system"
	Content interface{} // string or []*Block
	CallID  string      // for function_call_output
	Output  string      // for function_call_output
}

// ResponseResult is the result from the Responses API.
type ResponseResult struct {
	ID     string
	Model  string
	Output []*OutputItem
	Usage  Usage
	Status string // "completed", "in_progress", "failed", "cancelled"
}

// OutputItem is a single output item from the Responses API.
type OutputItem struct {
	Type      string // "message", "function_call"
	Role      string
	Content   []*Block
	CallID    string
	Name      string
	Arguments string
}

// ResponseEvent is a streaming event from the Responses API.
type ResponseEvent struct {
	Type      string
	TextDelta string
	Response  *ResponseResult // non-nil on "response.completed"
}

// Message is a single conversation turn.
type Message struct {
	Role       string // "system" | "user" | "assistant" | "tool"
	Content    []*Block
	ToolCallID string // non-empty for role="tool"
}

// Block is a single content block within a message.
type Block struct {
	Type string // "text" | "image" | "tool_use" | "tool_result"
	// text
	Text string
	// image
	Image *ImageData
	// tool_use
	ToolUse *ToolUseData
	// tool_result
	ToolResult *ToolResultData
}

// ImageData holds an image reference.
type ImageData struct {
	MediaType string
	Data      []byte
	URL       string
}

// ToolUseData is a tool call from the assistant.
type ToolUseData struct {
	ID        string
	Name      string
	InputJSON string
}

// ToolResultData is the result of a tool call.
type ToolResultData struct {
	ToolUseID string
	Content   string
	IsError   bool
}

// ToolDef defines a tool available to the model.
type ToolDef struct {
	Name            string
	Description     string
	InputSchemaJSON string
}

// ProviderResponse is the final response from a provider.
type ProviderResponse struct {
	ID         string
	Model      string
	Content    []*Block
	StopReason string // "end_turn" | "tool_use" | "max_tokens"
	Usage      Usage
}

// Chunk is a single streaming delta.
type Chunk struct {
	TextDelta string
	ToolUse   *ToolUseData    // complete tool call (when accumulation finishes)
	Final     *ProviderResponse // non-nil on the last chunk
}

// Usage holds token usage info.
type Usage struct {
	InputTokens      int32
	OutputTokens     int32
	CacheReadTokens  int32
	CacheWriteTokens int32
}

// ModelInfo describes a single available model.
type ModelInfo struct {
	Provider           string
	ID                 string
	DisplayName        string
	ContextWindow      int32
	MaxOutputTokens    int32
	SupportsVision     bool
	SupportsToolUse    bool
	SupportsStreaming   bool
	InputPricePerMTok  float64
	OutputPricePerMTok float64
}
