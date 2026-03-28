package usage

import (
	volundpb "github.com/ai-volund/volund-proto/gen/go/volund/v1"
)

// Service is the stub UsageService implementation.
type Service struct {
	volundpb.UnimplementedUsageServiceServer
}

// NewService creates a new usage Service.
func NewService() *Service {
	return &Service{}
}
