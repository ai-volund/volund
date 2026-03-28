package llm

import (
	volundpb "github.com/ai-volund/volund-proto/gen/go/volund/v1"
)

// Service is the stub LLMService implementation.
type Service struct {
	volundpb.UnimplementedLLMServiceServer
}

// NewService creates a new LLM Service.
func NewService() *Service {
	return &Service{}
}
