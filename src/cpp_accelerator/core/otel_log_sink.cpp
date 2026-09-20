#include "src/cpp_accelerator/core/otel_log_sink.h"

#include <chrono>
#include <cstring>
#include <memory>
#include <sstream>
#include <string>

#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wdeprecated-declarations"
#include <spdlog/details/log_msg.h>
#include <spdlog/spdlog.h>
#pragma GCC diagnostic pop

#include "src/cpp_accelerator/core/version.h"

#include "src/cpp_accelerator/core/otel_config.h"

// OpenTelemetry includes
#include "opentelemetry/exporters/otlp/otlp_grpc_log_record_exporter_factory.h"
#include "opentelemetry/exporters/otlp/otlp_grpc_log_record_exporter_options.h"
#include "opentelemetry/logs/provider.h"
#include "opentelemetry/sdk/logs/batch_log_record_processor.h"
#include "opentelemetry/sdk/logs/logger_provider.h"
#include "opentelemetry/sdk/logs/simple_log_record_processor.h"
#include "opentelemetry/sdk/resource/resource.h"
#include "opentelemetry/sdk/resource/semantic_conventions.h"

namespace jrb::core {

namespace logs_api = opentelemetry::logs;
namespace logs_sdk = opentelemetry::sdk::logs;
namespace otlp = opentelemetry::exporter::otlp;
namespace resource = opentelemetry::sdk::resource;

namespace {
constexpr const char* kDefaultServiceName = "cuda-cpp-accelerator";
}  // namespace

// PIMPL implementation to hide OpenTelemetry details
class OtelLogSinkImpl {
public:
  OtelLogSinkImpl(const std::string& endpoint, const std::string& environment) {
    try {
      // OTLP/gRPC to the local collector sidecar (http:// marks insecure).
      // The bazel-built libcurl has no TLS backend and its gRPC channel cannot
      // negotiate ALPN, so TLS termination happens in the otel-collector.
      const bool use_ssl = endpoint.rfind("https://", 0) == 0;

      otlp::OtlpGrpcLogRecordExporterOptions opts;
      opts.endpoint = otel::NormalizeGrpcEndpoint(endpoint);
      opts.use_ssl_credentials = use_ssl;
      opts.timeout = std::chrono::seconds(10);
      const std::string auth = otel::AuthorizationHeader();
      if (!auth.empty()) {
        opts.metadata.emplace("authorization", auth);
      }

      auto exporter = std::unique_ptr<logs_sdk::LogRecordExporter>(
          otlp::OtlpGrpcLogRecordExporterFactory::Create(opts));

      // Configure batch processor
      logs_sdk::BatchLogRecordProcessorOptions processor_opts;
      processor_opts.max_queue_size = 2048;
      processor_opts.schedule_delay_millis = std::chrono::milliseconds(5000);
      processor_opts.max_export_batch_size = 512;

      auto processor = std::unique_ptr<logs_sdk::LogRecordProcessor>(
          new logs_sdk::BatchLogRecordProcessor(std::move(exporter), processor_opts));

      std::string service_version = kLibraryVersionStr;
      if (service_version.empty() || service_version == "unknown") {
        service_version = "1.0.0";
      }
      const std::string service_name = otel::ServiceName(kDefaultServiceName);

      // Create resource attributes
      auto resource_attributes = resource::ResourceAttributes{
          {resource::SemanticConventions::kServiceName, service_name},
          {resource::SemanticConventions::kServiceVersion, service_version},
          {"environment", environment}};
      auto resource_ptr = resource::Resource::Create(resource_attributes);

      // Create logger provider
      auto provider = std::shared_ptr<logs_sdk::LoggerProvider>(
          new logs_sdk::LoggerProvider(std::move(processor), resource_ptr));

      // Set global logger provider
      logs_api::Provider::SetLoggerProvider(provider);

      // Get logger instance
      logger_ = provider->GetLogger(service_name, service_name, service_version);

      spdlog::info("OpenTelemetry logs sink initialized (gRPC endpoint: {})", opts.endpoint);
      initialized_ = true;
    } catch (const std::exception& e) {
      spdlog::error("Failed to initialize OpenTelemetry logs sink: {}", e.what());
      initialized_ = false;
    }
  }

  ~OtelLogSinkImpl() {
    if (initialized_) {
      try {
        auto provider = logs_api::Provider::GetLoggerProvider();
        if (auto sdk_provider = std::dynamic_pointer_cast<logs_sdk::LoggerProvider>(provider)) {
          sdk_provider->Shutdown();
        }
      } catch (const std::exception& e) {
        // Ignore shutdown errors
      }
    }
  }

  void EmitLog(const spdlog::details::log_msg& msg) {
    if (!initialized_ || !logger_) {
      return;
    }

    try {
      // Map spdlog level to OpenTelemetry severity
      logs_api::Severity severity;
      switch (msg.level) {
        case spdlog::level::trace:
        case spdlog::level::debug:
          severity = logs_api::Severity::kDebug;
          break;
        case spdlog::level::info:
          severity = logs_api::Severity::kInfo;
          break;
        case spdlog::level::warn:
          severity = logs_api::Severity::kWarn;
          break;
        case spdlog::level::err:
        case spdlog::level::critical:
        case spdlog::level::off:
          severity = logs_api::Severity::kError;
          break;
        default:
          severity = logs_api::Severity::kInfo;
      }

      // Get message string
      std::string message(msg.payload.data(), msg.payload.size());
      std::string severity_text = spdlog::level::to_string_view(msg.level).data();

      // Emit log using Logger API (simpler approach)
      logger_->Log(severity, message);
    } catch (const std::exception& e) {
      // Silently ignore errors to not break logging
    }
  }

  bool IsInitialized() const { return initialized_; }

private:
  std::shared_ptr<logs_api::Logger> logger_;
  bool initialized_ = false;
};

OtelLogSink::OtelLogSink(const std::string& endpoint, const std::string& environment)
    : pimpl_(std::make_unique<OtelLogSinkImpl>(endpoint, environment)) {}

OtelLogSink::~OtelLogSink() = default;

void OtelLogSink::sink_it_(const spdlog::details::log_msg& msg) {
  if (pimpl_) {
    pimpl_->EmitLog(msg);
  }
}

void OtelLogSink::flush_() {
  // OpenTelemetry handles flushing automatically via batch processor
}

}  // namespace jrb::core
