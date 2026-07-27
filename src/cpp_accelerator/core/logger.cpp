#include "src/cpp_accelerator/core/logger.h"

#include <cctype>
#include <cstdarg>
#include <cstddef>
#include <cstdlib>
#include <iostream>
#include <string>
#include <vector>

#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wdeprecated-declarations"
#include <spdlog/sinks/rotating_file_sink.h>
#include <spdlog/sinks/stdout_color_sinks.h>
#include <spdlog/spdlog.h>
#pragma GCC diagnostic pop

extern "C" {
#include <libavcodec/avcodec.h>
#include <libavutil/log.h>
}

#include "src/cpp_accelerator/core/otel_log_sink.h"

namespace jrb::core {

constexpr const char* kLoggerBuildTimestamp = __DATE__ " " __TIME__;

namespace {

constexpr const char* kLogFile = "/tmp/cppaccelerator.log";
constexpr const char* kFilePattern =
    "{\"timestamp\":%E,\"level\":\"%l\",\"message\":\"%v\",\"source\":\"cpp\"}\n";
constexpr const char* kConsolePattern = "[%Y-%m-%d %H:%M:%S.%e] [%^%l%$] %v";
constexpr std::size_t kLogMaxFileBytes = 64UL * 1024UL * 1024UL;
constexpr std::size_t kLogMaxFiles = 3;

spdlog::level::level_enum ParseLogLevel(const char* s) {
  if (s == nullptr || *s == '\0') {
    return spdlog::level::info;
  }
  std::string v(s);
  for (char& c : v) {
    c = static_cast<char>(std::tolower(static_cast<unsigned char>(c)));
  }
  if (v == "trace") return spdlog::level::trace;
  if (v == "debug") return spdlog::level::debug;
  if (v == "info") return spdlog::level::info;
  if (v == "warn" || v == "warning") return spdlog::level::warn;
  if (v == "error" || v == "err") return spdlog::level::err;
  if (v == "critical" || v == "fatal") return spdlog::level::critical;
  if (v == "off") return spdlog::level::off;
  return spdlog::level::info;
}

spdlog::sink_ptr MakeFileSink() {
  // Rotating, not basic: the sink flushes on every record, so a per-frame error
  // loop (e.g. a camera stream failing to decode) used to grow this file without
  // bound until the device filled up and every logging thread stalled.
  auto sink = std::make_shared<spdlog::sinks::rotating_file_sink_mt>(kLogFile, kLogMaxFileBytes,
                                                                     kLogMaxFiles, true);
  sink->set_pattern(kFilePattern);
  return sink;
}

spdlog::sink_ptr MakeConsoleSink() {
  auto sink = std::make_shared<spdlog::sinks::stdout_color_sink_mt>();
  sink->set_pattern(kConsolePattern);
  return sink;
}

void BuildLoggerFromSinks(const std::vector<spdlog::sink_ptr>& sinks) {
  auto logger = std::make_shared<spdlog::logger>("cpp_accelerator", sinks.begin(), sinks.end());

  const char* level_env = std::getenv("CUDA_ACCELERATOR_LOG_LEVEL");
  if (level_env == nullptr || *level_env == '\0') {
    level_env = std::getenv("SPDLOG_LEVEL");
  }
  const spdlog::level::level_enum log_level = ParseLogLevel(level_env);
  logger->set_level(log_level);
  logger->flush_on(log_level);

  spdlog::set_default_logger(logger);
  spdlog::set_level(log_level);
}

}  // namespace

void initialize_logger(const std::string& otlp_endpoint, bool remote_enabled,
                       const std::string& environment) {
  std::string endpoint = otlp_endpoint;
  bool enabled = remote_enabled;
  std::string env = environment;

  const char* env_endpoint = std::getenv("OTEL_LOGS_ENDPOINT");
  const char* env_enabled = std::getenv("OTEL_LOGS_ENABLED");
  const char* env_environment = std::getenv("OTEL_ENVIRONMENT");

  if (env_endpoint && env_endpoint[0] != '\0') {
    endpoint = env_endpoint;
  }
  if (env_enabled) {
    enabled = (std::string(env_enabled) == "true" || std::string(env_enabled) == "1");
  }
  if (env_environment && env_environment[0] != '\0') {
    env = env_environment;
  }

  try {
    std::vector<spdlog::sink_ptr> sinks;
    sinks.push_back(MakeFileSink());
    sinks.push_back(MakeConsoleSink());

    BuildLoggerFromSinks(sinks);

    spdlog::info("Logger initialized [BUILD: {}]", kLoggerBuildTimestamp);

    if (enabled && !endpoint.empty()) {
      try {
        spdlog::default_logger()->sinks().push_back(
          std::make_shared<OtelLogSink>(endpoint, env));
        spdlog::info("OpenTelemetry log sink added (endpoint: {})", endpoint);
      } catch (const std::exception& otel_ex) {
        spdlog::warn("Failed to create OpenTelemetry sink: {}", otel_ex.what());
      }
    } else if (enabled) {
      spdlog::warn("OpenTelemetry logging enabled but endpoint not configured");
    }

  } catch (const spdlog::spdlog_ex&) {
    try {
      std::vector<spdlog::sink_ptr> fallback_sinks;
      fallback_sinks.push_back(MakeConsoleSink());
      BuildLoggerFromSinks(fallback_sinks);
      spdlog::error("File logger unavailable, using console only");
    } catch (const std::exception& fallback_ex) {
      std::cerr << "FATAL: Logger initialization failed: " << fallback_ex.what() << std::endl;
    }
  } catch (const std::exception& ex) {
    std::cerr << "FATAL: Logger initialization failed: " << ex.what() << std::endl;
  }
}

void configure_ffmpeg_logging() {
  static bool configured = false;
  if (configured) return;

  av_log_set_callback([](void* /*ptr*/, int level, const char* fmt, va_list vl) {
    if (level > av_log_get_level()) return;

    if (fmt) {
      std::string fmt_str = fmt;
      if (fmt_str.find("decode_slice_header") != std::string::npos ||
          fmt_str.find("non-existing PPS") != std::string::npos ||
          fmt_str.find("non-existing SPS") != std::string::npos) {
        return;
      }
    }

    char buf[1024];
    vsnprintf(buf, sizeof(buf), fmt, vl);
    std::string msg(buf);
    while (!msg.empty() && (msg.back() == '\n' || msg.back() == '\r')) {
      msg.pop_back();
    }
    spdlog::warn("[ffmpeg] {}", msg);
  });
  av_log_set_level(AV_LOG_WARNING);
  configured = true;
}

}  // namespace jrb::core
