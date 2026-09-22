package application

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type tracedUseCase[Input any, Output any] struct {
	tracerName  string
	spanName    string
	inner       UseCase[Input, Output]
	inputAttrs  func(Input) []attribute.KeyValue
	outputAttrs func(Input, Output, error) []attribute.KeyValue
}

// WithTrace wraps a use case with an internal span. Wire at the composition root
// so tests can run the bare use case. Pass nil for unused attr callbacks.
func WithTrace[Input any, Output any](
	tracerName, spanName string,
	inner UseCase[Input, Output],
	inputAttrs func(Input) []attribute.KeyValue,
	outputAttrs func(Input, Output, error) []attribute.KeyValue,
) UseCase[Input, Output] {
	return &tracedUseCase[Input, Output]{
		tracerName:  tracerName,
		spanName:    spanName,
		inner:       inner,
		inputAttrs:  inputAttrs,
		outputAttrs: outputAttrs,
	}
}

func (t *tracedUseCase[Input, Output]) Execute(ctx context.Context, input Input) (Output, error) {
	ctx, span := otel.Tracer(t.tracerName).Start(ctx, t.spanName,
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	if t.inputAttrs != nil {
		span.SetAttributes(t.inputAttrs(input)...)
	}

	output, err := t.inner.Execute(ctx, input)
	if t.outputAttrs != nil {
		span.SetAttributes(t.outputAttrs(input, output, err)...)
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return output, err
}
