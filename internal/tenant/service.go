package tenant

import (
	volundpb "github.com/ai-volund/volund-proto/gen/go/volund/v1"
)

// Service is the stub TenantService implementation.
type Service struct {
	volundpb.UnimplementedTenantServiceServer
}

// NewService creates a new tenant Service.
func NewService() *Service {
	return &Service{}
}
