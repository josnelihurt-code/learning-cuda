#pragma once

#include <atomic>
#include <future>
#include <memory>
#include <mutex>
#include <string>
#include <thread>
#include <unordered_map>
#include <vector>

#include <rtc/rtc.hpp>

#include "proto/_virtual_imports/image_processor_service_proto/image_processor_service.pb.h"
#include "src/cpp_accelerator/adapters/camera/camera_hub.h"
#include "src/cpp_accelerator/adapters/webrtc/webrtc_session_state.h"
#include "src/cpp_accelerator/application/server_info/i_server_info_provider.h"
#include "src/cpp_accelerator/ports/media/i_media_session.h"

namespace jrb::application::engine {
class ProcessorEngine;
}

namespace jrb::domain::interfaces {
class IImageSink;
}

namespace jrb::adapters::webrtc {

class ControlMessageDispatcher;

struct WebRTCManagerConfig {
  std::shared_ptr<jrb::application::engine::ProcessorEngine> engine;
  std::shared_ptr<jrb::adapters::camera::CameraHub> camera_hub;
  std::shared_ptr<jrb::domain::interfaces::IImageSink> image_sink;
  std::string device_id;
  std::string display_name;
  std::string captures_dir{"/tmp/cuda-captures"};
  std::string accelerator_version;
};

class WebRTCManager : public std::enable_shared_from_this<WebRTCManager>,
                      public jrb::ports::media::IMediaSession {
public:
  explicit WebRTCManager(WebRTCManagerConfig config = {});
  virtual ~WebRTCManager();

  bool Initialize();
  void Shutdown();

  bool IsInitialized() const { return initialized_; }

  // Public egress IP observed by the control server; injected into SDP candidates.
  void SetObservedPublicIp(const std::string& ip);
  std::string observed_public_ip() const;

  bool CreateSession(const std::string& session_id, const std::string& sdp_offer,
                     std::string* sdp_answer, std::string* error_message);

  bool CreateCameraSession(const std::string& session_id, const std::string& sdp_offer,
                           int sensor_id, int width, int height, int fps, std::string* sdp_answer,
                           std::string* error_message);

  bool CloseSession(const std::string& session_id, std::string* error_message);

  bool StopCameraSession(const std::string& session_id, std::string* error_message);

  bool HandleRemoteCandidate(const std::string& session_id, const std::string& candidate,
                             const std::string& sdp_mid, int sdp_mline_index,
                             std::string* error_message);

  std::vector<rtc::Candidate> GetPendingLocalCandidates(const std::string& session_id);
  virtual void SendToSession(const std::string& session_id, const std::string& bytes);

private:
  std::shared_ptr<SessionState> GetSession(const std::string& session_id);
  void RemoveSession(const std::string& session_id);
  void CleanupInactiveSessions(int timeout_seconds = 30);

  // Close every data channel and the peer connection on `session`. Idempotent
  // and exception-safe — single point of truth used by Shutdown(),
  // CloseSession(), and CleanupInactiveSessions(). Caller is responsible for
  // any necessary locking.
  static void CloseSessionTransport(const std::string& session_id, SessionState& session);
  void RegisterSessionChannel(const std::string& session_id,
                              const std::shared_ptr<rtc::DataChannel>& data_channel);
  void UnregisterSessionChannel(const std::string& session_id);

  // Returns a shared_future that will be fulfilled with the SDP answer string.
  // Registers all peer connection state callbacks (onStateChange, onGatheringStateChange,
  // onIceStateChange, onLocalDescription, onLocalCandidate) and the onTrack callback.
  std::shared_future<std::string> SetupPeerConnectionCallbacks(
      const std::string& session_id, std::shared_ptr<SessionState> session,
      const std::string& public_ip, std::string* sdp_answer_out);

  // Adds the outbound H264 video track and configures the RTP packetizer chain
  // based on the parsed SDP offer. Returns false on error.
  bool SetupMediaTracks(const std::string& session_id, std::shared_ptr<SessionState> session,
                        const rtc::Description& offer, std::string* error_message);

  // Registers the onDataChannel callback that routes the four data channels
  // (main, detections, stats, control) to their respective handlers.
  void SetupDataChannels(const std::string& session_id, std::shared_ptr<SessionState> session);

  // Handles a framed binary payload from the control channel.
  void HandleControlMessage(const std::string& session_id, SessionState& state,
                            const rtc::binary& raw_chunk, rtc::DataChannel& response_channel);

  // Handles a framed binary payload from the main data channel (image processing requests).
  void HandleProcessingMessage(const std::string& session_id, SessionState& state,
                               const rtc::binary& raw_chunk, rtc::DataChannel& response_channel);

  // Processes a decoded inbound video frame and sends the result on the outbound track.
  void HandleVideoFrame(const std::string& session_id, SessionState& state, rtc::binary frame,
                        rtc::FrameInfo info);

  // Sets up RTP handlers and callbacks for an inbound RecvOnly video track.
  void SetupInboundTrackCallbacks(const std::string& session_id,
                                  std::shared_ptr<SessionState> session,
                                  std::shared_ptr<rtc::Track> track);

  // Per-channel setup helpers called from SetupDataChannels.
  void SetupDetectionChannel(const std::string& session_id, std::shared_ptr<SessionState> session,
                             std::shared_ptr<rtc::DataChannel> dc);
  void SetupControlChannel(const std::string& session_id, std::shared_ptr<SessionState> session,
                           std::shared_ptr<rtc::DataChannel> dc);
  void SetupStatsChannel(const std::string& session_id, std::shared_ptr<SessionState> session,
                         std::shared_ptr<rtc::DataChannel> dc);
  void SetupMainChannel(const std::string& session_id, std::shared_ptr<SessionState> session,
                        std::shared_ptr<rtc::DataChannel> dc);

  // Serializes and sends a ProcessingStatsFrame to the stats channel if open.
  void EmitProcessingStats(const std::string& session_id, SessionState& state, double elapsed_ms,
                           int64_t detection_count, uint32_t frame_id);

  // Serializes and sends a DetectionFrame to the detection channel if open.
  void ForwardDetections(const std::string& session_id, SessionState& state,
                         const cuda_learning::DetectionFrame& frame);

  std::shared_ptr<jrb::application::engine::ProcessorEngine> engine_;
  std::shared_ptr<jrb::adapters::camera::CameraHub> camera_hub_;
  std::unique_ptr<jrb::application::server_info::IServerInfoProvider> server_info_;
  std::shared_ptr<jrb::domain::interfaces::IImageSink> image_sink_;
  std::string captures_dir_;
  std::unique_ptr<ControlMessageDispatcher> dispatcher_;
  bool initialized_;
  std::unique_ptr<rtc::Configuration> config_;
  std::mutex sessions_mutex_;
  std::unordered_map<std::string, std::shared_ptr<SessionState>> sessions_;
  std::mutex session_channels_mutex_;
  std::unordered_map<std::string, std::weak_ptr<rtc::DataChannel>> session_channels_;
  std::string device_id_;
  std::string display_name_;
  std::atomic<bool> cleanup_running_;
  std::thread cleanup_thread_;
  std::string accelerator_version_;
  mutable std::mutex observed_ip_mutex_;
  std::string observed_public_ip_;
};

}  // namespace jrb::adapters::webrtc
