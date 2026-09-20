package config

import "encoding/base64"

// OtelExporterProtocol values.
const (
	OtelProtocolHTTP = "http/protobuf"
	OtelProtocolGRPC = "grpc"
)

type ObservabilityConfig struct {
	Enabled                   bool    `mapstructure:"enabled"`
	ServiceName               string  `mapstructure:"service_name"`
	ServiceVersion            string  `mapstructure:"service_version"`
	OtelExporterProtocol      string  `mapstructure:"otel_exporter_protocol"`
	OtelCollectorGRPCEndpoint string  `mapstructure:"otel_collector_grpc_endpoint"`
	OtelCollectorHTTPEndpoint string  `mapstructure:"otel_collector_http_endpoint"`
	OtelAuthInstanceID        string  `mapstructure:"otel_auth_instance_id"`
	OtelAuthToken             string  `mapstructure:"otel_auth_token"`
	OtelEnvironment           string  `mapstructure:"otel_environment"`
	TraceSamplingRate         float64 `mapstructure:"trace_sampling_rate"`
}

// AuthHeader returns the Authorization header value for OTLP exporters,
// built as Basic auth from the instance ID and token (Grafana Cloud style).
// Returns an empty string when no token is configured.
func (c *ObservabilityConfig) AuthHeader() string {
	if c.OtelAuthToken == "" {
		return ""
	}
	raw := c.OtelAuthInstanceID + ":" + c.OtelAuthToken
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(raw))
}

// UsesHTTPProtocol reports whether OTLP signals must be exported over
// HTTP/protobuf instead of gRPC.
func (c *ObservabilityConfig) UsesHTTPProtocol() bool {
	return c.OtelExporterProtocol == OtelProtocolHTTP
}
