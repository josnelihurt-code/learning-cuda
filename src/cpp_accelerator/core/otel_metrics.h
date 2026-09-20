#pragma once

#include <cstdint>
#include <string>

namespace jrb::core::otel {

// Initializes the global OpenTelemetry meter provider from environment:
//   OTEL_METRICS_ENABLED   ("true"/"1" to enable)
//   OTEL_METRICS_ENDPOINT  full OTLP/HTTP metrics URL (incl. /v1/metrics)
//   OTEL_ENVIRONMENT, OTEL_SERVICE_NAME, and the shared auth envs
//   documented in otel_config.h.
// Idempotent; no-op (with a log line) when disabled or misconfigured.
void InitializeMetrics();

// Flushes and releases the meter provider. Safe to call when disabled.
void ShutdownMetrics();

namespace metrics {

// No-ops when metrics are not initialized. Instruments are created lazily
// and cached; the optional attribute adds one label to the series.
void Count(const std::string& name, uint64_t delta = 1,
           const std::string& attr_key = "", const std::string& attr_value = "");

void ObserveMs(const std::string& name, double milliseconds);

}  // namespace metrics
}  // namespace jrb::core::otel
