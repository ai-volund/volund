package forge

import (
	volundpb "github.com/ai-volund/volund-proto/gen/go/volund/v1"
)

// Service is the stub ForgeService implementation.
type Service struct {
	volundpb.UnimplementedForgeServiceServer
}

// NewService creates a new forge Service.
func NewService() *Service {
	return &Service{}
}
