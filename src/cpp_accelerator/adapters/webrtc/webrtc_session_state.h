#pragma once

#include <atomic>
#include <chrono>
#include <condition_variable>
#include <memory>
#include <mutex>
#include <queue>
#include <vector>

#include <rtc/rtc.hpp>

#include "proto/_virtual_imports/image_processor_service_proto/image_processor_service.pb.h"
#include "src/cpp_accelerator/adapters/camera/camera_hub.h"
#include "src/cpp_accelerator/adapters/camera/gst_camera_source.h"
#include "src/cpp_accelerator/adapters/compute/cuda/memory/cuda_memory_pool.h"
#include "src/cpp_accelerator/adapters/webrtc/data_channel_framing.h"
#include "src/cpp_accelerator/adapters/webrtc/live_video_processor.h"

namespace jrb::adapters::webrtc {

struct SessionState {
  std::shared_ptr<rtc::PeerConnection> peer_connection;
  std::shared_ptr<rtc::DataChannel> data_channel;
  std::shared_ptr<rtc::DataChannel> detection_channel;
  std::shared_ptr<rtc::DataChannel> stats_channel;
  std::shared_ptr<rtc::DataChannel> control_channel;
  std::shared_ptr<rtc::Track> inbound_video_track;
  std::shared_ptr<rtc::Track> outbound_video_track;
  std::shared_ptr<rtc::RtcpReceivingSession> inbound_rtcp_session;
  std::shared_ptr<rtc::H264RtpDepacketizer> inbound_depacketizer;
  std::shared_ptr<rtc::RtpPacketizationConfig> outbound_rtp_config;
  std::shared_ptr<rtc::H264RtpPacketizer> outbound_packetizer;
  std::shared_ptr<rtc::RtcpSrReporter> outbound_sr_reporter;
  std::shared_ptr<rtc::RtcpNackResponder> outbound_nack_responder;
  // Lifetime invariant: memory_pool MUST be declared before
  // live_video_processor so the pool outlives the processor (members are
  // destroyed in reverse declaration order, and the processor holds a raw
  // pointer into the pool).
  std::unique_ptr<jrb::adapters::compute::cuda::CudaMemoryPool> memory_pool;
  std::unique_ptr<LiveVideoProcessor> live_video_processor;
  std::unique_ptr<jrb::adapters::camera::GstCameraSource> gst_camera_source;
  jrb::adapters::camera::CameraHub::Subscription camera_subscription;
  std::unique_ptr<ChunkReassembler> incoming_reassembler =
      std::make_unique<ChunkReassembler>();
  std::atomic<uint32_t> outgoing_message_id{0};
  std::atomic<int> frame_count{0};
  // Consecutive live-processing failures; used to rate-limit the error log so a
  // permanently undecodable stream cannot flood the logging pipeline.
  std::atomic<int> live_failure_count{0};
  cuda_learning::ProcessImageRequest live_filter_state;
  std::vector<rtc::Candidate> pending_candidates;
  std::queue<rtc::Candidate> local_candidates_queue;
  std::mutex mutex;
  std::mutex media_mutex;
  std::mutex candidates_mutex;
  std::condition_variable candidates_cv;
  std::chrono::steady_clock::time_point created_at;
  std::chrono::steady_clock::time_point last_heartbeat;
};

}  // namespace jrb::adapters::webrtc
