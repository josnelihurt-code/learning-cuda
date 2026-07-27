#include "src/cpp_accelerator/adapters/webrtc/webrtc_manager.h"

#include <algorithm>
#include <chrono>
#include <cstdint>
#include <ctime>
#include <exception>
#include <filesystem>
#include <fstream>
#include <future>
#include <memory>
#include <mutex>
#include <set>
#include <string>
#include <thread>
#include <variant>
#include <vector>

#include <spdlog/spdlog.h>
#include <rtc/rtc.hpp>

#include "proto/_virtual_imports/image_processor_service_proto/image_processor_service.pb.h"
#include "src/cpp_accelerator/adapters/camera/gst_camera_source.h"
#include "src/cpp_accelerator/adapters/image_io/image_writer.h"
#include "src/cpp_accelerator/adapters/webrtc/channel_labels.h"
#include "src/cpp_accelerator/adapters/webrtc/control_message_dispatcher.h"
#include "src/cpp_accelerator/adapters/webrtc/protocol/data_channel_envelope.h"
#include "src/cpp_accelerator/adapters/webrtc/protocol/filter_resolver.h"
#include "src/cpp_accelerator/adapters/webrtc/sdp/sdp_utils.h"
#include "src/cpp_accelerator/adapters/webrtc/session_routing.h"
#include "src/cpp_accelerator/application/engine/processor_engine.h"
#include "src/cpp_accelerator/application/server_info/server_info_provider.h"

namespace jrb::adapters::webrtc {

using cuda_learning::ACCELERATOR_TYPE_CUDA;
using cuda_learning::ControlRequest;
using cuda_learning::ControlResponse;
using cuda_learning::DetectionFrame;
using cuda_learning::FILTER_TYPE_NONE;
using cuda_learning::ProcessImageRequest;
using cuda_learning::ProcessImageResponse;
using cuda_learning::ProcessingStatsFrame;
using jrb::adapters::image::ImageWriter;
using jrb::adapters::webrtc::protocol::CopyProcessMetadata;
using jrb::adapters::webrtc::protocol::ParseDataChannelRequest;
using jrb::adapters::webrtc::protocol::ResolveGenericSelectionsInPlace;
using jrb::adapters::webrtc::sdp::BuildManualCandidateSdp;
using jrb::adapters::webrtc::sdp::FindOutboundVideoConfig;
using jrb::adapters::webrtc::sdp::MakeSsrc;
using jrb::adapters::webrtc::sdp::StripRtpHeaderExtensions;
using jrb::adapters::webrtc::sdp::WaitForSdpAnswer;

WebRTCManager::WebRTCManager(WebRTCManagerConfig config)
    : engine_(std::move(config.engine)),
      camera_hub_(std::move(config.camera_hub)),
      server_info_(
          std::make_unique<jrb::application::server_info::ServerInfoProvider>(engine_.get())),
      image_sink_(config.image_sink ? std::move(config.image_sink)
                                    : std::make_shared<ImageWriter>()),
      captures_dir_(std::move(config.captures_dir)),
      initialized_(false),
      device_id_(std::move(config.device_id)),
      display_name_(std::move(config.display_name)),
      cleanup_running_(false),
      accelerator_version_(std::move(config.accelerator_version)) {
  rtc::InitLogger(rtc::LogLevel::Error);
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
    // Enable TCP ICE candidates for firewall fallback (requires specific ports).
    config_->enableIceTcp = true;
    // Raise SCTP max message size so peers can safely transmit full-resolution frames
    // without Chrome aborting with OperationError: data-channel-failure.
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

    auto weak_self = weak_from_this();
    dispatcher_ = std::make_unique<ControlMessageDispatcher>(ControlMessageDispatcher::Config{
        .server_info = server_info_.get(),
        .engine = engine_,
        .camera_hub = camera_hub_,
        .image_sink = image_sink_,
        .device_id = device_id_,
        .display_name = display_name_,
        .captures_dir = captures_dir_,
        .accelerator_version = accelerator_version_,
        .video_frame_handler =
            [weak_self](const std::string& sid, rtc::binary data, rtc::FrameInfo info) {
              auto self = weak_self.lock();
              if (!self)
                return;
              auto s = self->GetSession(sid);
              if (!s)
                return;
              self->HandleVideoFrame(sid, *s, std::move(data), info);
            },
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
    std::lock_guard<std::mutex> session_lock(session->mutex);
    CloseSessionTransport(session_id, *session);
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

// ---------------------------------------------------------------------------
// CreateSession — orchestrates session setup in 8 clear steps.
// ---------------------------------------------------------------------------
bool WebRTCManager::CreateSession(const std::string& session_id, const std::string& sdp_offer_str,
                                  std::string* sdp_answer_str, std::string* error_message) {
  if (!initialized_) {
    if (error_message)
      *error_message = "WebRTC manager not initialized";
    return false;
  }
  if (session_id.empty() || sdp_offer_str.empty()) {
    if (error_message)
      *error_message = "session_id and sdp_offer are required";
    return false;
  }

  std::lock_guard<std::mutex> lock(sessions_mutex_);
  if (sessions_.count(session_id)) {
    if (error_message)
      *error_message = "Session with ID " + session_id + " already exists";
    return false;
  }

  try {
    // 1. Initialise session state.
    auto session = std::make_shared<SessionState>();
    session->created_at = std::chrono::steady_clock::now();
    session->last_heartbeat = std::chrono::steady_clock::now();
    session->peer_connection = std::make_shared<rtc::PeerConnection>(*config_);
    // Create memory pool before LiveVideoProcessor so the pool pointer is valid.
    session->memory_pool = std::make_unique<jrb::adapters::compute::cuda::CudaMemoryPool>();
    session->live_video_processor = std::make_unique<LiveVideoProcessor>(
        engine_.get(), session->memory_pool.get(), image_sink_.get());
    session->live_filter_state.set_accelerator(ACCELERATOR_TYPE_CUDA);
    session->live_filter_state.add_filters(FILTER_TYPE_NONE);
    session->live_filter_state.set_api_version("1.1");
    spdlog::info("[WebRTC:{}] Created dedicated CUDA memory pool for session", session_id);

    // 2. Build manual ICE candidate SDP (reads env vars; may return empty string).
    auto manual_candidate_ptr = std::make_shared<std::string>(BuildManualCandidateSdp(session_id));

    // 3. Register peer connection and media track callbacks.
    auto answer_future =
        SetupPeerConnectionCallbacks(session_id, session, manual_candidate_ptr, sdp_answer_str);

    // 4. Register data channel dispatcher.
    SetupDataChannels(session_id, session);

    // 5. Parse, sanitize, and apply the remote SDP offer.
    const bool has_fingerprint = sdp_offer_str.find("fingerprint:") != std::string::npos ||
                                 sdp_offer_str.find("a=fingerprint:") != std::string::npos;
    spdlog::debug("[WebRTC:{}] Parsing SDP offer (length: {}, fingerprint: {})", session_id,
                  sdp_offer_str.length(), has_fingerprint);
    if (!has_fingerprint) {
      spdlog::warn("[WebRTC:{}] SDP offer missing fingerprint - DTLS handshake may fail",
                   session_id);
      spdlog::debug("[WebRTC:{}] SDP offer preview: {}", session_id, sdp_offer_str.substr(0, 300));
    }
    const std::string sanitized_sdp = StripRtpHeaderExtensions(sdp_offer_str);
    spdlog::debug("[WebRTC:{}] SDP offer sanitized (extmap stripped, length: {})", session_id,
                  sanitized_sdp.length());
    rtc::Description offer(sanitized_sdp, rtc::Description::Type::Offer);
    try {
      session->peer_connection->setRemoteDescription(offer);
      spdlog::debug("[WebRTC:{}] Remote description set successfully", session_id);
    } catch (const std::exception& e) {
      spdlog::error("[WebRTC:{}] Failed to set remote description: {}", session_id, e.what());
      spdlog::error("[WebRTC:{}] Full SDP offer that failed:\n{}", session_id, sanitized_sdp);
      if (error_message)
        *error_message = std::string("Failed to set remote description: ") + e.what();
      return false;
    }

    // Log SSRCs from offer for diagnostics.
    for (int i = 0; i < offer.mediaCount(); ++i) {
      const auto entry = offer.media(i);
      if (std::holds_alternative<rtc::Description::Media*>(entry)) {
        const auto* media = std::get<rtc::Description::Media*>(entry);
        if (media) {
          const auto ssrcs = media->getSSRCs();
          spdlog::debug("[WebRTC:{}] SDP mid={} dir={} has {} SSRCs: {}", session_id, media->mid(),
                        static_cast<int>(media->direction()), ssrcs.size(),
                        ssrcs.empty() ? "(none)" : std::to_string(*ssrcs.begin()));
        }
      }
    }

    // 6. Add the outbound video track (requires parsed offer).
    if (!SetupMediaTracks(session_id, session, offer, error_message)) {
      return false;
    }
    spdlog::debug("[WebRTC:{}] After addTrack: inbound_track={} outbound_track={}", session_id,
                  session->inbound_video_track != nullptr,
                  session->outbound_video_track != nullptr);

    // Apply any ICE candidates that arrived before the remote description was set.
    std::this_thread::sleep_for(std::chrono::milliseconds(100));
    for (const auto& candidate : session->pending_candidates) {
      session->peer_connection->addRemoteCandidate(candidate);
    }
    session->pending_candidates.clear();

    // 7. Trigger SDP answer generation and wait for the result.
    try {
      session->peer_connection->setLocalDescription(rtc::Description::Type::Answer);
    } catch (const std::exception& e) {
      // Non-fatal: libdatachannel sometimes throws but the callback still fires.
      spdlog::warn("[WebRTC:{}] setLocalDescription threw (may be non-fatal): {}", session_id,
                   e.what());
    }

    if (!WaitForSdpAnswer(session_id, answer_future, *session->peer_connection, sdp_answer_str)) {
      try {
        std::lock_guard<std::mutex> sl(session->mutex);
        if (session->data_channel)
          session->data_channel->close();
        if (session->peer_connection)
          session->peer_connection->close();
      } catch (const std::exception& e) {
        spdlog::warn("[WebRTC:{}] Error cleaning up failed session: {}", session_id, e.what());
      }
      RemoveSession(session_id);
      if (error_message) {
        *error_message = "Timeout waiting for SDP answer after " +
                         std::to_string(std::chrono::seconds(10).count()) + " seconds";
      }
      return false;
    }

    spdlog::info("[WebRTC:{}] Session created successfully (answer length: {})", session_id,
                 sdp_answer_str ? sdp_answer_str->length() : 0);
    spdlog::debug("[WebRTC:{}] SDP answer preview: {}", session_id,
                  sdp_answer_str
                      ? sdp_answer_str->substr(0, std::min(size_t(500), sdp_answer_str->length()))
                      : "");

    // 8. Commit the session.
    sessions_[session_id] = session;
    return true;
  } catch (const std::exception& e) {
    if (error_message)
      *error_message = "Failed to create WebRTC session: " + std::string(e.what());
    spdlog::error("[WebRTC:{}] Failed to create session: {}", session_id, e.what());
    RemoveSession(session_id);
    return false;
  }
}

// ---------------------------------------------------------------------------
// CreateCameraSession — like CreateSession but wires GstCameraSource output.
// ---------------------------------------------------------------------------
bool WebRTCManager::CreateCameraSession(const std::string& session_id,
                                        const std::string& sdp_offer_str, int sensor_id, int width,
                                        int height, int fps, std::string* sdp_answer_str,
                                        std::string* error_message) {
  if (!initialized_) {
    if (error_message)
      *error_message = "WebRTC manager not initialized";
    return false;
  }
  if (session_id.empty() || sdp_offer_str.empty()) {
    if (error_message)
      *error_message = "session_id and sdp_offer are required";
    return false;
  }

  std::lock_guard<std::mutex> lock(sessions_mutex_);
  if (sessions_.count(session_id)) {
    if (error_message)
      *error_message = "Session with ID " + session_id + " already exists";
    return false;
  }

  try {
    auto session = std::make_shared<SessionState>();
    session->created_at = std::chrono::steady_clock::now();
    session->last_heartbeat = std::chrono::steady_clock::now();
    session->peer_connection = std::make_shared<rtc::PeerConnection>(*config_);

    auto manual_candidate_ptr = std::make_shared<std::string>(BuildManualCandidateSdp(session_id));

    auto answer_future =
        SetupPeerConnectionCallbacks(session_id, session, manual_candidate_ptr, sdp_answer_str);

    SetupDataChannels(session_id, session);

    const std::string sanitized_sdp = StripRtpHeaderExtensions(sdp_offer_str);
    rtc::Description offer(sanitized_sdp, rtc::Description::Type::Offer);
    try {
      session->peer_connection->setRemoteDescription(offer);
    } catch (const std::exception& e) {
      if (error_message)
        *error_message = std::string("Failed to set remote description: ") + e.what();
      return false;
    }

    if (!SetupMediaTracks(session_id, session, offer, error_message)) {
      return false;
    }

    std::this_thread::sleep_for(std::chrono::milliseconds(100));
    for (const auto& candidate : session->pending_candidates) {
      session->peer_connection->addRemoteCandidate(candidate);
    }
    session->pending_candidates.clear();

    try {
      session->peer_connection->setLocalDescription(rtc::Description::Type::Answer);
    } catch (const std::exception& e) {
      spdlog::warn("[WebRTC:{}] setLocalDescription threw (may be non-fatal): {}", session_id,
                   e.what());
    }

    if (!WaitForSdpAnswer(session_id, answer_future, *session->peer_connection, sdp_answer_str)) {
      if (error_message)
        *error_message = "Timeout waiting for SDP answer";
      return false;
    }

    session->gst_camera_source = std::make_unique<jrb::adapters::camera::GstCameraSource>();
    session->gst_camera_source->SetFrameCallback(
        [weak_self = weak_from_this(), sid = session_id](rtc::binary data, rtc::FrameInfo info) {
          auto mgr = weak_self.lock();
          if (!mgr)
            return;
          auto s = mgr->GetSession(sid);
          if (!s || !s->outbound_video_track)
            return;
          if (!s->outbound_video_track->isOpen())
            return;
          try {
            s->outbound_video_track->sendFrame(std::move(data), info);
          } catch (const std::exception& e) {
            spdlog::warn("[Camera:{}] Frame send failed: {}", sid, e.what());
          }
        });

    std::string cam_err;
    if (!session->gst_camera_source->Start(sensor_id, width, height, fps, &cam_err)) {
      if (error_message)
        *error_message = "Failed to start camera: " + cam_err;
      spdlog::error("[WebRTC:{}] Failed to start camera sensor_id={}: {}", session_id, sensor_id,
                    cam_err);
      return false;
    }

    spdlog::info("[WebRTC:{}] Camera session created (sensor_id={}, {}x{}@{}fps)", session_id,
                 sensor_id, width, height, fps);

    sessions_[session_id] = session;
    return true;
  } catch (const std::exception& e) {
    if (error_message)
      *error_message = "Failed to create camera WebRTC session: " + std::string(e.what());
    spdlog::error("[WebRTC:{}] Failed to create camera session: {}", session_id, e.what());
    RemoveSession(session_id);
    return false;
  }
}

// ---------------------------------------------------------------------------
// StopCameraSession
// ---------------------------------------------------------------------------
bool WebRTCManager::StopCameraSession(const std::string& session_id, std::string* error_message) {
  auto session = GetSession(session_id);
  if (!session) {
    if (error_message)
      *error_message = "Session not found: " + session_id;
    return false;
  }
  if (session->gst_camera_source) {
    session->gst_camera_source->Stop();
  }
  return CloseSession(session_id, error_message);
}

std::shared_future<std::string> WebRTCManager::SetupPeerConnectionCallbacks(
    const std::string& session_id, std::shared_ptr<SessionState> session,
    std::shared_ptr<std::string> manual_candidate_sdp, std::string* sdp_answer_out) {
  auto answer_promise = std::make_shared<std::promise<std::string>>();
  std::shared_future<std::string> answer_future = answer_promise->get_future();

  session->peer_connection->onStateChange([session_id](rtc::PeerConnection::State state) {
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
    spdlog::debug("[WebRTC:{}] Peer connection state changed: {} ({})", session_id, state_str,
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
        spdlog::debug("[WebRTC:{}] ICE gathering state changed: {} ({})", session_id, state_str,
                      static_cast<int>(state));
      });

  session->peer_connection->onIceStateChange([session_id](rtc::PeerConnection::IceState state) {
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
    spdlog::debug("[WebRTC:{}] ICE state changed: {} ({})", session_id, state_str,
                  static_cast<int>(state));
  });

  session->peer_connection->onLocalDescription([session_id, sdp_answer_out, answer_promise,
                                                manual_candidate_sdp](
                                                   rtc::Description description) {
    spdlog::debug("[WebRTC:{}] Local description created (type: {})", session_id,
                  description.typeString());
    if (description.type() != rtc::Description::Type::Answer) {
      spdlog::warn("[WebRTC:{}] Local description is not Answer type: {}", session_id,
                   description.typeString());
      return;
    }
    std::string sdp = description.generateSdp();

    // Inject manual ICE candidate into SDP if configured.
    if (!manual_candidate_sdp->empty()) {
      spdlog::info("[WebRTC:{}] Injecting manual ICE candidate into SDP", session_id);
      const size_t media_pos = sdp.find("m=");
      if (media_pos != std::string::npos) {
        // Insert before the second media section (or at end) so the candidate
        // becomes its own properly delimited line that Chrome will accept.
        const size_t next_media = sdp.find("\r\nm=", media_pos + 2);
        const size_t insert_pos = (next_media == std::string::npos) ? sdp.size() : next_media + 2;
        sdp.insert(insert_pos, *manual_candidate_sdp);
        spdlog::info("[WebRTC:{}] Manual ICE candidate injected (SDP length: {} -> {})", session_id,
                     description.generateSdp().length(), sdp.length());
      } else {
        spdlog::warn("[WebRTC:{}] Could not find media section in SDP, candidate not injected",
                     session_id);
      }
    }

    if (sdp_answer_out != nullptr && sdp_answer_out->empty()) {
      *sdp_answer_out = sdp;
    }
    try {
      answer_promise->set_value(sdp);
      spdlog::debug("[WebRTC:{}] SDP answer generated and stored (length: {})", session_id,
                    sdp.length());
    } catch (const std::future_error& e) {
      spdlog::warn("[WebRTC:{}] Promise already set: {}", session_id, e.what());
    }
  });

  session->peer_connection->onLocalCandidate([session_id, session](rtc::Candidate candidate) {
    spdlog::debug("[WebRTC:{}] Local ICE candidate: {}", session_id, candidate.candidate());
    {
      std::lock_guard<std::mutex> lock(session->candidates_mutex);
      session->local_candidates_queue.push(candidate);
    }
    session->candidates_cv.notify_one();
  });

  // Register the onTrack callback before setRemoteDescription so that both the
  // pre-captured outbound SendOnly track and the inbound RecvOnly track are handled.
  session->peer_connection->onTrack([weak_self = weak_from_this(), session_id,
                                     session](std::shared_ptr<rtc::Track> track) {
    if (track == nullptr) {
      return;
    }
    const auto description = track->description();
    spdlog::debug("[WebRTC:{}] Remote track received (mid={}, type={}, direction={})", session_id,
                  track->mid(), description.type(), static_cast<int>(description.direction()));

    if (description.type() != "video") {
      return;
    }

    std::lock_guard<std::mutex> lock(session->mutex);

    // SendOnly from C++'s perspective = outbound processed video track (browser recvonly).
    // Pre-capture the shared_ptr here so the impl::Track weak_ptr in mTracks stays valid
    // when addTrack() is called, allowing openTracks() to find and open the track.
    if (description.direction() == rtc::Description::Direction::SendOnly) {
      if (session->outbound_video_track == nullptr) {
        session->outbound_video_track = track;
        spdlog::debug("[WebRTC:{}] Outbound video track pre-captured in onTrack (mid={})",
                      session_id, track->mid());
      }
      return;
    }

    if (session->inbound_video_track != nullptr) {
      spdlog::warn("[WebRTC:{}] Ignoring additional inbound video track with mid={}", session_id,
                   track->mid());
      return;
    }

    auto self = weak_self.lock();
    if (self)
      self->SetupInboundTrackCallbacks(session_id, session, track);
  });

  return answer_future;
}

// ---------------------------------------------------------------------------
// SetupMediaTracks
// ---------------------------------------------------------------------------
bool WebRTCManager::SetupMediaTracks(const std::string& session_id,
                                     std::shared_ptr<SessionState> session,
                                     const rtc::Description& offer, std::string* error_message) {
  const auto outbound_video_config = FindOutboundVideoConfig(offer);
  if (!outbound_video_config.has_value()) {
    spdlog::warn(
        "[WebRTC:{}] Offer does not include a recvonly H264 video transceiver; "
        "processed remote track will not be negotiated",
        session_id);
    return true;
  }

  try {
    rtc::Description::Video media(outbound_video_config->mid,
                                  rtc::Description::Direction::SendOnly);
    media.addH264Codec(outbound_video_config->payload_type);
    media.setBitrate(static_cast<int>(kProcessedVideoBitrate));

    const uint32_t ssrc = MakeSsrc(session_id);
    media.addSSRC(ssrc, kProcessedVideoTrackLabel, session_id, kProcessedVideoTrackLabel);

    session->outbound_video_track = session->peer_connection->addTrack(media);
    session->outbound_rtp_config = std::make_shared<rtc::RtpPacketizationConfig>(
        ssrc, kProcessedVideoTrackLabel, static_cast<uint8_t>(outbound_video_config->payload_type),
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
      spdlog::info("[WebRTC:{}] Outbound processed video track opened (mid={})", session_id, mid);
    });
    session->outbound_video_track->onClosed([session_id, mid = outbound_video_config->mid]() {
      spdlog::warn("[WebRTC:{}] Outbound processed video track closed (mid={})", session_id, mid);
    });
    return true;
  } catch (const std::exception& e) {
    spdlog::error("[WebRTC:{}] Failed to create outbound processed video track: {}", session_id,
                  e.what());
    if (error_message) {
      *error_message = std::string("Failed to create outbound processed video track: ") + e.what();
    }
    return false;
  }
}

// ---------------------------------------------------------------------------
// SetupDataChannels — dispatcher
// ---------------------------------------------------------------------------
void WebRTCManager::SetupDataChannels(const std::string& session_id,
                                      std::shared_ptr<SessionState> session) {
  session->peer_connection->onDataChannel(
      [weak_self = weak_from_this(), session_id, session](std::shared_ptr<rtc::DataChannel> dc) {
        spdlog::debug("[WebRTC:{}] Remote data channel received: {}", session_id, dc->label());
        auto self = weak_self.lock();
        if (!self)
          return;

        const std::string& label = dc->label();
        if (label == kDetectionChannelLabel)
          self->SetupDetectionChannel(session_id, session, dc);
        else if (label == kControlChannelLabel)
          self->SetupControlChannel(session_id, session, dc);
        else if (label == kStatsChannelLabel)
          self->SetupStatsChannel(session_id, session, dc);
        else
          self->SetupMainChannel(session_id, session, dc);
      });
}

// ---------------------------------------------------------------------------
// SetupDetectionChannel
// ---------------------------------------------------------------------------
void WebRTCManager::SetupDetectionChannel(const std::string& session_id,
                                          std::shared_ptr<SessionState> session,
                                          std::shared_ptr<rtc::DataChannel> dc) {
  {
    std::lock_guard<std::mutex> lock(session->mutex);
    session->detection_channel = dc;
  }
  dc->onOpen([session_id]() { spdlog::debug("[WebRTC:{}] Detection channel opened", session_id); });
  dc->onClosed([session_id, session]() {
    spdlog::warn("[WebRTC:{}] Detection channel closed", session_id);
    std::lock_guard<std::mutex> lock(session->mutex);
    session->detection_channel = nullptr;
  });
  dc->onError([session_id](std::string err) {
    spdlog::error("[WebRTC:{}] Detection channel error: {}", session_id, err);
  });
}

// ---------------------------------------------------------------------------
// SetupControlChannel
// ---------------------------------------------------------------------------
void WebRTCManager::SetupControlChannel(const std::string& session_id,
                                        std::shared_ptr<SessionState> session,
                                        std::shared_ptr<rtc::DataChannel> dc) {
  {
    std::lock_guard<std::mutex> lock(session->mutex);
    session->control_channel = dc;
  }
  dc->onOpen([session_id]() { spdlog::debug("[WebRTC:{}] Control channel opened", session_id); });
  dc->onClosed([session_id, session]() {
    spdlog::warn("[WebRTC:{}] Control channel closed", session_id);
    std::lock_guard<std::mutex> lock(session->mutex);
    session->control_channel = nullptr;
  });
  dc->onError([session_id](std::string err) {
    spdlog::error("[WebRTC:{}] Control channel error: {}", session_id, err);
  });
  std::weak_ptr<rtc::DataChannel> weak_dc = dc;
  dc->onMessage(
      [weak_self = weak_from_this(), weak_dc, session_id, session](rtc::message_variant data) {
        auto self = weak_self.lock();
        auto ch = weak_dc.lock();
        if (!self || !ch)
          return;
        if (!std::holds_alternative<rtc::binary>(data)) {
          spdlog::warn("[WebRTC:{}] Control channel: ignoring non-binary message", session_id);
          return;
        }
        self->HandleControlMessage(session_id, *session, std::get<rtc::binary>(data), *ch);
      });
}

// ---------------------------------------------------------------------------
// SetupStatsChannel
// ---------------------------------------------------------------------------
void WebRTCManager::SetupStatsChannel(const std::string& session_id,
                                      std::shared_ptr<SessionState> session,
                                      std::shared_ptr<rtc::DataChannel> dc) {
  {
    std::lock_guard<std::mutex> lock(session->mutex);
    session->stats_channel = dc;
  }
  dc->onOpen([session_id]() { spdlog::debug("[WebRTC:{}] Stats channel opened", session_id); });
  dc->onClosed([session_id, session]() {
    spdlog::warn("[WebRTC:{}] Stats channel closed", session_id);
    std::lock_guard<std::mutex> lock(session->mutex);
    session->stats_channel = nullptr;
  });
  dc->onError([session_id](std::string err) {
    spdlog::error("[WebRTC:{}] Stats channel error: {}", session_id, err);
  });
}

// ---------------------------------------------------------------------------
// SetupMainChannel
// ---------------------------------------------------------------------------
void WebRTCManager::SetupMainChannel(const std::string& session_id,
                                     std::shared_ptr<SessionState> session,
                                     std::shared_ptr<rtc::DataChannel> dc) {
  session->data_channel = dc;
  const std::string label = dc->label();
  std::weak_ptr<rtc::DataChannel> weak_dc = dc;

  dc->onOpen([weak_self = weak_from_this(), weak_dc, session_id, label, session]() {
    auto self = weak_self.lock();
    auto ch = weak_dc.lock();
    if (!self || !ch)
      return;
    {
      std::lock_guard<std::mutex> lock(session->mutex);
      session->last_heartbeat = std::chrono::steady_clock::now();
    }
    if (ShouldRegisterSessionChannel(session_id, label)) {
      self->RegisterSessionChannel(session_id, ch);
    }
    spdlog::info("[WebRTC:{}] Main data channel opened (isOpen={})", session_id, ch->isOpen());
  });
  dc->onMessage(
      [weak_self = weak_from_this(), weak_dc, session_id, session](rtc::message_variant data) {
        auto self = weak_self.lock();
        auto ch = weak_dc.lock();
        if (!self || !ch)
          return;
        if (!std::holds_alternative<rtc::binary>(data)) {
          spdlog::warn("[WebRTC:{}] Ignoring non-binary data channel message", session_id);
          return;
        }
        self->HandleProcessingMessage(session_id, *session, std::get<rtc::binary>(data), *ch);
      });
  dc->onClosed([weak_self = weak_from_this(), session_id, label]() {
    auto self = weak_self.lock();
    if (!self)
      return;
    if (ShouldRegisterSessionChannel(session_id, label)) {
      self->UnregisterSessionChannel(session_id);
    }
    spdlog::warn("[WebRTC:{}] Main data channel closed", session_id);
  });
  dc->onError([session_id](std::string err) {
    spdlog::error("[WebRTC:{}] Main data channel error: {}", session_id, err);
  });
}

// ---------------------------------------------------------------------------
// SetupInboundTrackCallbacks
// ---------------------------------------------------------------------------
void WebRTCManager::SetupInboundTrackCallbacks(const std::string& session_id,
                                               std::shared_ptr<SessionState> session,
                                               std::shared_ptr<rtc::Track> track) {
  // incomingChain processes in reverse: rtcp_session validates RTP first,
  // then depacketizer assembles the NAL frame.
  session->inbound_video_track = track;
  session->inbound_rtcp_session = std::make_shared<rtc::RtcpReceivingSession>();
  session->inbound_depacketizer = std::make_shared<rtc::H264RtpDepacketizer>();
  session->inbound_depacketizer->addToChain(session->inbound_rtcp_session);
  track->setMediaHandler(session->inbound_depacketizer);

  track->onOpen([session_id, mid = track->mid()]() {
    spdlog::info("[WebRTC:{}] Inbound video track opened (mid={})", session_id, mid);
  });
  track->onClosed([session_id, mid = track->mid()]() {
    spdlog::warn("[WebRTC:{}] Inbound video track closed (mid={})", session_id, mid);
  });
  track->onFrame(
      [weak_self = weak_from_this(), session_id, session](rtc::binary frame, rtc::FrameInfo info) {
        auto self = weak_self.lock();
        if (!self)
          return;
        self->HandleVideoFrame(session_id, *session, std::move(frame), info);
      });
}

// ---------------------------------------------------------------------------
// EmitProcessingStats
// ---------------------------------------------------------------------------
void WebRTCManager::EmitProcessingStats(const std::string& session_id, SessionState& state,
                                        double elapsed_ms, int64_t detection_count,
                                        uint32_t frame_id) {
  std::shared_ptr<rtc::DataChannel> stats_ch;
  {
    std::lock_guard<std::mutex> lock(state.mutex);
    stats_ch = state.stats_channel;
  }
  if (!stats_ch || !stats_ch->isOpen())
    return;

  ProcessingStatsFrame stats_frame;
  stats_frame.set_session_id(session_id);
  stats_frame.set_frame_id(frame_id);
  stats_frame.set_capture_ts_unix_ms(std::chrono::duration_cast<std::chrono::milliseconds>(
                                         std::chrono::system_clock::now().time_since_epoch())
                                         .count());

  auto* total_ms = stats_frame.add_metrics();
  total_ms->set_key("pipeline.total.ms");
  total_ms->set_unit("ms");
  total_ms->mutable_value()->set_double_value(elapsed_ms);

  auto* det_count = stats_frame.add_metrics();
  det_count->set_key("detections.count");
  det_count->set_unit("count");
  det_count->mutable_value()->set_int64_value(detection_count);

  std::string payload;
  if (stats_frame.SerializeToString(&payload)) {
    SendFramed(*stats_ch, payload,
               state.outgoing_message_id.fetch_add(1, std::memory_order_relaxed) + 1U);
  } else {
    spdlog::error("[WebRTC:{}] Failed to serialize ProcessingStatsFrame", session_id);
  }
}

// ---------------------------------------------------------------------------
// ForwardDetections
// ---------------------------------------------------------------------------
void WebRTCManager::ForwardDetections(const std::string& session_id, SessionState& state,
                                      const DetectionFrame& frame) {
  if (frame.detections_size() == 0)
    return;

  std::shared_ptr<rtc::DataChannel> det_ch;
  {
    std::lock_guard<std::mutex> lock(state.mutex);
    det_ch = state.detection_channel;
  }
  if (!det_ch || !det_ch->isOpen())
    return;

  std::string payload;
  if (frame.SerializeToString(&payload)) {
    SendFramed(*det_ch, payload,
               state.outgoing_message_id.fetch_add(1, std::memory_order_relaxed) + 1U);
  } else {
    spdlog::error("[WebRTC:{}] Failed to serialize DetectionFrame", session_id);
  }
}

// ---------------------------------------------------------------------------
// HandleControlMessage
// ---------------------------------------------------------------------------
void WebRTCManager::HandleControlMessage(const std::string& session_id, SessionState& state,
                                         const rtc::binary& raw_chunk,
                                         rtc::DataChannel& response_channel) {
  const auto raw_span = std::span<const std::byte>(raw_chunk.data(), raw_chunk.size());
  auto maybe_payload = state.incoming_reassembler->PushChunk(raw_span);
  if (!maybe_payload.has_value()) {
    return;
  }
  const auto& assembled = maybe_payload.value();

  ControlRequest request;
  if (!request.ParseFromArray(assembled.data(), static_cast<int>(assembled.size()))) {
    spdlog::warn("[WebRTC:{}] Control: failed to parse ControlRequest ({} bytes)", session_id,
                 assembled.size());
    return;
  }

  ControlResponse response;
  response.set_request_id(request.request_id());
  if (request.has_trace_context()) {
    *response.mutable_trace_context() = request.trace_context();
  }

  dispatcher_->Dispatch(request, session_id, state, &response);

  std::string out;
  if (!response.SerializeToString(&out)) {
    spdlog::error("[WebRTC:{}] Control: failed to serialize ControlResponse", session_id);
    return;
  }
  if (!response_channel.isOpen()) {
    spdlog::warn("[WebRTC:{}] Control: cannot send response, channel closed", session_id);
    return;
  }
  SendFramed(response_channel, out,
             state.outgoing_message_id.fetch_add(1, std::memory_order_relaxed) + 1U);
}

// ---------------------------------------------------------------------------
// HandleProcessingMessage
// ---------------------------------------------------------------------------
void WebRTCManager::HandleProcessingMessage(const std::string& session_id, SessionState& state,
                                            const rtc::binary& raw_chunk,
                                            rtc::DataChannel& response_channel) {
  {
    std::lock_guard<std::mutex> lock(state.mutex);
    state.last_heartbeat = std::chrono::steady_clock::now();
  }

  if (engine_ == nullptr) {
    spdlog::error("[WebRTC:{}] Cannot process frame: processor engine unavailable", session_id);
    return;
  }

  const auto raw_span = std::span<const std::byte>(raw_chunk.data(), raw_chunk.size());
  auto maybe_payload = state.incoming_reassembler->PushChunk(raw_span);
  if (!maybe_payload.has_value()) {
    return;
  }
  const auto& assembled = maybe_payload.value();

  ProcessImageRequest request;
  bool is_keepalive = false;
  if (!ParseDataChannelRequest(assembled, &request, &is_keepalive)) {
    spdlog::warn("[WebRTC:{}] Failed to parse DataChannelRequest ({} bytes)", session_id,
                 assembled.size());
    return;
  }
  if (is_keepalive) {
    spdlog::debug("[WebRTC:{}] Keepalive message received", session_id);
    return;
  }

  const bool is_control_update = request.image_data().empty() && request.width() == 0 &&
                                 request.height() == 0 && request.channels() == 0;
  if (is_control_update) {
    if (state.live_video_processor == nullptr) {
      spdlog::error("[WebRTC:{}] Cannot apply live filter update: video processor missing",
                    session_id);
      return;
    }
    std::lock_guard<std::mutex> media_lock(state.media_mutex);
    std::string err;
    if (!state.live_video_processor->UpdateFilterState(request, &state.live_filter_state, &err)) {
      spdlog::error("[WebRTC:{}] Failed to update live filter state: {}", session_id, err);
      return;
    }
    spdlog::info("[WebRTC:{}] Live camera filter state updated (generic_filters={})", session_id,
                 state.live_filter_state.generic_filters_size());
    return;
  }

  ProcessImageRequest resolved = request;
  ResolveGenericSelectionsInPlace(&resolved);

  ProcessImageResponse response;
  CopyProcessMetadata(resolved, &response);

  const auto process_started = std::chrono::steady_clock::now();
  const bool ok = engine_->ProcessImage(resolved, &response, state.memory_pool.get());
  if (!ok || response.code() != 0) {
    spdlog::warn("[WebRTC:{}] DataChannel frame processing failed (code={}): {}", session_id,
                 response.code(), response.message());
  }
  response.set_frame_id(request.frame_id());

  // Capture from static-image pipeline: consume any pending capture and write the
  // processed frame as a BMP. The live-camera path writes captures inside ProcessAccessUnit;
  // static images never call ProcessAccessUnit so the capture would otherwise be lost.
  if (state.live_video_processor != nullptr) {
    const std::string capture_path = state.live_video_processor->ConsumePendingCapture();
    if (!capture_path.empty() && !response.image_data().empty() && response.width() > 0 &&
        response.height() > 0) {
      const auto& img = response.image_data();
      const int channels = response.channels() > 0 ? response.channels() : 3;
      image_sink_->writeBmp(capture_path.c_str(), reinterpret_cast<const uint8_t*>(img.data()),
                            response.width(), response.height(), channels);
      spdlog::info("[WebRTC:{}] CaptureFrame(static) saved: {}", session_id, capture_path);
    }
  }

  std::string output;
  if (!response.SerializeToString(&output)) {
    spdlog::error("[WebRTC:{}] Failed to serialize ProcessImageResponse", session_id);
    return;
  }
  if (!response_channel.isOpen()) {
    spdlog::warn("[WebRTC:{}] Cannot send ProcessImageResponse: data channel closed", session_id);
    return;
  }
  SendFramed(response_channel, output,
             state.outgoing_message_id.fetch_add(1, std::memory_order_relaxed) + 1U);

  const auto elapsed_ms = std::chrono::duration_cast<std::chrono::microseconds>(
                              std::chrono::steady_clock::now() - process_started)
                              .count() /
                          1000.0;
  EmitProcessingStats(session_id, state, elapsed_ms, response.detections_size(),
                      request.frame_id());

  DetectionFrame det_frame;
  det_frame.set_frame_id(request.frame_id());
  det_frame.set_image_width(request.width());
  det_frame.set_image_height(request.height());
  for (const auto& d : response.detections()) {
    *det_frame.add_detections() = d;
  }
  ForwardDetections(session_id, state, det_frame);
}

// ---------------------------------------------------------------------------
// HandleVideoFrame
// ---------------------------------------------------------------------------
void WebRTCManager::HandleVideoFrame(const std::string& session_id, SessionState& state,
                                     rtc::binary frame, rtc::FrameInfo info) {
  {
    std::lock_guard<std::mutex> heartbeat_lock(state.mutex);
    state.last_heartbeat = std::chrono::steady_clock::now();
  }

  std::lock_guard<std::mutex> media_lock(state.media_mutex);

  const int frame_num = ++state.frame_count;
  if (frame_num <= 5) {
    spdlog::info("[WebRTC:{}] onFrame fired (#{}) size={} processor={} outbound={} open={}",
                 session_id, frame_num, frame.size(), state.live_video_processor != nullptr,
                 state.outbound_video_track != nullptr,
                 state.outbound_video_track ? state.outbound_video_track->isOpen() : false);
  } else {
    SPDLOG_TRACE("[WebRTC:{}] onFrame fired (#{}) size={} processor={} outbound={} open={}",
                 session_id, frame_num, frame.size(), state.live_video_processor != nullptr,
                 state.outbound_video_track != nullptr,
                 state.outbound_video_track ? state.outbound_video_track->isOpen() : false);
  }

  if (state.live_video_processor == nullptr || state.outbound_video_track == nullptr)
    return;
  if (!state.outbound_video_track->isOpen())
    return;

  const auto process_started = std::chrono::steady_clock::now();
  std::vector<EncodedAccessUnit> encoded_units;
  DetectionFrame detection_frame;
  std::string error_msg;
  const bool ok = state.live_video_processor->ProcessAccessUnit(
      frame, info, state.live_filter_state, &encoded_units, &detection_frame, &error_msg);
  if (!ok) {
    // Log the first few failures, then every 300th (~10s at 30fps). An
    // undecodable stream fails on every single frame, and the logger flushes
    // synchronously to file, stdout and OTLP on each record.
    const int failures = ++state.live_failure_count;
    if (failures <= 3 || failures % 300 == 0) {
      spdlog::error("[WebRTC:{}] Live camera frame processing failed (x{}): {}", session_id,
                    failures, error_msg);
    }
    return;
  }
  state.live_failure_count.store(0);

  if (frame_num <= 5) {
    spdlog::info("[WebRTC:{}] Frame #{} processed OK, {} encoded units", session_id, frame_num,
                 encoded_units.size());
  } else {
    SPDLOG_TRACE("[WebRTC:{}] Frame #{} processed OK, {} encoded units", session_id, frame_num,
                 encoded_units.size());
  }

  for (const auto& unit : encoded_units) {
    try {
      state.outbound_video_track->sendFrame(unit.data, unit.frame_info);
    } catch (const std::exception& e) {
      spdlog::error("[WebRTC:{}] Failed to send processed video frame: {}", session_id, e.what());
      return;
    }
  }

  const auto elapsed_ms = std::chrono::duration_cast<std::chrono::microseconds>(
                              std::chrono::steady_clock::now() - process_started)
                              .count() /
                          1000.0;
  EmitProcessingStats(session_id, state, elapsed_ms, detection_frame.detections_size(), 0);
  ForwardDetections(session_id, state, detection_frame);
}

// ---------------------------------------------------------------------------
// Remaining public methods (unchanged in logic)
// ---------------------------------------------------------------------------
bool WebRTCManager::CloseSession(const std::string& session_id, std::string* error_message) {
  auto session = GetSession(session_id);
  if (!session) {
    if (error_message)
      *error_message = "Session with ID " + session_id + " not found";
    return false;
  }

  try {
    std::lock_guard<std::mutex> lock(session->mutex);

    if (session->memory_pool) {
      session->memory_pool->Clear();
      spdlog::info("[WebRTC:{}] CUDA memory pool cleared", session_id);
    }
    CloseSessionTransport(session_id, *session);

    RemoveSession(session_id);
    spdlog::info("[WebRTC:{}] Session closed successfully", session_id);
    return true;
  } catch (const std::exception& e) {
    if (error_message)
      *error_message = "Failed to close session: " + std::string(e.what());
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
    if (error_message)
      *error_message = "Session with ID " + session_id + " not found";
    return false;
  }

  try {
    rtc::Candidate candidate(candidate_str, sdp_mid);
    std::lock_guard<std::mutex> lock(session->mutex);

    // Drop late candidates once the peer connection is already connected to avoid
    // libdatachannel/juice resetting the ICE agent and tearing down the transport.
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
    if (error_message)
      *error_message = "Failed to add remote ICE candidate: " + std::string(e.what());
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
  const uint32_t msg_id =
      session ? session->outgoing_message_id.fetch_add(1, std::memory_order_relaxed) + 1U : 1U;
  SendFramed(*channel, bytes, msg_id);

  if (session) {
    std::lock_guard<std::mutex> lock(session->mutex);
    session->last_heartbeat = std::chrono::steady_clock::now();
  }
}

std::shared_ptr<SessionState> WebRTCManager::GetSession(const std::string& session_id) {
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

void WebRTCManager::CloseSessionTransport(const std::string& session_id, SessionState& session) {
  try {
    if (session.data_channel)
      session.data_channel->close();
    if (session.detection_channel)
      session.detection_channel->close();
    if (session.stats_channel)
      session.stats_channel->close();
    if (session.control_channel)
      session.control_channel->close();
    if (session.peer_connection)
      session.peer_connection->close();
  } catch (const std::exception& e) {
    spdlog::error("[WebRTC:{}] Error closing session transport: {}", session_id, e.what());
  }
}

void WebRTCManager::CleanupInactiveSessions(int timeout_seconds) {
  std::lock_guard<std::mutex> lock(sessions_mutex_);
  const auto now = std::chrono::steady_clock::now();
  const auto timeout_duration = std::chrono::seconds(timeout_seconds);

  std::vector<std::string> sessions_to_remove;
  for (const auto& [session_id, session] : sessions_) {
    std::lock_guard<std::mutex> session_lock(session->mutex);
    const auto elapsed = now - session->last_heartbeat;
    if (elapsed > timeout_duration) {
      spdlog::warn("[WebRTC:{}] Session inactive for {} seconds, closing", session_id,
                   std::chrono::duration_cast<std::chrono::seconds>(elapsed).count());
      sessions_to_remove.push_back(session_id);
      CloseSessionTransport(session_id, *session);
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
  spdlog::debug("[WebRTC:{}] Registered routable browser data channel", session_id);
}

void WebRTCManager::UnregisterSessionChannel(const std::string& session_id) {
  std::lock_guard<std::mutex> lock(session_channels_mutex_);
  session_channels_.erase(session_id);
}

}  // namespace jrb::adapters::webrtc
