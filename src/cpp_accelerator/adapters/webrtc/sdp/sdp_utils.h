#pragma once

#include <cstdint>
#include <future>
#include <optional>
#include <string>

#include <rtc/rtc.hpp>

namespace jrb::adapters::webrtc::sdp {

struct OutboundVideoConfig {
  std::string mid;
  int payload_type;
};

std::string NormalizeCodecName(const std::string& value);
std::string StripRtpHeaderExtensions(const std::string& sdp);
uint32_t MakeSsrc(const std::string& session_id);
std::optional<OutboundVideoConfig> FindOutboundVideoConfig(const rtc::Description& offer);

// Builds "a=candidate:..." lines for the accelerator's public address to inject
// into the SDP answer, or empty when public_ip is empty. udp_port should be the
// port this session actually bound; WEBRTC_PUBLIC_PORT overrides it for
// single-port forwards. TCP fallback port: WEBRTC_PUBLIC_TCP_PORT (default 60060).
std::string BuildPublicCandidateSdp(const std::string& session_id, const std::string& public_ip,
                                    uint16_t udp_port);

// UDP host port this session bound, from the local description's candidates.
std::optional<uint16_t> LocalUdpHostPort(const rtc::Description& description);

// Waits up to 10s for the SDP answer via future or pc.localDescription(). Returns true on success.
bool WaitForSdpAnswer(const std::string& session_id,
                      std::shared_future<std::string> answer_future,
                      rtc::PeerConnection& pc,
                      std::string* sdp_answer_str);

}  // namespace jrb::adapters::webrtc::sdp
