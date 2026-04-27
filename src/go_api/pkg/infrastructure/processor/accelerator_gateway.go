package processor

import (
	"context"

	gen "github.com/jrb/cuda-learning/proto/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AcceleratorGateway struct {
	registry *Registry
}

type AcceleratorGatewayConfig struct {
	Registry *Registry
}

func NewAcceleratorGateway(cfg AcceleratorGatewayConfig) *AcceleratorGateway {
	return &AcceleratorGateway{registry: cfg.Registry}
}

// IsAvailable reports whether at least one accelerator is currently registered
// on the inbound control stream. Used by health checks.
func (c *AcceleratorGateway) IsAvailable() bool {
	if c.registry == nil {
		return false
	}
	_, ok := c.registry.First()
	return ok
}

// SignalingStream returns a bidi-stream adapter that routes signaling messages
// through the registered accelerator's control stream instead of a direct gRPC dial.
func (c *AcceleratorGateway) SignalingStream(ctx context.Context) (gen.WebRTCSignalingService_SignalingStreamClient, error) {
	if c.registry == nil {
		return nil, status.Error(codes.Unavailable, "no accelerator registry configured")
	}
	sess, ok := c.registry.First()
	if !ok {
		return nil, status.Error(codes.Unavailable, "no accelerator registered")
	}
	return newSignalingStreamAdapter(ctx, sess), nil
}
