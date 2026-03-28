package chat

import (
	volundpb "github.com/ai-volund/volund-proto/gen/go/volund/v1"
)

// Service is the stub ChatService implementation.
type Service struct {
	volundpb.UnimplementedChatServiceServer
}

// NewService creates a new chat Service.
func NewService() *Service {
	return &Service{}
}
