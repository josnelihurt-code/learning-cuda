#include "src/cpp_accelerator/adapters/webrtc/sdp/sdp_utils.h"

#include <cstdint>
#include <cstdlib>
#include <string>

#include <gtest/gtest.h>
#include <rtc/rtc.hpp>

using namespace jrb::adapters::webrtc::sdp;

namespace {

rtc::Description MakeAnswerWithUdpHostCandidate(const std::string& candidate_line) {
  const std::string sdp =
      "v=0\r\n"
      "o=- 0 0 IN IP4 127.0.0.1\r\n"
      "s=-\r\n"
      "t=0 0\r\n"
      "m=video 9 UDP/TLS/RTPSAVPF 96\r\n"
      "a=" + candidate_line + "\r\n";
  rtc::Description description(sdp, rtc::Description::Type::Answer);
  description.addCandidate(rtc::Candidate(candidate_line));
  return description;
}

TEST(SdpUtilsTest, Success_LocalUdpHostPort) {
  auto description = MakeAnswerWithUdpHostCandidate(
      "candidate:1 1 UDP 2130706431 192.168.10.213 10042 typ host");
  const auto port = LocalUdpHostPort(description);
  ASSERT_TRUE(port.has_value());
  EXPECT_EQ(*port, 10042);
}

TEST(SdpUtilsTest, Edge_NoUdpHostCandidate) {
  rtc::Description description(
      "v=0\r\n"
      "o=- 0 0 IN IP4 127.0.0.1\r\n"
      "s=-\r\n"
      "t=0 0\r\n"
      "m=video 9 UDP/TLS/RTPSAVPF 96\r\n",
      rtc::Description::Type::Answer);
  EXPECT_FALSE(LocalUdpHostPort(description).has_value());
}

TEST(SdpUtilsTest, Success_BuildPublicCandidateSdp) {
  const std::string sdp = BuildPublicCandidateSdp("sess", "98.248.157.222", 10042);
  EXPECT_NE(sdp.find("a=candidate:1 1 UDP 2130706431 98.248.157.222 10042 typ host"),
            std::string::npos);
  EXPECT_NE(sdp.find("a=candidate:2 1 TCP 2130706430 98.248.157.222 60060 typ host tcptype passive"),
            std::string::npos);
}

TEST(SdpUtilsTest, Edge_EmptyPublicIpSkipsCandidates) {
  EXPECT_TRUE(BuildPublicCandidateSdp("sess", "", 10042).empty());
}

TEST(SdpUtilsTest, Success_PortEnvOverride) {
  ::setenv("WEBRTC_PUBLIC_PORT", "20000", 1);
  ::unsetenv("WEBRTC_PUBLIC_TCP_PORT");
  const std::string sdp = BuildPublicCandidateSdp("sess", "98.248.157.222", 10042);
  ::unsetenv("WEBRTC_PUBLIC_PORT");
  EXPECT_NE(sdp.find("98.248.157.222 20000 typ host"), std::string::npos);
}

TEST(SdpUtilsTest, Success_InjectPublicCandidateBeforeSecondMediaSection) {
  auto description = MakeAnswerWithUdpHostCandidate(
      "candidate:1 1 UDP 2130706431 192.168.10.213 10042 typ host");
  std::string sdp =
      "v=0\r\n"
      "m=audio 9 UDP/TLS/RTPSAVPF 111\r\n"
      "a=rtpmap:111 opus/48000/2\r\n"
      "m=video 9 UDP/TLS/RTPSAVPF 96\r\n"
      "a=rtpmap:96 H264/90000\r\n";
  InjectPublicCandidate("sess", "98.248.157.222", description, &sdp);
  const size_t audio_pos = sdp.find("m=audio");
  const size_t injected_pos = sdp.find("a=candidate:1 1 UDP 2130706431 98.248.157.222 10042");
  const size_t video_pos = sdp.find("m=video");
  ASSERT_NE(injected_pos, std::string::npos);
  EXPECT_LT(audio_pos, injected_pos);
  EXPECT_LT(injected_pos, video_pos);
}

TEST(SdpUtilsTest, Edge_InjectPublicCandidateNoOp) {
  auto description = MakeAnswerWithUdpHostCandidate(
      "candidate:1 1 UDP 2130706431 192.168.10.213 10042 typ host");
  const std::string original =
      "v=0\r\n"
      "m=video 9 UDP/TLS/RTPSAVPF 96\r\n";
  std::string sdp = original;
  InjectPublicCandidate("sess", "", description, &sdp);
  EXPECT_EQ(sdp, original);

  rtc::Description without_candidate(
      "v=0\r\n"
      "m=video 9 UDP/TLS/RTPSAVPF 96\r\n",
      rtc::Description::Type::Answer);
  InjectPublicCandidate("sess", "98.248.157.222", without_candidate, &sdp);
  EXPECT_EQ(sdp, original);
}

}  // namespace
