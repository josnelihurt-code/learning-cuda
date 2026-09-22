package logger

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	otelog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace"
)

type otlpHook struct {
	logger otelog.Logger
	ctx    context.Context
}

func newOTLPHook(endpoint, environment, serviceName, serviceVersion, authHeader string) (zerolog.Hook, error) {
	ctx := context.Background()

	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", serviceName),
			attribute.String("service.version", serviceVersion),
			attribute.String("environment", environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	parsedURL, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to parse endpoint URL: %w", err)
	}

	host := parsedURL.Host
	path := parsedURL.Path
	if path == "" {
		path = "/v1/logs"
	}

	opts := []otlploghttp.Option{
		otlploghttp.WithEndpoint(host),
		otlploghttp.WithURLPath(path),
		otlploghttp.WithTimeout(30 * time.Second),
	}
	if authHeader != "" {
		opts = append(opts, otlploghttp.WithHeaders(map[string]string{"Authorization": authHeader}))
	}

	if parsedURL.Scheme == "http" {
		opts = append(opts, otlploghttp.WithInsecure())
	}

	exporter, err := otlploghttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP log exporter: %w", err)
	}

	loggerProvider := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(
			sdklog.NewBatchProcessor(exporter),
		),
	)

	global.SetLoggerProvider(loggerProvider)
	otelLogger := global.Logger("zerolog")

	return &otlpHook{
		logger: otelLogger,
		ctx:    ctx,
	}, nil
}

func (h *otlpHook) Run(e *zerolog.Event, level zerolog.Level, message string) {
	ctx := h.ctx
	spanContext := trace.SpanContextFromContext(ctx)
	severity := mapZerologToOTELSeverity(level)

	record := otelog.Record{}
	record.SetTimestamp(time.Now())
	record.SetSeverity(severity)
	record.SetSeverityText(level.String())
	record.SetBody(otelog.StringValue(message))
	record.SetObservedTimestamp(time.Now())

	record.AddAttributes(
		otelog.String("message", message),
		otelog.String("level", level.String()),
	)

	if spanContext.IsValid() {
		record.AddAttributes(
			otelog.String("trace_id", spanContext.TraceID().String()),
			otelog.String("span_id", spanContext.SpanID().String()),
		)
	}

	h.logger.Emit(ctx, record)
}

func mapZerologToOTELSeverity(level zerolog.Level) otelog.Severity {
	switch level {
	case zerolog.TraceLevel, zerolog.DebugLevel:
		return otelog.SeverityDebug
	case zerolog.InfoLevel:
		return otelog.SeverityInfo
	case zerolog.WarnLevel:
		return otelog.SeverityWarn
	case zerolog.ErrorLevel:
		return otelog.SeverityError
	case zerolog.FatalLevel, zerolog.PanicLevel:
		return otelog.SeverityFatal
	case zerolog.NoLevel, zerolog.Disabled:
		return otelog.SeverityInfo
	default:
		return otelog.SeverityInfo
	}
}
