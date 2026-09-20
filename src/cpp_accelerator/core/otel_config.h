#pragma once

#include <string>

namespace jrb::core::otel {

// Returns the Authorization header value for OTLP exporters ("Basic ..."),
// built from OTEL_EXPORTER_OTLP_TOKEN + OTEL_AUTH_INSTANCE_ID, or
// OTEL_AUTH_HEADER verbatim when set. Empty when no token is configured.
std::string AuthorizationHeader();

// Returns the OTLP service.name: OTEL_SERVICE_NAME when set, otherwise the
// given fallback.
std::string ServiceName(const std::string& fallback);

// Returns true when the env variable equals "true" or "1".
bool EnvFlagEnabled(const char* name);

// Normalizes an OTLP endpoint ("https://host:443/path", "host", "host:4317")
// to the gRPC "host:port" form. Defaults to port 443 for https/bare hosts
// and 80 for http.
std::string NormalizeGrpcEndpoint(const std::string& endpoint);

}  // namespace jrb::core::otel
