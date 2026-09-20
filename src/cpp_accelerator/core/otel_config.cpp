#include "src/cpp_accelerator/core/otel_config.h"

#include <cstdint>
#include <cstdlib>
#include <string>

namespace jrb::core::otel {

namespace {

std::string EnvValue(const char* name) {
  const char* value = std::getenv(name);
  if (value == nullptr) {
    return {};
  }
  return std::string(value);
}

const char kBase64Alphabet[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

std::string Base64Encode(const std::string& input) {
  std::string out;
  out.reserve(((input.size() + 2) / 3) * 4);
  size_t i = 0;
  while (i + 2 < input.size()) {
    const uint32_t triplet = (static_cast<uint8_t>(input[i]) << 16) |
                             (static_cast<uint8_t>(input[i + 1]) << 8) |
                             static_cast<uint8_t>(input[i + 2]);
    out.push_back(kBase64Alphabet[(triplet >> 18) & 0x3F]);
    out.push_back(kBase64Alphabet[(triplet >> 12) & 0x3F]);
    out.push_back(kBase64Alphabet[(triplet >> 6) & 0x3F]);
    out.push_back(kBase64Alphabet[triplet & 0x3F]);
    i += 3;
  }
  const size_t remaining = input.size() - i;
  if (remaining == 1) {
    const uint32_t triplet = static_cast<uint8_t>(input[i]) << 16;
    out.push_back(kBase64Alphabet[(triplet >> 18) & 0x3F]);
    out.push_back(kBase64Alphabet[(triplet >> 12) & 0x3F]);
    out.push_back('=');
    out.push_back('=');
  } else if (remaining == 2) {
    const uint32_t triplet = (static_cast<uint8_t>(input[i]) << 16) |
                             (static_cast<uint8_t>(input[i + 1]) << 8);
    out.push_back(kBase64Alphabet[(triplet >> 18) & 0x3F]);
    out.push_back(kBase64Alphabet[(triplet >> 12) & 0x3F]);
    out.push_back(kBase64Alphabet[(triplet >> 6) & 0x3F]);
    out.push_back('=');
  }
  return out;
}

}  // namespace

std::string AuthorizationHeader() {
  const std::string header = EnvValue("OTEL_AUTH_HEADER");
  if (!header.empty()) {
    return header;
  }
  const std::string token = EnvValue("OTEL_EXPORTER_OTLP_TOKEN");
  if (token.empty()) {
    return {};
  }
  return "Basic " + Base64Encode(EnvValue("OTEL_AUTH_INSTANCE_ID") + ":" + token);
}

std::string ServiceName(const std::string& fallback) {
  std::string name = EnvValue("OTEL_SERVICE_NAME");
  if (name.empty()) {
    name = fallback;
  }
  return name;
}

bool EnvFlagEnabled(const char* name) {
  const std::string value = EnvValue(name);
  return value == "true" || value == "1";
}

std::string NormalizeGrpcEndpoint(const std::string& endpoint) {
  std::string host = endpoint;
  bool insecure = false;
  if (host.rfind("https://", 0) == 0) {
    host = host.substr(8);
  } else if (host.rfind("http://", 0) == 0) {
    host = host.substr(7);
    insecure = true;
  }
  const size_t slash = host.find('/');
  if (slash != std::string::npos) {
    host = host.substr(0, slash);
  }
  if (host.find(':') == std::string::npos) {
    host += insecure ? ":80" : ":443";
  }
  return host;
}

}  // namespace jrb::core::otel
