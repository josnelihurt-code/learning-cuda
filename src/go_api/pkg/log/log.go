// Package log provides the neutral, zerolog-backed logging API, importable
// from any layer without depending on infrastructure. Sink wiring lives in
// pkg/infrastructure/logger, which installs the global logger at startup.
package log

import (
	"context"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

// The zero value discards events until infrastructure installs the real
// logger at startup.
var global zerolog.Logger

// SetGlobal installs the process-global logger; only infrastructure wiring
// calls it.
func SetGlobal(l zerolog.Logger) {
	global = l
}

func Global() *zerolog.Logger {
	return &global
}

// FromContext returns the global logger, with trace_id/span_id attached
// when ctx carries a valid OpenTelemetry span.
func FromContext(ctx context.Context) *zerolog.Logger {
	spanContext := trace.SpanContextFromContext(ctx)

	if !spanContext.IsValid() {
		return &global
	}

	logger := global.With().
		Str("trace_id", spanContext.TraceID().String()).
		Str("span_id", spanContext.SpanID().String()).
		Logger()

	return &logger
}
