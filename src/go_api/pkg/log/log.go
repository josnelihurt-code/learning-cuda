// Package log provides the neutral logging API shared across every layer of
// the service. It wraps zerolog — a third-party library — so importing it
// never creates a dependency on our own infrastructure packages, and any
// layer (application, interfaces, infrastructure) may use it freely.
//
// The package is deliberately small: it exposes only the process-global
// logger and a context helper with trace correlation. Configuration and sink
// wiring (level, console/file output, OTLP remote logging) live in
// pkg/infrastructure/logger, which installs the configured logger here at
// startup via SetGlobal.
package log

import (
	"context"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

// global is the process-global logger installed by infrastructure at
// startup; the zero value discards events until then.
var global zerolog.Logger

// SetGlobal installs l as the process-global logger. It is called by the
// infrastructure logger package when it wires sinks; application code should
// not call it.
func SetGlobal(l zerolog.Logger) {
	global = l
}

// Global returns the process-global logger.
func Global() *zerolog.Logger {
	return &global
}

// FromContext returns the logger for ctx: the process-global logger with
// trace_id/span_id fields attached when ctx carries a valid OpenTelemetry
// span, and the process-global logger as-is otherwise.
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
