package telemetry

import (
	"net/url"

	"github.com/jrb/cuda-learning/src/go_api/pkg/config"
)

// otlpHTTPTarget is the parsed form of an OTLP HTTP base endpoint.
type otlpHTTPTarget struct {
	host     string
	path     string
	insecure bool
	auth     string
}

// parseOTLPHTTPTarget splits an OTLP base endpoint URL (scheme://host[:port][/prefix])
// into exporter options. The signal path suffix (/v1/traces, /v1/metrics) is
// appended by each exporter.
func parseOTLPHTTPTarget(endpoint, authHeader string) (otlpHTTPTarget, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return otlpHTTPTarget{}, err
	}
	return otlpHTTPTarget{
		host:     parsed.Host,
		path:     parsed.Path,
		insecure: parsed.Scheme == "http",
		auth:     authHeader,
	}, nil
}

// authHeaders returns the Authorization header map for OTLP exporters,
// or nil when no auth is configured.
func authHeaders(cfg *config.ObservabilityConfig) map[string]string {
	header := cfg.AuthHeader()
	if header == "" {
		return nil
	}
	return map[string]string{"Authorization": header}
}
