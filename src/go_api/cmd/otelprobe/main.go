// Command otelprobe validates an OTLP ingestion endpoint end to end by
// sending one trace, several log records and a counter metric, then
// flushing every provider so the data can be verified immediately.
//
// It is safe to run against production: it sends a handful of records
// tagged with service.name from the -service flag and exits.
package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	metricapi "go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type target struct {
	host      string
	path      string
	insecure  bool
	authValue string
}

func parseTarget(endpoint, authHeader, token, instanceID string) (target, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return target{}, err
	}
	path := parsed.Path
	if path == "" {
		path = ""
	}
	auth := authHeader
	if auth == "" && token != "" {
		auth = "Basic " + base64.StdEncoding.EncodeToString([]byte(instanceID+":"+token))
	}
	return target{
		host:      parsed.Host,
		path:      path,
		insecure:  parsed.Scheme == "http",
		authValue: auth,
	}, nil
}

func main() {
	endpoint := flag.String("endpoint", "", "OTLP base endpoint, e.g. https://otlp-gateway-prod-us-west-0.grafana.net/otlp")
	token := flag.String("token", "", "API token; combined with -instance-id into Basic auth when set")
	instanceID := flag.String("instance-id", "", "Numeric instance ID used as the Basic auth username")
	authHeader := flag.String("auth-header", "", "Raw Authorization header value (overrides -token/-instance-id)")
	service := flag.String("service", "otelprobe", "service.name resource attribute")
	environment := flag.String("environment", "probe", "environment resource attribute")
	flag.Parse()

	if *endpoint == "" {
		fmt.Fprintln(os.Stderr, "-endpoint is required")
		os.Exit(2)
	}

	tgt, err := parseTarget(*endpoint, *authHeader, *token, *instanceID)
	if err != nil {
		fatal("endpoint", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", *service),
			attribute.String("service.version", "probe"),
			attribute.String("environment", *environment),
		),
	)
	if err != nil {
		fatal("resource", err)
	}

	// Logs
	logExporter, err := newLogExporter(ctx, tgt)
	if err != nil {
		fatal("log exporter", err)
	}
	logProvider := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
	)
	defer logProvider.Shutdown(context.Background())

	global.SetLoggerProvider(logProvider)
	otelLogger := global.Logger("otelprobe")
	for i := 1; i <= 3; i++ {
		record := otelog.Record{}
		record.SetTimestamp(time.Now())
		record.SetObservedTimestamp(time.Now())
		record.SetSeverity(otelog.SeverityInfo)
		record.SetSeverityText("info")
		record.SetBody(otelog.StringValue("otelprobe validation log " + strconv.Itoa(i)))
		record.AddAttributes(otelog.String("probe", "otelprobe"))
		otelLogger.Emit(ctx, record)
	}

	// Metrics
	metricExporter, err := newMetricExporter(ctx, tgt)
	if err != nil {
		fatal("metric exporter", err)
	}
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter,
			sdkmetric.WithInterval(2*time.Second))),
	)
	defer meterProvider.Shutdown(context.Background())

	counter, _ := meterProvider.Meter("otelprobe").Int64Counter("otelprobe_validation_events")
	counter.Add(ctx, 3, metricapi.WithAttributes(attribute.String("probe", "otelprobe")))

	// Traces
	traceExporter, err := newTraceExporter(ctx, tgt)
	if err != nil {
		fatal("trace exporter", err)
	}
	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	defer traceProvider.Shutdown(context.Background())

	tracer := traceProvider.Tracer("otelprobe")
	ctx, span := tracer.Start(ctx, "otelprobe.validation")
	_, child := tracer.Start(ctx, "otelprobe.validation.child")
	child.End()
	fmt.Printf("trace_id=%s\n", span.SpanContext().TraceID())
	span.End()

	// Force flush so the data is queryable right away.
	if err := logProvider.Shutdown(ctx); err != nil {
		fatal("log flush", err)
	}
	if err := meterProvider.ForceFlush(ctx); err != nil {
		fatal("metric flush", err)
	}
	if err := traceProvider.ForceFlush(ctx); err != nil {
		fatal("trace flush", err)
	}

	fmt.Println("OTLP probe finished: 3 logs, 1 metric series, 1 trace sent")
}

func newLogExporter(ctx context.Context, tgt target) (*otlploghttp.Exporter, error) {
	opts := []otlploghttp.Option{
		otlploghttp.WithEndpoint(tgt.host),
		otlploghttp.WithURLPath(tgt.path + "/v1/logs"),
		otlploghttp.WithTimeout(30 * time.Second),
	}
	if tgt.authValue != "" {
		opts = append(opts, otlploghttp.WithHeaders(map[string]string{"Authorization": tgt.authValue}))
	}
	if tgt.insecure {
		opts = append(opts, otlploghttp.WithInsecure())
	}
	return otlploghttp.New(ctx, opts...)
}

func newMetricExporter(ctx context.Context, tgt target) (*otlpmetrichttp.Exporter, error) {
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(tgt.host),
		otlpmetrichttp.WithURLPath(tgt.path + "/v1/metrics"),
		otlpmetrichttp.WithTimeout(30 * time.Second),
	}
	if tgt.authValue != "" {
		opts = append(opts, otlpmetrichttp.WithHeaders(map[string]string{"Authorization": tgt.authValue}))
	}
	if tgt.insecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}
	return otlpmetrichttp.New(ctx, opts...)
}

func newTraceExporter(ctx context.Context, tgt target) (*otlptrace.Exporter, error) {
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(tgt.host),
		otlptracehttp.WithURLPath(tgt.path + "/v1/traces"),
		otlptracehttp.WithTimeout(30 * time.Second),
	}
	if tgt.authValue != "" {
		opts = append(opts, otlptracehttp.WithHeaders(map[string]string{"Authorization": tgt.authValue}))
	}
	if tgt.insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	return otlptracehttp.New(ctx, opts...)
}

func fatal(stage string, err error) {
	fmt.Fprintf(os.Stderr, "otelprobe: %s: %v\n", stage, err)
	os.Exit(1)
}
