package task

import (
	volundpb "github.com/ai-volund/volund-proto/gen/go/volund/v1"
)

// Service is the stub TaskService implementation.
type Service struct {
	volundpb.UnimplementedTaskServiceServer
}

// NewService creates a new task Service.
func NewService() *Service {
	return &Service{}
}
