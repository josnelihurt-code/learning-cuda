package telemetry

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/jrb/cuda-learning/proto/gen"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// TraceContextInterceptor extracts trace context from protobuf messages and creates spans
func TraceContextInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			// Extract from protobuf TraceContext if present
			if msg := req.Any(); msg != nil {
				if v, ok := msg.(interface{ GetTraceContext() *pb.TraceContext }); ok {
					if tc := v.GetTraceContext(); tc != nil {
						ctx = extractFromProtobuf(ctx, tc)
					}
				}
			}

			// Create span for this RPC call
			tracer := otel.Tracer("connectrpc")
			ctx, span := tracer.Start(ctx, req.Spec().Procedure,
				trace.WithSpanKind(trace.SpanKindServer))
			defer span.End()

			// Call next handler with enriched context
			resp, err := next(ctx, req)
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			} else {
				span.SetStatus(codes.Ok, "")
			}

			return resp, err
		}
	}
}
