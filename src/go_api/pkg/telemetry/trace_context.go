package telemetry

import (
	"context"

	pb "github.com/jrb/cuda-learning/proto/gen"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// extractFromProtobuf extracts trace context from protobuf TraceContext message
func extractFromProtobuf(ctx context.Context, traceCtx *pb.TraceContext) context.Context {
	if traceCtx == nil || traceCtx.Traceparent == "" {
		return ctx
	}

	carrier := propagation.MapCarrier{
		"traceparent": traceCtx.Traceparent,
	}
	if traceCtx.Tracestate != "" {
		carrier["tracestate"] = traceCtx.Tracestate
	}

	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}
