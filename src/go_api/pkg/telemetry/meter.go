package telemetry

import (
	"context"
	"fmt"
	"time"

	"github.com/jrb/cuda-learning/src/go_api/pkg/config"
	"github.com/jrb/cuda-learning/src/go_api/pkg/infrastructure/logger"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

// MeterProvider wraps the SDK meter provider so the caller can shut it
// down cleanly, mirroring TracerProvider.
type MeterProvider struct {
	provider *metric.MeterProvider
	enabled  bool
}

// NewMeterProvider initializes the global OTel meter provider exporting
// over OTLP/HTTP. Metrics always use the HTTP endpoint: local collectors
// expose 4318 and hosted gateways (e.g. Grafana Cloud) are HTTP-only.
// Setting the global provider makes net/http otelhttp instrumentation
// report http.server.* metrics automatically.
func NewMeterProvider(ctx context.Context, enabled bool, config *config.ObservabilityConfig) (*MeterProvider, error) {
	if !enabled {
		logger.Global().Info().Msg("OpenTelemetry metrics disabled by feature flag")
		return &MeterProvider{enabled: false}, nil
	}

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

	target, err := parseOTLPHTTPTarget(config.OtelCollectorHTTPEndpoint, config.AuthHeader())
	if err != nil {
		return nil, fmt.Errorf("failed to parse OTLP HTTP endpoint %s: %w", config.OtelCollectorHTTPEndpoint, err)
	}

	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(target.host),
		otlpmetrichttp.WithURLPath(target.path + "/v1/metrics"),
		otlpmetrichttp.WithTimeout(30 * time.Second),
	}
	if headers := authHeaders(config); headers != nil {
		opts = append(opts, otlpmetrichttp.WithHeaders(headers))
	}
	if target.insecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}

	exporter, err := otlpmetrichttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP metric exporter: %w", err)
	}

	provider := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(metric.NewPeriodicReader(exporter,
			metric.WithInterval(60*time.Second))),
	)
	otel.SetMeterProvider(provider)

	logger.Global().Info().
		Str("http_endpoint", config.OtelCollectorHTTPEndpoint).
		Msg("OpenTelemetry meter provider initialized")

	return &MeterProvider{
		provider: provider,
		enabled:  true,
	}, nil
}

func (mp *MeterProvider) Shutdown(ctx context.Context) error {
	if !mp.enabled || mp.provider == nil {
		return nil
	}

	logger.Global().Info().Msg("Shutting down OpenTelemetry meter provider...")
	if err := mp.provider.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown meter provider: %w", err)
	}
	logger.Global().Info().Msg("OpenTelemetry meter provider shutdown complete")
	return nil
}
