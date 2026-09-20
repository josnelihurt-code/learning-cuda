#include "src/cpp_accelerator/core/otel_metrics.h"

#include <atomic>
#include <chrono>
#include <cstdlib>
#include <map>
#include <memory>
#include <mutex>
#include <string>
#include <utility>

#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wdeprecated-declarations"
#include <spdlog/spdlog.h>
#pragma GCC diagnostic pop

#include "opentelemetry/exporters/otlp/otlp_grpc_metric_exporter_factory.h"
#include "opentelemetry/exporters/otlp/otlp_grpc_metric_exporter_options.h"
#include "opentelemetry/metrics/noop.h"
#include "opentelemetry/metrics/provider.h"
#include "opentelemetry/sdk/metrics/export/periodic_exporting_metric_reader_factory.h"
#include "opentelemetry/sdk/metrics/export/periodic_exporting_metric_reader_options.h"
#include "opentelemetry/sdk/metrics/meter_provider.h"
#include "opentelemetry/sdk/metrics/view/view_registry.h"
#include "opentelemetry/sdk/resource/resource.h"
#include "opentelemetry/sdk/resource/semantic_conventions.h"

#include "src/cpp_accelerator/core/otel_config.h"
#include "src/cpp_accelerator/core/version.h"

namespace jrb::core::otel {

namespace metrics_api = opentelemetry::metrics;
namespace metrics_sdk = opentelemetry::sdk::metrics;
namespace nostd = opentelemetry::nostd;
namespace resource = opentelemetry::sdk::resource;
namespace otlp = opentelemetry::exporter::otlp;

namespace {

constexpr const char* kDefaultServiceName = "cuda-cpp-accelerator";

std::mutex g_mutex;
std::atomic<bool> g_enabled{false};
bool g_initialized = false;
std::shared_ptr<metrics_sdk::MeterProvider> g_provider;
nostd::shared_ptr<metrics_api::Meter> g_meter;
std::map<std::string, nostd::unique_ptr<metrics_api::Counter<uint64_t>>> g_counters;
std::map<std::string, nostd::unique_ptr<metrics_api::Histogram<double>>> g_histograms;

std::string EnvValue(const char* name) {
  const char* value = std::getenv(name);
  return value == nullptr ? std::string{} : std::string(value);
}

}  // namespace

void InitializeMetrics() {
  std::lock_guard<std::mutex> lock(g_mutex);
  if (g_initialized) {
    return;
  }
  g_initialized = true;

  if (!EnvFlagEnabled("OTEL_METRICS_ENABLED")) {
    spdlog::info("OpenTelemetry metrics disabled (OTEL_METRICS_ENABLED != true)");
    return;
  }

  const std::string endpoint = EnvValue("OTEL_METRICS_ENDPOINT");
  if (endpoint.empty()) {
    spdlog::warn("OpenTelemetry metrics enabled but OTEL_METRICS_ENDPOINT is not set");
    return;
  }

  try {
    // OTLP/gRPC to the local collector sidecar (http:// marks insecure).
    // The bazel-built libcurl has no TLS backend and its gRPC channel cannot
    // negotiate ALPN, so TLS termination happens in the otel-collector.
    otlp::OtlpGrpcMetricExporterOptions opts;
    opts.endpoint = NormalizeGrpcEndpoint(endpoint);
    opts.use_ssl_credentials = endpoint.rfind("https://", 0) == 0;
    opts.timeout = std::chrono::seconds(10);
    const std::string auth = AuthorizationHeader();
    if (!auth.empty()) {
      opts.metadata.emplace("authorization", auth);
    }

    auto exporter = otlp::OtlpGrpcMetricExporterFactory::Create(opts);

    metrics_sdk::PeriodicExportingMetricReaderOptions reader_opts;
    reader_opts.export_interval_millis = std::chrono::milliseconds(60000);
    reader_opts.export_timeout_millis = std::chrono::milliseconds(10000);
    auto reader = metrics_sdk::PeriodicExportingMetricReaderFactory::Create(
        std::move(exporter), reader_opts);

    const std::string service_name = ServiceName(kDefaultServiceName);
    const std::string service_version = kLibraryVersionStr;
    auto resource_ptr = resource::Resource::Create(resource::ResourceAttributes{
        {resource::SemanticConventions::kServiceName, service_name},
        {resource::SemanticConventions::kServiceVersion, service_version},
        {"environment", EnvValue("OTEL_ENVIRONMENT")}});

    std::shared_ptr<metrics_sdk::MeterProvider> provider(new metrics_sdk::MeterProvider(
        std::unique_ptr<metrics_sdk::ViewRegistry>(new metrics_sdk::ViewRegistry()),
        resource_ptr));
    provider->AddMetricReader(std::move(reader));
    metrics_api::Provider::SetMeterProvider(provider);

    g_meter = provider->GetMeter(service_name, "", service_version);
    g_provider = std::move(provider);
    g_enabled.store(true);

    spdlog::info("OpenTelemetry metrics initialized (gRPC endpoint: {}, auth: {})", opts.endpoint,
                 auth.empty() ? "none" : "basic");
  } catch (const std::exception& e) {
    spdlog::error("Failed to initialize OpenTelemetry metrics: {}", e.what());
  }
}

void ShutdownMetrics() {
  std::lock_guard<std::mutex> lock(g_mutex);
  if (!g_enabled.exchange(false)) {
    return;
  }
  // Replace the global provider first so static destruction cannot shut the
  // SDK provider down a second time.
  metrics_api::Provider::SetMeterProvider(
      std::shared_ptr<metrics_api::MeterProvider>(new metrics_api::NoopMeterProvider()));
  try {
    if (g_provider) {
      g_provider->Shutdown();
    }
  } catch (const std::exception& e) {
    spdlog::warn("OpenTelemetry metrics shutdown error: {}", e.what());
  }
  g_counters.clear();
  g_histograms.clear();
  g_meter = nullptr;
  g_provider.reset();
  spdlog::info("OpenTelemetry metrics shut down");
}

namespace metrics {

void Count(const std::string& name, uint64_t delta, const std::string& attr_key,
           const std::string& attr_value) {
  if (!g_enabled.load()) {
    return;
  }
  std::lock_guard<std::mutex> lock(g_mutex);
  if (g_meter == nullptr) {
    return;
  }
  auto& counter = g_counters[name];
  if (counter == nullptr) {
    counter = g_meter->CreateUInt64Counter(name, name, "1");
    if (counter == nullptr) {
      return;
    }
  }
  if (attr_key.empty()) {
    counter->Add(delta);
  } else {
    counter->Add(delta, {{attr_key, attr_value}});
  }
}

void ObserveMs(const std::string& name, double milliseconds) {
  if (!g_enabled.load()) {
    return;
  }
  std::lock_guard<std::mutex> lock(g_mutex);
  if (g_meter == nullptr) {
    return;
  }
  auto& histogram = g_histograms[name];
  if (histogram == nullptr) {
    histogram = g_meter->CreateDoubleHistogram(name, name, "ms");
    if (histogram == nullptr) {
      return;
    }
  }
  histogram->Record(milliseconds, opentelemetry::context::Context{});
}

}  // namespace metrics
}  // namespace jrb::core::otel
