package agent

import (
	volundpb "github.com/ai-volund/volund-proto/gen/go/volund/v1"
)

// Service is the stub AgentService implementation.
type Service struct {
	volundpb.UnimplementedAgentServiceServer
}

// NewService creates a new agent Service.
func NewService() *Service {
	return &Service{}
}
