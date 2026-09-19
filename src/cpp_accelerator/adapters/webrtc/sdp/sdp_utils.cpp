#include "src/cpp_accelerator/adapters/webrtc/sdp/sdp_utils.h"

#include <algorithm>
#include <cctype>
#include <cstdlib>
#include <exception>
#include <future>
#include <sstream>
#include <string>
#include <thread>
#include <variant>

#include <spdlog/spdlog.h>

namespace jrb::adapters::webrtc::sdp {

std::string NormalizeCodecName(const std::string& value) {
  std::string normalized = value;
  std::transform(normalized.begin(), normalized.end(), normalized.begin(),
                 [](unsigned char c) { return static_cast<char>(std::toupper(c)); });
  return normalized;
}

// Strips a=extmap lines from SDP to prevent malformed RTP header issues.
// Browsers include RTP header extensions (transport-cc, abs-send-time, etc.)
// that libdatachannel's RTP parser cannot handle, causing every incoming frame
// to be marked as malformed and dropped. Removing extmap from the offer
// causes the browser to omit header extensions from its RTP packets.
std::string StripRtpHeaderExtensions(const std::string& sdp) {
  std::string result;
  result.reserve(sdp.size());
  std::istringstream stream(sdp);
  std::string line;
  while (std::getline(stream, line)) {
    const std::string_view view(line);
    if (view.find("a=extmap:") != std::string_view::npos ||
        view.find("a=extmap-allow-mixed") != std::string_view::npos) {
      continue;
    }
    result += line;
    result += "\r\n";
  }
  return result;
}

uint32_t MakeSsrc(const std::string& session_id) {
  const uint32_t hash = static_cast<uint32_t>(std::hash<std::string>{}(session_id));
  return hash == 0 ? 1U : hash;
}

std::optional<OutboundVideoConfig> FindOutboundVideoConfig(const rtc::Description& offer) {
  for (int index = 0; index < offer.mediaCount(); ++index) {
    const auto entry = offer.media(index);
    if (!std::holds_alternative<const rtc::Description::Media*>(entry)) continue;
    const auto* media = std::get<const rtc::Description::Media*>(entry);
    if (media == nullptr || media->type() != "video" ||
        media->direction() != rtc::Description::Direction::RecvOnly) {
      continue;
    }
    for (const int payload_type : media->payloadTypes()) {
      const auto* rtp_map = media->rtpMap(payload_type);
      if (rtp_map == nullptr) continue;
      if (NormalizeCodecName(rtp_map->format) == "H264") {
        return OutboundVideoConfig{media->mid(), payload_type};
      }
    }
  }
  return std::nullopt;
}

std::optional<uint16_t> LocalUdpHostPort(const rtc::Description& description) {
  for (rtc::Candidate candidate : description.candidates()) {
    if (candidate.type() != rtc::Candidate::Type::Host ||
        candidate.transportType() != rtc::Candidate::TransportType::Udp) {
      continue;
    }
    // Candidates parsed from SDP strings are unresolved until asked to.
    if (!candidate.port()) {
      candidate.resolve(rtc::Candidate::ResolveMode::Simple);
    }
    if (candidate.port()) {
      return candidate.port();
    }
  }
  return std::nullopt;
}

std::string BuildPublicCandidateSdp(const std::string& session_id, const std::string& public_ip,
                                    uint16_t udp_port) {
  if (public_ip.empty()) {
    spdlog::debug("[WebRTC:{}] no observed public IP — skipping public ICE candidate", session_id);
    return {};
  }

  uint16_t udp_public_port = udp_port;
  if (const char* override_port = std::getenv("WEBRTC_PUBLIC_PORT")) {
    udp_public_port = static_cast<uint16_t>(std::stoi(override_port));
  }
  uint16_t tcp_public_port = 60060;
  if (const char* override_tcp = std::getenv("WEBRTC_PUBLIC_TCP_PORT")) {
    tcp_public_port = static_cast<uint16_t>(std::stoi(override_tcp));
  }

  try {
    std::ostringstream candidate_sdp;
    candidate_sdp << "a=candidate:1 1 UDP 2130706431 " << public_ip << " " << udp_public_port
                  << " typ host\r\n";
    candidate_sdp << "a=candidate:2 1 TCP 2130706430 " << public_ip << " " << tcp_public_port
                  << " typ host tcptype passive\r\n";
    spdlog::info("[WebRTC:{}] Will inject public ICE candidates: {} udp:{}/tcp:{} (firewall fallback)",
                 session_id, public_ip, udp_public_port, tcp_public_port);
    return candidate_sdp.str();
  } catch (const std::exception& e) {
    spdlog::warn("[WebRTC:{}] Failed to prepare public ICE candidate: {}", session_id, e.what());
    return {};
  }
}

bool WaitForSdpAnswer(const std::string& session_id,
                      std::shared_future<std::string> answer_future,
                      rtc::PeerConnection& pc,
                      std::string* sdp_answer_str) {
  const auto timeout = std::chrono::seconds(10);
  const auto start = std::chrono::steady_clock::now();

  if (pc.localDescription().has_value()) {
    auto local_desc = pc.localDescription().value();
    if (local_desc.type() == rtc::Description::Type::Answer) {
      std::string sdp = local_desc.generateSdp();
      if (sdp_answer_str != nullptr && sdp_answer_str->empty()) {
        *sdp_answer_str = sdp;
        spdlog::debug("[WebRTC:{}] SDP answer available immediately (length: {})", session_id,
                     sdp.length());
        return true;
      }
    }
  }

  spdlog::debug("[WebRTC:{}] Waiting for SDP answer (timeout: {}s)", session_id, timeout.count());
  while ((std::chrono::steady_clock::now() - start) < timeout) {
    const auto status = answer_future.wait_for(std::chrono::milliseconds(100));
    if (status == std::future_status::ready) {
      try {
        const std::string answer = answer_future.get();
        if (sdp_answer_str != nullptr && sdp_answer_str->empty()) {
          *sdp_answer_str = answer;
        }
        spdlog::debug("[WebRTC:{}] SDP answer received via callback (length: {})", session_id,
                     answer.length());
        return true;
      } catch (const std::exception& e) {
        spdlog::error("[WebRTC:{}] Error getting answer from future: {}", session_id, e.what());
      }
    }

    if (pc.localDescription().has_value()) {
      auto local_desc = pc.localDescription().value();
      if (local_desc.type() == rtc::Description::Type::Answer) {
        std::string sdp = local_desc.generateSdp();
        if (sdp_answer_str != nullptr && sdp_answer_str->empty()) {
          *sdp_answer_str = sdp;
          spdlog::debug("[WebRTC:{}] Retrieved SDP answer directly (length: {})", session_id,
                       sdp.length());
          return true;
        }
      }
    }

    std::this_thread::sleep_for(std::chrono::milliseconds(50));
  }

  spdlog::error("[WebRTC:{}] Timeout waiting for SDP answer ({}s)", session_id, timeout.count());
  return false;
}

}  // namespace jrb::adapters::webrtc::sdp
