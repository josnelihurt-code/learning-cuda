package telemetry

import (
	"context"
	"fmt"
	"time"

	"github.com/jrb/cuda-learning/src/go_api/pkg/config"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/logger"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type TracerProvider struct {
	provider *sdktrace.TracerProvider
	enabled  bool
}

func New(ctx context.Context, enabled bool, config *config.ObservabilityConfig) (*TracerProvider, error) {
	if !enabled {
		logger.Global().Info().Msg("Observability disabled by feature flag")
		return &TracerProvider{enabled: false}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resAttrs := []attribute.KeyValue{
		attribute.String("service.name", config.ServiceName),
		attribute.String("service.version", config.ServiceVersion),
	}
	if config.OtelEnvironment != "" {
		resAttrs = append(resAttrs, attribute.String("environment", config.OtelEnvironment))
	}
	res, err := resource.New(ctx, resource.WithAttributes(resAttrs...))
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	var exporter *otlptrace.Exporter
	if config.UsesHTTPProtocol() {
		exporter, err = newHTTPTraceExporter(ctx, config)
		if err != nil {
			return nil, err
		}
	} else {
		conn, err := grpc.NewClient(config.OtelCollectorGRPCEndpoint,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to OpenTelemetry collector at %s: %w", config.OtelCollectorGRPCEndpoint, err)
		}
		exporter, err = otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
		if err != nil {
			return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
		}
	}

	samplingRate := config.TraceSamplingRate
	var sampler sdktrace.Sampler
	switch {
	case samplingRate >= 1.0:
		sampler = sdktrace.AlwaysSample()
	case samplingRate <= 0.0:
		sampler = sdktrace.NeverSample()
	default:
		sampler = sdktrace.TraceIDRatioBased(samplingRate)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	logger.Global().Info().
		Str("protocol", config.OtelExporterProtocol).
		Str("http_endpoint", config.OtelCollectorHTTPEndpoint).
		Str("grpc_endpoint", config.OtelCollectorGRPCEndpoint).
		Float64("sampling_rate", config.TraceSamplingRate).
		Msg("OpenTelemetry tracer initialized")

	return &TracerProvider{
		provider: provider,
		enabled:  true,
	}, nil
}

func newHTTPTraceExporter(ctx context.Context, config *config.ObservabilityConfig) (*otlptrace.Exporter, error) {
	target, err := parseOTLPHTTPTarget(config.OtelCollectorHTTPEndpoint, config.AuthHeader())
	if err != nil {
		return nil, fmt.Errorf("failed to parse OTLP HTTP endpoint %s: %w", config.OtelCollectorHTTPEndpoint, err)
	}

	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(target.host),
		otlptracehttp.WithURLPath(target.path + "/v1/traces"),
		otlptracehttp.WithTimeout(30 * time.Second),
	}
	if headers := authHeaders(config); headers != nil {
		opts = append(opts, otlptracehttp.WithHeaders(headers))
	}
	if target.insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP HTTP trace exporter: %w", err)
	}
	return exporter, nil
}

func (tp *TracerProvider) Shutdown(ctx context.Context) error {
	if !tp.enabled || tp.provider == nil {
		return nil
	}

	logger.Global().Info().Msg("Shutting down OpenTelemetry tracer provider...")
	if err := tp.provider.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown tracer provider: %w", err)
	}
	logger.Global().Info().Msg("OpenTelemetry tracer provider shutdown complete")
	return nil
}
