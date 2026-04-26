#pragma once

#include <atomic>
#include <chrono>
#include <condition_variable>
#include <memory>
#include <mutex>
#include <queue>
#include <string>
#include <thread>
#include <unordered_map>
#include <vector>

#include <rtc/rtc.hpp>

#include "proto/_virtual_imports/image_processor_service_proto/image_processor_service.pb.h"
#include "src/cpp_accelerator/infrastructure/cuda/cuda_memory_pool.h"
#include "src/cpp_accelerator/ports/grpc/data_channel_framing.h"
#include "src/cpp_accelerator/ports/grpc/live_video_processor.h"

namespace jrb::ports::shared_lib {
class ProcessorEngine;
}

namespace jrb::ports::grpc_service {

class WebRTCManager : public std::enable_shared_from_this<WebRTCManager> {
public:
  explicit WebRTCManager(std::shared_ptr<jrb::ports::shared_lib::ProcessorEngine> engine = nullptr);
  virtual ~WebRTCManager();

  bool Initialize();
  void Shutdown();

  bool IsInitialized() const { return initialized_; }

  bool CreateSession(const std::string& session_id, const std::string& sdp_offer,
                     std::string* sdp_answer, std::string* error_message);

  bool CloseSession(const std::string& session_id, std::string* error_message);

  bool HandleRemoteCandidate(const std::string& session_id, const std::string& candidate,
                             const std::string& sdp_mid, int sdp_mline_index,
                             std::string* error_message);

  std::vector<rtc::Candidate> GetPendingLocalCandidates(const std::string& session_id);
  virtual void SendToSession(const std::string& session_id, const std::string& bytes);

  private:
  struct SessionState {
    std::shared_ptr<rtc::PeerConnection> peer_connection;
    std::shared_ptr<rtc::DataChannel> data_channel;
    std::shared_ptr<rtc::DataChannel> detection_channel;
    std::shared_ptr<rtc::DataChannel> stats_channel;
    std::shared_ptr<rtc::Track> inbound_video_track;
    std::shared_ptr<rtc::Track> outbound_video_track;
    std::shared_ptr<rtc::RtcpReceivingSession> inbound_rtcp_session;
    std::shared_ptr<rtc::H264RtpDepacketizer> inbound_depacketizer;
    std::shared_ptr<rtc::RtpPacketizationConfig> outbound_rtp_config;
    std::shared_ptr<rtc::H264RtpPacketizer> outbound_packetizer;
    std::shared_ptr<rtc::RtcpSrReporter> outbound_sr_reporter;
    std::shared_ptr<rtc::RtcpNackResponder> outbound_nack_responder;
    std::unique_ptr<LiveVideoProcessor> live_video_processor;
    std::unique_ptr<ChunkReassembler> incoming_reassembler =
        std::make_unique<ChunkReassembler>();
    std::atomic<uint32_t> outgoing_message_id{0};
    cuda_learning::ProcessImageRequest live_filter_state;
    std::vector<rtc::Candidate> pending_candidates;
    std::queue<rtc::Candidate> local_candidates_queue;
    std::mutex mutex;
    std::mutex media_mutex;
    std::mutex candidates_mutex;
    std::condition_variable candidates_cv;
    std::chrono::steady_clock::time_point created_at;
    std::chrono::steady_clock::time_point last_heartbeat;
    std::unique_ptr<jrb::infrastructure::cuda::CudaMemoryPool> memory_pool;
  };

  std::shared_ptr<SessionState> GetSession(const std::string& session_id);
  void RemoveSession(const std::string& session_id);
  void CleanupInactiveSessions(int timeout_seconds = 30);
  void RegisterSessionChannel(const std::string& session_id,
                              const std::shared_ptr<rtc::DataChannel>& data_channel);
  void UnregisterSessionChannel(const std::string& session_id);

  std::shared_ptr<jrb::ports::shared_lib::ProcessorEngine> engine_;
  bool initialized_;
  std::unique_ptr<rtc::Configuration> config_;
  std::mutex sessions_mutex_;
  std::unordered_map<std::string, std::shared_ptr<SessionState>> sessions_;
  std::mutex session_channels_mutex_;
  std::unordered_map<std::string, std::weak_ptr<rtc::DataChannel>> session_channels_;
  std::atomic<bool> cleanup_running_;
  std::thread cleanup_thread_;
};

}  // namespace jrb::ports::grpc_service
