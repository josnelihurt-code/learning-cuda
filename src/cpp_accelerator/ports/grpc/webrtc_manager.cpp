#include "src/cpp_accelerator/ports/grpc/webrtc_manager.h"

#include <cctype>
#include <chrono>
#include <cstring>
#include <exception>
#include <future>
#include <memory>
#include <mutex>
#include <optional>
#include <random>
#include <sstream>
#include <string>
#include <string_view>
#include <thread>
#include <variant>
#include <vector>

#include <spdlog/spdlog.h>
#include <rtc/rtc.hpp>

#include "proto/_virtual_imports/image_processor_service_proto/image_processor_service.pb.h"
#include "src/cpp_accelerator/ports/shared_lib/processor_engine.h"

namespace jrb::ports::grpc_service {

namespace {

constexpr std::string_view kGoVideoSessionPrefix = "go-video-";
constexpr uint32_t kProcessedVideoBitrate = 2'500'000;
constexpr const char* kProcessedVideoTrackLabel = "processed-video";
constexpr const char* kStatsChannelLabel = "cpp-processor-stats";

bool IsGoVideoSession(const std::string& value) {
  return value.rfind(kGoVideoSessionPrefix, 0) == 0;
}

bool ShouldRegisterSessionChannel(const std::string& session_id, const std::string& label) {
  return !IsGoVideoSession(session_id) && !IsGoVideoSession(label);
}

// Sends a logical message over the data channel as one or more framed chunks.
// Short-circuits and logs on the first send failure.
void SendFramed(rtc::DataChannel& dc,
                const std::string& payload,
                uint32_t message_id) {
  const auto span = std::span<const std::byte>(
      reinterpret_cast<const std::byte*>(payload.data()), payload.size());
  auto chunks = PackMessage(message_id, span);
  for (auto& chunk : chunks) {
    if (!dc.send(std::move(chunk))) {
      spdlog::error("[framing] Failed to send chunk for message_id={}", message_id);
      return;
    }
  }
}

void CopyProcessMetadata(const cuda_learning::ProcessImageRequest& request,
                         cuda_learning::ProcessImageResponse* response) {
  if (response == nullptr) {
    return;
  }

  response->set_api_version(request.api_version());
  response->mutable_trace_context()->CopyFrom(request.trace_context());
}

int64_t CurrentUnixTimeMs() {
  return std::chrono::duration_cast<std::chrono::milliseconds>(
             std::chrono::system_clock::now().time_since_epoch())
      .count();
}

std::string NormalizeCodecName(const std::string& value) {
  std::string normalized = value;
  std::transform(
      normalized.begin(), normalized.end(), normalized.begin(),
      [](unsigned char character) { return static_cast<char>(std::toupper(character)); });
  return normalized;
}

struct OutboundVideoConfig {
  std::string mid;
  int payload_type;
};

std::optional<OutboundVideoConfig> FindOutboundVideoConfig(const rtc::Description& offer) {
  for (int index = 0; index < offer.mediaCount(); ++index) {
    const auto entry = offer.media(index);
    if (!std::holds_alternative<const rtc::Description::Media*>(entry)) {
      continue;
    }

    const auto* media = std::get<const rtc::Description::Media*>(entry);
    if (media == nullptr || media->type() != "video" ||
        media->direction() != rtc::Description::Direction::RecvOnly) {
      continue;
    }

    for (const int payload_type : media->payloadTypes()) {
      const auto* rtp_map = media->rtpMap(payload_type);
      if (rtp_map == nullptr) {
        continue;
      }
      if (NormalizeCodecName(rtp_map->format) == "H264") {
        return OutboundVideoConfig{media->mid(), payload_type};
      }
    }
  }

  return std::nullopt;
}

uint32_t MakeSsrc(const std::string& session_id) {
  const uint32_t hash = static_cast<uint32_t>(std::hash<std::string>{}(session_id));
  return hash == 0 ? 1U : hash;
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

}  // namespace

WebRTCManager::WebRTCManager(std::shared_ptr<jrb::ports::shared_lib::ProcessorEngine> engine)
    : engine_(std::move(engine)), initialized_(false), cleanup_running_(false) {
  rtc::InitLogger(rtc::LogLevel::Info);
}

WebRTCManager::~WebRTCManager() {
  if (initialized_) {
    Shutdown();
  }
}

bool WebRTCManager::Initialize() {
  if (initialized_) {
    spdlog::warn("WebRTCManager already initialized");
    return true;
  }

  try {
    spdlog::info("Initializing WebRTCManager with libdatachannel");
    config_ = std::make_unique<rtc::Configuration>();
    config_->iceServers.emplace_back("stun:stun.l.google.com:19302");

    config_->portRangeBegin = 10000;
    config_->portRangeEnd = 10199;

    // Enable TCP ICE candidates for firewall fallback (requires specific ports)
    config_->enableIceTcp = true;

    // The library default advertises ~256 KiB as the SCTP max message size.
    // Our ProcessImageRequest payloads for full-resolution frames (e.g. 512x512
    // RGB = ~768 KiB, plus protobuf overhead) routinely exceed that, and Chrome
    // aborts the send with OperationError: data-channel-failure, tearing the
    // channel down. Raise the advertised limit so peers can safely transmit
    // single-message frames up to this cap before we decide to chunk.
    constexpr size_t kMaxDataChannelMessageBytes = 16 * 1024 * 1024;  // 16 MiB
    config_->maxMessageSize = kMaxDataChannelMessageBytes;

    spdlog::info("WebRTC Configuration:");
    spdlog::info("  - STUN Server: stun.l.google.com:19302");
    spdlog::info("  - UDP Port Range: {}-{} ({} ports)", config_->portRangeBegin,
                 config_->portRangeEnd, config_->portRangeEnd - config_->portRangeBegin + 1);
    spdlog::info("  - TCP ICE: enabled for firewall fallback");
    spdlog::info("  - Data channel max message size: {} bytes", kMaxDataChannelMessageBytes);
    spdlog::info("  - TURN Server: Not configured");
    spdlog::info("  - Session Cleanup: Enabled (30s timeout)");

    cleanup_running_ = true;
    cleanup_thread_ = std::thread([this]() {
      while (cleanup_running_) {
        std::this_thread::sleep_for(std::chrono::seconds(10));
        if (cleanup_running_) {
          CleanupInactiveSessions(30);
        }
      }
    });

    initialized_ = true;
    spdlog::info("WebRTCManager initialized successfully");
    return true;
  } catch (const std::exception& e) {
    spdlog::error("Failed to initialize WebRTCManager: {}", e.what());
    initialized_ = false;
    return false;
  }
}

void WebRTCManager::Shutdown() {
  if (!initialized_) {
    return;
  }

  spdlog::info("Shutting down WebRTCManager...");

  cleanup_running_ = false;
  if (cleanup_thread_.joinable()) {
    cleanup_thread_.join();
  }

  std::lock_guard<std::mutex> lock(sessions_mutex_);
  for (auto& [session_id, session] : sessions_) {
    try {
      std::lock_guard<std::mutex> session_lock(session->mutex);
      if (session->data_channel) {
        session->data_channel->close();
      }
      if (session->stats_channel) {
        session->stats_channel->close();
      }
      if (session->peer_connection) {
        session->peer_connection->close();
      }
    } catch (const std::exception& e) {
      spdlog::error("[WebRTC:{}] Error closing session during shutdown: {}", session_id, e.what());
    }
  }
  sessions_.clear();
  {
    std::lock_guard<std::mutex> channels_lock(session_channels_mutex_);
    session_channels_.clear();
  }
  config_.reset();
  initialized_ = false;
  spdlog::info("WebRTCManager shut down");
}

bool WebRTCManager::CreateSession(const std::string& session_id, const std::string& sdp_offer_str,
                                  std::string* sdp_answer_str, std::string* error_message) {
  if (!initialized_) {
    if (error_message != nullptr) {
      *error_message = "WebRTC manager not initialized";
    }
    return false;
  }

  if (session_id.empty() || sdp_offer_str.empty()) {
    if (error_message != nullptr) {
      *error_message = "session_id and sdp_offer are required";
    }
    return false;
  }

  std::lock_guard<std::mutex> lock(sessions_mutex_);
  if (sessions_.count(session_id)) {
    if (error_message != nullptr) {
      *error_message = "Session with ID " + session_id + " already exists";
    }
    return false;
  }

  try {
    auto session = std::make_shared<SessionState>();
    session->created_at = std::chrono::steady_clock::now();
    session->last_heartbeat = std::chrono::steady_clock::now();
    session->peer_connection = std::make_shared<rtc::PeerConnection>(*config_);
    session->live_video_processor = std::make_unique<LiveVideoProcessor>(
        engine_.get(), session->memory_pool.get());
    session->live_filter_state.set_accelerator(cuda_learning::ACCELERATOR_TYPE_CUDA);
    session->live_filter_state.add_filters(cuda_learning::FILTER_TYPE_NONE);
    session->live_filter_state.set_api_version("1.1");
    session->memory_pool = std::make_unique<jrb::infrastructure::cuda::CudaMemoryPool>();
    engine_->SetMemoryPool(session->memory_pool.get());
    spdlog::info("[WebRTC:{}] Created dedicated CUDA memory pool for session", session_id);

    // Prepare manual ICE candidate data for SDP modification if configured (before callbacks)
    // This allows clients behind NAT/firewall to connect using the Jetson's public endpoint
    const char* public_ip_env = std::getenv("WEBRTC_PUBLIC_IP");
    const char* public_port_env = std::getenv("WEBRTC_PUBLIC_PORT");
    const char* public_tcp_port_env = std::getenv("WEBRTC_PUBLIC_TCP_PORT");
    std::string manual_candidate_sdp;
    if (public_ip_env != nullptr && public_port_env != nullptr) {
      try {
        const std::string public_ip = public_ip_env;
        const std::string public_port = public_port_env;

        // SDP candidate format: candidate:foundation component-id transport priority connection-address port typ candidate-type
        // Example: candidate:1 1 UDP 2130706431 73.71.7.90 60062 typ host
        std::ostringstream candidate_sdp;
        candidate_sdp << "a=candidate:1 1 UDP 2130706431 " << public_ip << " " << public_port
                      << " typ host\r\n";

        // Add TCP fallback candidate if configured (for clients behind firewalls that block UDP)
        if (public_tcp_port_env != nullptr) {
          const std::string public_tcp_port = public_tcp_port_env;
          candidate_sdp << "a=candidate:2 1 TCP 2130706430 " << public_ip << " " << public_tcp_port
                        << " typ host tcptype passive\r\n";
          spdlog::info("[WebRTC:{}] Will inject TCP ICE candidate for firewall fallback: {}:{}",
                       session_id, public_ip, public_tcp_port);
        }

        manual_candidate_sdp = candidate_sdp.str();

        spdlog::info("[WebRTC:{}] Will inject manual ICE candidate in SDP: {}:{}",
                     session_id, public_ip, public_port);
      } catch (const std::exception& e) {
        spdlog::warn("[WebRTC:{}] Failed to prepare manual ICE candidate: {}", session_id, e.what());
      }
    } else {
      spdlog::debug("[WebRTC:{}] WEBRTC_PUBLIC_IP or WEBRTC_PUBLIC_PORT not set, skipping manual ICE candidate",
                    session_id);
    }

    session->peer_connection->onStateChange(
        [session_id, session](rtc::PeerConnection::State state) {
          std::string state_str;
          switch (state) {
            case rtc::PeerConnection::State::New:
              state_str = "New";
              break;
            case rtc::PeerConnection::State::Connecting:
              state_str = "Connecting";
              break;
            case rtc::PeerConnection::State::Connected:
              state_str = "Connected";
              break;
            case rtc::PeerConnection::State::Disconnected:
              state_str = "Disconnected";
              spdlog::warn("[WebRTC:{}] Peer connection disconnected", session_id);
              break;
            case rtc::PeerConnection::State::Failed:
              state_str = "Failed";
              spdlog::error("[WebRTC:{}] Peer connection failed", session_id);
              break;
            case rtc::PeerConnection::State::Closed:
              state_str = "Closed";
              spdlog::info("[WebRTC:{}] Peer connection closed", session_id);
              break;
            default:
              state_str = "Unknown";
              break;
          }
          spdlog::info("[WebRTC:{}] Peer connection state changed: {} ({})", session_id, state_str,
                       static_cast<int>(state));
        });

    session->peer_connection->onGatheringStateChange(
        [session_id](rtc::PeerConnection::GatheringState state) {
          std::string state_str;
          switch (state) {
            case rtc::PeerConnection::GatheringState::New:
              state_str = "New";
              break;
            case rtc::PeerConnection::GatheringState::InProgress:
              state_str = "InProgress";
              break;
            case rtc::PeerConnection::GatheringState::Complete:
              state_str = "Complete";
              break;
            default:
              state_str = "Unknown";
              break;
          }
          spdlog::info("[WebRTC:{}] ICE gathering state changed: {} ({})", session_id, state_str,
                       static_cast<int>(state));
        });

    session->peer_connection->onIceStateChange(
        [session_id, session](rtc::PeerConnection::IceState state) {
          std::string state_str;
          switch (state) {
            case rtc::PeerConnection::IceState::New:
              state_str = "New";
              break;
            case rtc::PeerConnection::IceState::Checking:
              state_str = "Checking";
              break;
            case rtc::PeerConnection::IceState::Connected:
              state_str = "Connected";
              spdlog::info("[WebRTC:{}] ICE connection established", session_id);
              break;
            case rtc::PeerConnection::IceState::Completed:
              state_str = "Completed";
              spdlog::info("[WebRTC:{}] ICE connection completed", session_id);
              break;
            case rtc::PeerConnection::IceState::Failed:
              state_str = "Failed";
              spdlog::error("[WebRTC:{}] ICE connection failed", session_id);
              break;
            case rtc::PeerConnection::IceState::Disconnected:
              state_str = "Disconnected";
              spdlog::warn("[WebRTC:{}] ICE connection disconnected", session_id);
              break;
            case rtc::PeerConnection::IceState::Closed:
              state_str = "Closed";
              spdlog::info("[WebRTC:{}] ICE connection closed", session_id);
              break;
            default:
              state_str = "Unknown";
              break;
          }
          spdlog::info("[WebRTC:{}] ICE state changed: {} ({})", session_id, state_str,
                       static_cast<int>(state));
        });

    // Store answer pointer for async callback
    std::string* answer_ptr = sdp_answer_str;
    auto answer_promise = std::make_shared<std::promise<std::string>>();
    std::shared_future<std::string> answer_future = answer_promise->get_future();

    // Capture manual_candidate_sdp for the callback by moving it into a shared_ptr
    auto manual_candidate_ptr = std::make_shared<std::string>(std::move(manual_candidate_sdp));

    session->peer_connection->onLocalDescription(
        [session_id, answer_ptr, answer_promise, manual_candidate_ptr](rtc::Description description) {
          spdlog::info("[WebRTC:{}] Local description created (type: {})", session_id,
                       description.typeString());
          if (description.type() == rtc::Description::Type::Answer) {
            std::string sdp = description.generateSdp();

            // Inject manual ICE candidate into SDP if configured
            if (!manual_candidate_ptr->empty()) {
              spdlog::info("[WebRTC:{}] Injecting manual ICE candidate into SDP", session_id);
              // Find the media section (m=video or m=application) and add candidate after it
              size_t media_pos = sdp.find("m=");
              if (media_pos != std::string::npos) {
                // Find the CRLF that terminates the line before the next m= block. Skip past
                // that CRLF so the candidate becomes its own properly delimited line instead
                // of being concatenated onto the previous attribute (which Chrome rejects as
                // "Invalid SDP line").
                size_t next_media = sdp.find("\r\nm=", media_pos + 2);
                size_t insert_pos = (next_media == std::string::npos) ? sdp.size()
                                                                      : next_media + 2;
                sdp.insert(insert_pos, *manual_candidate_ptr);
                spdlog::info("[WebRTC:{}] Manual ICE candidate injected (SDP length: {} -> {})",
                             session_id, description.generateSdp().length(), sdp.length());
              } else {
                spdlog::warn("[WebRTC:{}] Could not find media section in SDP, candidate not injected",
                             session_id);
              }
            }

            if (answer_ptr != nullptr && answer_ptr->empty()) {
              *answer_ptr = sdp;
            }
            try {
              answer_promise->set_value(sdp);
              spdlog::info("[WebRTC:{}] SDP answer generated and stored (length: {})", session_id,
                           sdp.length());
            } catch (const std::future_error& e) {
              spdlog::warn("[WebRTC:{}] Promise already set: {}", session_id, e.what());
            }
          } else {
            spdlog::warn("[WebRTC:{}] Local description is not Answer type: {}", session_id,
                         description.typeString());
          }
        });

    session->peer_connection->onLocalCandidate([session_id, session](rtc::Candidate candidate) {
      spdlog::info("[WebRTC:{}] Local ICE candidate: {}", session_id, candidate.candidate());
      {
        std::lock_guard<std::mutex> lock(session->candidates_mutex);
        session->local_candidates_queue.push(candidate);
      }
      session->candidates_cv.notify_one();
    });

    session->peer_connection->onTrack([session_id, session](std::shared_ptr<rtc::Track> track) {
      if (track == nullptr) {
        return;
      }

      const auto description = track->description();
      spdlog::info("[WebRTC:{}] Remote track received (mid={}, type={}, direction={})", session_id,
                   track->mid(), description.type(), static_cast<int>(description.direction()));

      if (description.type() != "video") {
        return;
      }

      std::lock_guard<std::mutex> lock(session->mutex);

      // SendOnly from C++'s perspective = outbound processed video track (browser's recvonly).
      // Capture it here to keep the impl::Track alive so mTracks["mid"] weak_ptr stays valid
      // when addTrack() is called later. Without this, the weak_ptr expires and openTracks()
      // cannot find the track to open it, leaving isOpen() permanently false.
      if (description.direction() == rtc::Description::Direction::SendOnly) {
        if (session->outbound_video_track == nullptr) {
          session->outbound_video_track = track;
          spdlog::info("[WebRTC:{}] Outbound video track pre-captured in onTrack (mid={})",
                       session_id, track->mid());
        }
        return;
      }

      if (session->inbound_video_track != nullptr) {
        spdlog::warn("[WebRTC:{}] Ignoring additional inbound video track with mid={}", session_id,
                     track->mid());
        return;
      }

      session->inbound_video_track = track;
      session->inbound_rtcp_session = std::make_shared<rtc::RtcpReceivingSession>();
      session->inbound_depacketizer = std::make_shared<rtc::H264RtpDepacketizer>();
      // incomingChain() processes in reverse order (next first, then current),
      // so depacketizer must be the root with rtcp_session as next:
      // execution order → rtcp_session::incoming first (validate RTP), then depacketizer::incoming
      // (assemble frame)
      session->inbound_depacketizer->addToChain(session->inbound_rtcp_session);
      track->setMediaHandler(session->inbound_depacketizer);

      track->onOpen([session_id, mid = track->mid()]() {
        spdlog::info("[WebRTC:{}] Inbound video track opened (mid={})", session_id, mid);
      });

      track->onClosed([session_id, mid = track->mid()]() {
        spdlog::warn("[WebRTC:{}] Inbound video track closed (mid={})", session_id, mid);
      });

      track->onFrame([session_id, session](rtc::binary frame, rtc::FrameInfo info) {
        {
          std::lock_guard<std::mutex> heartbeat_lock(session->mutex);
          session->last_heartbeat = std::chrono::steady_clock::now();
        }

        std::lock_guard<std::mutex> media_lock(session->media_mutex);

        static std::atomic<int> s_frame_count{0};
        const int frame_num = ++s_frame_count;
        if (frame_num <= 5 || frame_num % 30 == 0) {
          spdlog::info(
              "[WebRTC:{}] onFrame fired (#{}) size={} processor={} outbound={} open={}",
              session_id, frame_num, frame.size(), session->live_video_processor != nullptr,
              session->outbound_video_track != nullptr,
              session->outbound_video_track ? session->outbound_video_track->isOpen() : false);
        }

        if (session->live_video_processor == nullptr || session->outbound_video_track == nullptr) {
          return;
        }

        if (!session->outbound_video_track->isOpen()) {
          return;
        }

        const auto process_started = std::chrono::steady_clock::now();
        std::vector<EncodedAccessUnit> encoded_units;
        cuda_learning::DetectionFrame detection_frame;
        std::string error_message;
        const bool ok = session->live_video_processor->ProcessAccessUnit(
            frame, info, session->live_filter_state, &encoded_units, &detection_frame, &error_message);
        if (!ok) {
          spdlog::error("[WebRTC:{}] Live camera frame processing failed: {}", session_id,
                        error_message);
          return;
        }

        if (frame_num <= 5 || frame_num % 30 == 0) {
          spdlog::info("[WebRTC:{}] Frame #{} processed OK, {} encoded units", session_id,
                       frame_num, encoded_units.size());
        }

        for (const auto& encoded_unit : encoded_units) {
          try {
            session->outbound_video_track->sendFrame(encoded_unit.data, encoded_unit.frame_info);
          } catch (const std::exception& exception) {
            spdlog::error("[WebRTC:{}] Failed to send processed video frame: {}", session_id,
                          exception.what());
            return;
          }
        }

        {
          std::shared_ptr<rtc::DataChannel> stats_ch;
          {
            std::lock_guard<std::mutex> lock(session->mutex);
            stats_ch = session->stats_channel;
          }
          if (stats_ch && stats_ch->isOpen()) {
            const auto elapsed_ms = std::chrono::duration_cast<std::chrono::microseconds>(
                                        std::chrono::steady_clock::now() - process_started)
                                        .count() /
                                    1000.0;
            cuda_learning::ProcessingStatsFrame stats_frame;
            stats_frame.set_session_id(session_id);
            stats_frame.set_frame_id(0);
            stats_frame.set_capture_ts_unix_ms(CurrentUnixTimeMs());

            auto* total_ms_metric = stats_frame.add_metrics();
            total_ms_metric->set_key("pipeline.total.ms");
            total_ms_metric->set_unit("ms");
            total_ms_metric->mutable_value()->set_double_value(elapsed_ms);

            auto* detections_metric = stats_frame.add_metrics();
            detections_metric->set_key("detections.count");
            detections_metric->set_unit("count");
            detections_metric->mutable_value()->set_int64_value(detection_frame.detections_size());

            std::string stats_payload;
            if (stats_frame.SerializeToString(&stats_payload)) {
              SendFramed(*stats_ch, stats_payload,
                         session->outgoing_message_id.fetch_add(1, std::memory_order_relaxed) + 1U);
            } else {
              spdlog::error("[WebRTC:{}] Failed to serialize ProcessingStatsFrame", session_id);
            }
          }
        }

        if (detection_frame.detections_size() > 0) {
          std::shared_ptr<rtc::DataChannel> det_ch;
          {
            std::lock_guard<std::mutex> lock(session->mutex);
            det_ch = session->detection_channel;
          }
          if (det_ch && det_ch->isOpen()) {
            std::string det_output;
            if (detection_frame.SerializeToString(&det_output)) {
              SendFramed(*det_ch, det_output,
                         session->outgoing_message_id.fetch_add(1, std::memory_order_relaxed) + 1U);
            }
          }
        }
      });
    });

    // Register callback to receive remote data channel from client
    // This must be done BEFORE setRemoteDescription to ensure we capture the channel
    session->peer_connection->onDataChannel([weak_self = weak_from_this(), session_id, session](
                                                std::shared_ptr<rtc::DataChannel> data_channel) {
      const std::string label = data_channel->label();
      spdlog::info("[WebRTC:{}] Remote data channel received: {}", session_id, label);

      if (label == "detections") {
        data_channel->onOpen([session_id]() {
          spdlog::info("[WebRTC:{}] Detection channel opened", session_id);
        });
        data_channel->onClosed([session_id, session]() {
          spdlog::warn("[WebRTC:{}] Detection channel closed", session_id);
          std::lock_guard<std::mutex> cl(session->mutex);
          session->detection_channel = nullptr;
        });
        data_channel->onError([session_id](std::string err) {
          spdlog::error("[WebRTC:{}] Detection channel error: {}", session_id, err);
        });
        {
          std::lock_guard<std::mutex> lock(session->mutex);
          session->detection_channel = data_channel;
        }
        return;
      }

      if (label == kStatsChannelLabel) {
        data_channel->onOpen([session_id]() {
          spdlog::info("[WebRTC:{}] Stats channel opened", session_id);
        });
        data_channel->onClosed([session_id, session]() {
          spdlog::warn("[WebRTC:{}] Stats channel closed", session_id);
          std::lock_guard<std::mutex> cl(session->mutex);
          session->stats_channel = nullptr;
        });
        data_channel->onError([session_id](std::string err) {
          spdlog::error("[WebRTC:{}] Stats channel error: {}", session_id, err);
        });
        {
          std::lock_guard<std::mutex> lock(session->mutex);
          session->stats_channel = data_channel;
        }
        return;
      }

      session->data_channel = data_channel;

      std::weak_ptr<rtc::DataChannel> weak_dc = data_channel;

      // Register callbacks on the received data channel
      data_channel->onOpen([weak_self, weak_dc, session_id, label, session]() {
        auto self = weak_self.lock();
        auto dc = weak_dc.lock();
        if (!self || !dc)
          return;
        {
          std::lock_guard<std::mutex> lock(session->mutex);
          session->last_heartbeat = std::chrono::steady_clock::now();
        }
        if (ShouldRegisterSessionChannel(session_id, label)) {
          self->RegisterSessionChannel(session_id, dc);
        }
        spdlog::info("[WebRTC:{}] Remote data channel opened successfully - isOpen: {}", session_id,
                     dc->isOpen());
      });

      data_channel->onMessage([weak_self, weak_dc, session_id, session](rtc::message_variant data) {
        auto self = weak_self.lock();
        auto dc = weak_dc.lock();
        if (!self || !dc)
          return;

        {
          std::lock_guard<std::mutex> lock(session->mutex);
          session->last_heartbeat = std::chrono::steady_clock::now();
        }

        if (!std::holds_alternative<rtc::binary>(data)) {
          spdlog::warn("[WebRTC:{}] Ignoring non-binary data channel message", session_id);
          return;
        }

        if (self->engine_ == nullptr) {
          spdlog::error("[WebRTC:{}] Cannot process frame: processor engine unavailable",
                        session_id);
          return;
        }

        const auto& raw_chunk = std::get<rtc::binary>(data);
        const auto raw_span = std::span<const std::byte>(raw_chunk.data(), raw_chunk.size());
        auto maybe_payload = session->incoming_reassembler->PushChunk(raw_span);
        if (!maybe_payload.has_value()) {
          return;  // chunk accepted but message not yet complete
        }
        const auto& assembled = maybe_payload.value();

        cuda_learning::ProcessImageRequest request;
        if (!request.ParseFromArray(assembled.data(), static_cast<int>(assembled.size()))) {
          spdlog::warn("[WebRTC:{}] Failed to parse ProcessImageRequest ({} bytes)", session_id,
                       assembled.size());
          return;
        }

        const bool is_control_update = request.image_data().empty() && request.width() == 0 &&
                                       request.height() == 0 && request.channels() == 0;

        if (is_control_update) {
          // A control update with no filters is treated as a keepalive: the
          // last_heartbeat timestamp was already refreshed above, so we skip
          // UpdateFilterState to avoid clobbering the active filter state.
          if (request.generic_filters_size() == 0 && request.filters_size() == 0) {
            spdlog::debug("[WebRTC:{}] Heartbeat control message received", session_id);
            return;
          }

          if (session->live_video_processor == nullptr) {
            spdlog::error("[WebRTC:{}] Cannot apply live filter update: video processor missing",
                          session_id);
            return;
          }

          std::lock_guard<std::mutex> media_lock(session->media_mutex);
          std::string error_message;
          if (!session->live_video_processor->UpdateFilterState(
                  request, &session->live_filter_state, &error_message)) {
            spdlog::error("[WebRTC:{}] Failed to update live filter state: {}", session_id,
                          error_message);
            return;
          }

          spdlog::info("[WebRTC:{}] Live camera filter state updated (generic_filters={})",
                       session_id, session->live_filter_state.generic_filters_size());
          return;
        }

        cuda_learning::ProcessImageResponse response;
        CopyProcessMetadata(request, &response);

        const auto process_started = std::chrono::steady_clock::now();
        const bool ok = self->engine_->ProcessImage(request, &response);
        if (!ok || response.code() != 0) {
          spdlog::warn("[WebRTC:{}] DataChannel frame processing failed (code={}): {}", session_id,
                       response.code(), response.message());
        }

        response.set_frame_id(request.frame_id());

        std::string output;
        if (!response.SerializeToString(&output)) {
          spdlog::error("[WebRTC:{}] Failed to serialize ProcessImageResponse", session_id);
          return;
        }

        if (!dc->isOpen()) {
          spdlog::warn("[WebRTC:{}] Cannot send ProcessImageResponse: data channel closed",
                       session_id);
          return;
        }

        SendFramed(*dc, output,
                   session->outgoing_message_id.fetch_add(1, std::memory_order_relaxed) + 1U);

        {
          std::shared_ptr<rtc::DataChannel> stats_ch;
          {
            std::lock_guard<std::mutex> lock(session->mutex);
            stats_ch = session->stats_channel;
          }
          if (stats_ch && stats_ch->isOpen()) {
            const auto elapsed_ms = std::chrono::duration_cast<std::chrono::microseconds>(
                                        std::chrono::steady_clock::now() - process_started)
                                        .count() /
                                    1000.0;
            cuda_learning::ProcessingStatsFrame stats_frame;
            stats_frame.set_session_id(session_id);
            stats_frame.set_frame_id(request.frame_id());
            stats_frame.set_capture_ts_unix_ms(CurrentUnixTimeMs());

            auto* total_ms_metric = stats_frame.add_metrics();
            total_ms_metric->set_key("pipeline.total.ms");
            total_ms_metric->set_unit("ms");
            total_ms_metric->mutable_value()->set_double_value(elapsed_ms);

            auto* detections_metric = stats_frame.add_metrics();
            detections_metric->set_key("detections.count");
            detections_metric->set_unit("count");
            detections_metric->mutable_value()->set_int64_value(response.detections_size());

            std::string stats_payload;
            if (stats_frame.SerializeToString(&stats_payload)) {
              SendFramed(*stats_ch, stats_payload,
                         session->outgoing_message_id.fetch_add(1, std::memory_order_relaxed) + 1U);
            } else {
              spdlog::error("[WebRTC:{}] Failed to serialize ProcessingStatsFrame", session_id);
            }
          }
        }

        if (response.detections_size() > 0) {
          std::shared_ptr<rtc::DataChannel> det_ch;
          {
            std::lock_guard<std::mutex> lock(session->mutex);
            det_ch = session->detection_channel;
          }
          if (det_ch && det_ch->isOpen()) {
            cuda_learning::DetectionFrame det_frame;
            det_frame.set_frame_id(request.frame_id());
            det_frame.set_image_width(request.width());
            det_frame.set_image_height(request.height());
            for (const auto& d : response.detections()) {
              *det_frame.add_detections() = d;
            }
            std::string det_output;
            if (det_frame.SerializeToString(&det_output)) {
              SendFramed(*det_ch, det_output,
                         session->outgoing_message_id.fetch_add(1, std::memory_order_relaxed) + 1U);
            }
          }
        }
      });

      data_channel->onClosed([weak_self, session_id, label]() {
        auto self = weak_self.lock();
        if (!self)
          return;
        if (ShouldRegisterSessionChannel(session_id, label)) {
          self->UnregisterSessionChannel(session_id);
        }
        spdlog::warn("[WebRTC:{}] Remote data channel closed", session_id);
      });

      data_channel->onError([session_id](std::string err) {
        spdlog::error("[WebRTC:{}] Remote data channel error: {}", session_id, err);
      });

      spdlog::info("[WebRTC:{}] Data channel callbacks registered on remote channel", session_id);
    });

    // Set remote description - this will trigger onDataChannel if client created a channel
    spdlog::debug("[WebRTC:{}] Parsing SDP offer (length: {})", session_id, sdp_offer_str.length());

    // Check if SDP contains fingerprint (required for DTLS)
    bool has_fingerprint = sdp_offer_str.find("fingerprint:") != std::string::npos ||
                           sdp_offer_str.find("a=fingerprint:") != std::string::npos;
    spdlog::debug("[WebRTC:{}] SDP offer contains fingerprint: {}", session_id, has_fingerprint);

    if (!has_fingerprint) {
      spdlog::warn(
          "[WebRTC:{}] SDP offer missing fingerprint - this may cause DTLS handshake to fail",
          session_id);
      spdlog::debug("[WebRTC:{}] SDP offer preview: {}", session_id, sdp_offer_str.substr(0, 300));
    }

    const std::string sanitized_sdp = StripRtpHeaderExtensions(sdp_offer_str);
    spdlog::debug("[WebRTC:{}] SDP offer sanitized (extmap stripped, length: {})", session_id,
                  sanitized_sdp.length());

    rtc::Description offer(sanitized_sdp, rtc::Description::Type::Offer);
    spdlog::debug("[WebRTC:{}] SDP offer parsed successfully, type: {}", session_id,
                  offer.typeString());

    try {
      session->peer_connection->setRemoteDescription(offer);
      spdlog::info("[WebRTC:{}] Remote description set successfully", session_id);
    } catch (const std::exception& e) {
      spdlog::error("[WebRTC:{}] Failed to set remote description: {}", session_id, e.what());
      spdlog::error("[WebRTC:{}] Full SDP offer that failed:\n{}", session_id, sanitized_sdp);
      if (error_message != nullptr) {
        *error_message = std::string("Failed to set remote description: ") + e.what();
      }
      return false;
    }

    // Log SSRCs from offer to diagnose routing
    for (int i = 0; i < offer.mediaCount(); ++i) {
      const auto entry = offer.media(i);
      if (std::holds_alternative<rtc::Description::Media*>(entry)) {
        const auto* media = std::get<rtc::Description::Media*>(entry);
        if (media) {
          const auto ssrcs = media->getSSRCs();
          spdlog::info("[WebRTC:{}] SDP mid={} dir={} has {} SSRCs: {}", session_id, media->mid(),
                       static_cast<int>(media->direction()), ssrcs.size(),
                       ssrcs.empty() ? "(none)" : std::to_string(*ssrcs.begin()));
        }
      }
    }
    if (const auto outbound_video_config = FindOutboundVideoConfig(offer);
        outbound_video_config.has_value()) {
      try {
        rtc::Description::Video media(outbound_video_config->mid,
                                      rtc::Description::Direction::SendOnly);
        media.addH264Codec(outbound_video_config->payload_type);
        media.setBitrate(static_cast<int>(kProcessedVideoBitrate));

        const uint32_t ssrc = MakeSsrc(session_id);
        media.addSSRC(ssrc, "processed-video", session_id, kProcessedVideoTrackLabel);

        session->outbound_video_track = session->peer_connection->addTrack(media);
        session->outbound_rtp_config = std::make_shared<rtc::RtpPacketizationConfig>(
            ssrc, "processed-video", static_cast<uint8_t>(outbound_video_config->payload_type),
            rtc::H264RtpPacketizer::ClockRate);
        session->outbound_packetizer = std::make_shared<rtc::H264RtpPacketizer>(
            rtc::NalUnit::Separator::StartSequence, session->outbound_rtp_config);
        session->outbound_sr_reporter =
            std::make_shared<rtc::RtcpSrReporter>(session->outbound_rtp_config);
        session->outbound_nack_responder = std::make_shared<rtc::RtcpNackResponder>();
        session->outbound_packetizer->addToChain(session->outbound_sr_reporter);
        session->outbound_packetizer->addToChain(session->outbound_nack_responder);
        session->outbound_video_track->setMediaHandler(session->outbound_packetizer);
        session->outbound_video_track->onOpen([session_id, mid = outbound_video_config->mid]() {
          spdlog::info("[WebRTC:{}] Outbound processed video track opened (mid={})", session_id,
                       mid);
        });
        session->outbound_video_track->onClosed([session_id, mid = outbound_video_config->mid]() {
          spdlog::warn("[WebRTC:{}] Outbound processed video track closed (mid={})", session_id,
                       mid);
        });
      } catch (const std::exception& exception) {
        if (error_message != nullptr) {
          *error_message =
              std::string("Failed to create outbound processed video track: ") + exception.what();
        }
        spdlog::error("[WebRTC:{}] Failed to create outbound processed video track: {}", session_id,
                      exception.what());
        return false;
      }
    } else {
      spdlog::warn(
          "[WebRTC:{}] Offer does not include a recvonly H264 video transceiver; "
          "processed remote track will not be negotiated",
          session_id);
    }

    spdlog::info("[WebRTC:{}] After addTrack: inbound_track={} outbound_track={}", session_id,
                 session->inbound_video_track != nullptr, session->outbound_video_track != nullptr);

    // Wait a moment for the state to transition and for onDataChannel to be called
    std::this_thread::sleep_for(std::chrono::milliseconds(100));

    for (const auto& candidate : session->pending_candidates) {
      session->peer_connection->addRemoteCandidate(candidate);
    }
    session->pending_candidates.clear();

    // Create answer - libdatachannel generates it asynchronously
    // The signaling state should now be "have-remote-offer" which allows creating an answer
    // Note: setLocalDescription may throw an exception, but the callback will still fire
    try {
      session->peer_connection->setLocalDescription(rtc::Description::Type::Answer);
    } catch (const std::exception& e) {
      spdlog::warn("[WebRTC:{}] setLocalDescription threw exception (may be non-fatal): {}",
                   session_id, e.what());
      // Continue anyway - the callback may still fire and generate the answer
      // libdatachannel sometimes throws but still generates the answer
    }

    // Check immediately if answer is available (libdatachannel may generate it synchronously)
    // Then wait for callback with timeout
    auto start_time = std::chrono::steady_clock::now();
    const auto timeout = std::chrono::seconds(10);
    bool answer_ready = false;

    // First, check immediately
    if (session->peer_connection->localDescription().has_value()) {
      auto local_desc = session->peer_connection->localDescription().value();
      if (local_desc.type() == rtc::Description::Type::Answer) {
        std::string sdp = local_desc.generateSdp();
        if (sdp_answer_str != nullptr && sdp_answer_str->empty()) {
          *sdp_answer_str = sdp;
          spdlog::info("[WebRTC:{}] SDP answer available immediately (length: {})", session_id,
                       sdp.length());
          answer_ready = true;
        }
      }
    }

    // If not available immediately, wait for callback with periodic checks
    if (!answer_ready) {
      spdlog::info("[WebRTC:{}] Waiting for SDP answer (timeout: {}s)", session_id,
                   timeout.count());
      while ((std::chrono::steady_clock::now() - start_time) < timeout) {
        // Check if callback fired
        auto status = answer_future.wait_for(std::chrono::milliseconds(100));
        if (status == std::future_status::ready) {
          try {
            std::string answer = answer_future.get();
            if (sdp_answer_str != nullptr && sdp_answer_str->empty()) {
              *sdp_answer_str = answer;
            }
            spdlog::info("[WebRTC:{}] SDP answer received via callback (length: {})", session_id,
                         answer.length());
            answer_ready = true;
            break;
          } catch (const std::exception& e) {
            spdlog::error("[WebRTC:{}] Error getting answer from future: {}", session_id, e.what());
          }
        }

        // Also check directly (callback may have fired but promise not set yet)
        if (session->peer_connection->localDescription().has_value()) {
          auto local_desc = session->peer_connection->localDescription().value();
          if (local_desc.type() == rtc::Description::Type::Answer) {
            std::string sdp = local_desc.generateSdp();
            if (sdp_answer_str != nullptr && sdp_answer_str->empty()) {
              *sdp_answer_str = sdp;
              spdlog::info("[WebRTC:{}] Retrieved SDP answer directly (length: {})", session_id,
                           sdp.length());
              answer_ready = true;
              break;
            }
          }
        }

        std::this_thread::sleep_for(std::chrono::milliseconds(50));
      }
    }

    // If we got the answer, consider it successful even if setLocalDescription threw
    if (answer_ready && sdp_answer_str != nullptr && !sdp_answer_str->empty()) {
      spdlog::info("[WebRTC:{}] Session created successfully (answer length: {})", session_id,
                   sdp_answer_str->length());
      // Log first 500 characters of SDP for diagnostics
      spdlog::debug("[WebRTC:{}] SDP answer preview: {}", session_id,
                    sdp_answer_str->substr(0, std::min(size_t(500), sdp_answer_str->length())));
      sessions_[session_id] = session;
      return true;
    }

    if (!answer_ready) {
      spdlog::error("[WebRTC:{}] Timeout waiting for SDP answer ({}s)", session_id,
                    timeout.count());
      // Clean up the session before returning
      try {
        std::lock_guard<std::mutex> session_lock(session->mutex);
        if (session->data_channel) {
          session->data_channel->close();
        }
        if (session->peer_connection) {
          session->peer_connection->close();
        }
      } catch (const std::exception& e) {
        spdlog::warn("[WebRTC:{}] Error cleaning up failed session: {}", session_id, e.what());
      }
      RemoveSession(session_id);
      if (error_message != nullptr) {
        *error_message =
            "Timeout waiting for SDP answer after " + std::to_string(timeout.count()) + " seconds";
      }
      return false;
    }

    // This should not be reached if answer_ready is true (we return early above)
    // But keep it as fallback
    sessions_[session_id] = session;
    spdlog::info("[WebRTC:{}] Session created successfully", session_id);
    return true;
  } catch (const std::exception& e) {
    if (error_message != nullptr) {
      *error_message = "Failed to create WebRTC session: " + std::string(e.what());
    }
    spdlog::error("[WebRTC:{}] Failed to create session: {}", session_id, e.what());
    RemoveSession(session_id);
    return false;
  }
}

bool WebRTCManager::CloseSession(const std::string& session_id, std::string* error_message) {
  auto session = GetSession(session_id);
  if (!session) {
    if (error_message != nullptr) {
      *error_message = "Session with ID " + session_id + " not found";
    }
    return false;
  }

  try {
    std::lock_guard<std::mutex> lock(session->mutex);

    if (session->memory_pool) {
      session->memory_pool->Clear();
      spdlog::info("[WebRTC:{}] CUDA memory pool cleared", session_id);
    }

    if (session->data_channel) {
      session->data_channel->close();
      spdlog::info("[WebRTC:{}] Data channel closed", session_id);
    }

    if (session->stats_channel) {
      session->stats_channel->close();
      spdlog::info("[WebRTC:{}] Stats channel closed", session_id);
    }

    if (session->peer_connection) {
      session->peer_connection->close();
      spdlog::info("[WebRTC:{}] Peer connection closed", session_id);
    }

    RemoveSession(session_id);
    spdlog::info("[WebRTC:{}] Session closed successfully", session_id);
    return true;
  } catch (const std::exception& e) {
    if (error_message != nullptr) {
      *error_message = "Failed to close session: " + std::string(e.what());
    }
    spdlog::error("[WebRTC:{}] Failed to close session: {}", session_id, e.what());
    RemoveSession(session_id);
    return false;
  }
}

bool WebRTCManager::HandleRemoteCandidate(const std::string& session_id,
                                          const std::string& candidate_str,
                                          const std::string& sdp_mid, int /* sdp_mline_index */,
                                          std::string* error_message) {
  auto session = GetSession(session_id);
  if (!session) {
    if (error_message != nullptr) {
      *error_message = "Session with ID " + session_id + " not found";
    }
    return false;
  }

  try {
    rtc::Candidate candidate(candidate_str, sdp_mid);
    std::lock_guard<std::mutex> lock(session->mutex);

    // Defense in depth: libdatachannel/juice can reset the ICE agent and tear down
    // an established DTLS/SCTP transport when remote candidates arrive after the peer
    // connection is already connected. The browser filters these out too, but we also
    // drop them here so a stale client cannot destabilize an active session.
    const auto pc_state = session->peer_connection->state();
    if (pc_state == rtc::PeerConnection::State::Connected) {
      spdlog::debug("[WebRTC:{}] Ignoring late remote ICE candidate (peer already connected): {}",
                    session_id, candidate_str);
      return true;
    }

    if (session->peer_connection->remoteDescription().has_value()) {
      session->peer_connection->addRemoteCandidate(candidate);
      spdlog::info("[WebRTC:{}] Added remote ICE candidate: {}", session_id, candidate_str);
    } else {
      session->pending_candidates.push_back(candidate);
      spdlog::info("[WebRTC:{}] Stored pending remote ICE candidate: {}", session_id,
                   candidate_str);
    }
    return true;
  } catch (const std::exception& e) {
    if (error_message != nullptr) {
      *error_message = "Failed to add remote ICE candidate: " + std::string(e.what());
    }
    spdlog::error("[WebRTC:{}] Failed to add candidate: {}", session_id, e.what());
    return false;
  }
}

std::vector<rtc::Candidate> WebRTCManager::GetPendingLocalCandidates(
    const std::string& session_id) {
  std::vector<rtc::Candidate> candidates;
  auto session = GetSession(session_id);
  if (session) {
    std::lock_guard<std::mutex> lock(session->candidates_mutex);
    while (!session->local_candidates_queue.empty()) {
      candidates.push_back(session->local_candidates_queue.front());
      session->local_candidates_queue.pop();
    }
  }
  return candidates;
}

void WebRTCManager::SendToSession(const std::string& session_id, const std::string& bytes) {
  if (session_id.empty()) {
    spdlog::warn("[WebRTC] Cannot route response to empty session id");
    return;
  }

  std::shared_ptr<rtc::DataChannel> channel;
  {
    std::lock_guard<std::mutex> lock(session_channels_mutex_);
    auto it = session_channels_.find(session_id);
    if (it == session_channels_.end()) {
      spdlog::warn("[WebRTC:{}] No routable data channel registered for session", session_id);
      return;
    }
    channel = it->second.lock();
    if (!channel) {
      session_channels_.erase(it);
      spdlog::warn("[WebRTC:{}] Routable data channel expired", session_id);
      return;
    }
  }

  if (!channel->isOpen()) {
    spdlog::warn("[WebRTC:{}] Cannot route response: data channel is not open", session_id);
    return;
  }

  auto session = GetSession(session_id);
  const uint32_t msg_id = session
      ? session->outgoing_message_id.fetch_add(1, std::memory_order_relaxed) + 1U
      : 1U;
  SendFramed(*channel, bytes, msg_id);

  if (session) {
    std::lock_guard<std::mutex> lock(session->mutex);
    session->last_heartbeat = std::chrono::steady_clock::now();
  }
}

std::shared_ptr<WebRTCManager::SessionState> WebRTCManager::GetSession(
    const std::string& session_id) {
  std::lock_guard<std::mutex> lock(sessions_mutex_);
  auto it = sessions_.find(session_id);
  if (it != sessions_.end()) {
    return it->second;
  }
  return nullptr;
}

void WebRTCManager::RemoveSession(const std::string& session_id) {
  {
    std::lock_guard<std::mutex> lock(sessions_mutex_);
    sessions_.erase(session_id);
  }
  UnregisterSessionChannel(session_id);
  spdlog::info("[WebRTC:{}] Session removed", session_id);
}

void WebRTCManager::CleanupInactiveSessions(int timeout_seconds) {
  std::lock_guard<std::mutex> lock(sessions_mutex_);
  auto now = std::chrono::steady_clock::now();
  auto timeout_duration = std::chrono::seconds(timeout_seconds);

  std::vector<std::string> sessions_to_remove;

  for (const auto& [session_id, session] : sessions_) {
    std::lock_guard<std::mutex> session_lock(session->mutex);
    auto elapsed = now - session->last_heartbeat;

    if (elapsed > timeout_duration) {
      spdlog::warn("[WebRTC:{}] Session inactive for {} seconds, closing", session_id,
                   std::chrono::duration_cast<std::chrono::seconds>(elapsed).count());
      sessions_to_remove.push_back(session_id);

      try {
        if (session->data_channel) {
          session->data_channel->close();
        }
        if (session->stats_channel) {
          session->stats_channel->close();
        }
        if (session->peer_connection) {
          session->peer_connection->close();
        }
      } catch (const std::exception& e) {
        spdlog::error("[WebRTC:{}] Error closing inactive session: {}", session_id, e.what());
      }
    }
  }

  for (const auto& session_id : sessions_to_remove) {
    sessions_.erase(session_id);
    UnregisterSessionChannel(session_id);
    spdlog::info("[WebRTC:{}] Inactive session removed", session_id);
  }

  if (!sessions_to_remove.empty()) {
    spdlog::info("Cleaned up {} inactive WebRTC session(s)", sessions_to_remove.size());
  }
}

void WebRTCManager::RegisterSessionChannel(const std::string& session_id,
                                           const std::shared_ptr<rtc::DataChannel>& data_channel) {
  std::lock_guard<std::mutex> lock(session_channels_mutex_);
  session_channels_[session_id] = data_channel;
  spdlog::info("[WebRTC:{}] Registered routable browser data channel", session_id);
}

void WebRTCManager::UnregisterSessionChannel(const std::string& session_id) {
  std::lock_guard<std::mutex> lock(session_channels_mutex_);
  session_channels_.erase(session_id);
}

}  // namespace jrb::ports::grpc_service
