#include "src/cpp_accelerator/ports/grpc/accelerator_control_client.h"

#include <chrono>
#include <fstream>
#include <random>
#include <sstream>
#include <string>
#include <thread>
#include <vector>

#include <spdlog/spdlog.h>

#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wdeprecated-declarations"
#include <grpcpp/security/credentials.h>
#pragma GCC diagnostic pop

#include "proto/_virtual_imports/common_proto/common.pb.h"
#include "proto/_virtual_imports/image_processor_service_proto/image_processor_service.pb.h"

namespace jrb::ports::grpc_service {

namespace {

std::string ReadFile(const std::string& path) {
  std::ifstream f(path);
  if (!f.is_open()) {
    return {};
  }
  std::ostringstream ss;
  ss << f.rdbuf();
  return ss.str();
}

std::string NewCommandId() {
  // UUID v4 format using thread-local Mersenne Twister.
  thread_local std::mt19937_64 rng{std::random_device{}()};
  std::uniform_int_distribution<uint64_t> dist;
  uint64_t hi = dist(rng);
  uint64_t lo = dist(rng);
  // Set version (4) and variant bits.
  hi = (hi & 0xFFFFFFFFFFFF0FFFULL) | 0x0000000000004000ULL;
  lo = (lo & 0x3FFFFFFFFFFFFFFFULL) | 0x8000000000000000ULL;
  char buf[37];
  snprintf(buf, sizeof(buf),
           "%08x-%04x-%04x-%04x-%012llx",
           static_cast<uint32_t>(hi >> 32),
           static_cast<uint16_t>(hi >> 16),
           static_cast<uint16_t>(hi),
           static_cast<uint16_t>(lo >> 48),
           static_cast<unsigned long long>(lo & 0x0000FFFFFFFFFFFFULL));
  return buf;
}

}  // namespace

AcceleratorControlClient::AcceleratorControlClient(
    AcceleratorControlClientConfig config,
    std::shared_ptr<ProcessorEngineProvider> engine,
    std::shared_ptr<WebRTCManager> webrtc_manager)
    : config_(std::move(config)),
      engine_(std::move(engine)),
      webrtc_manager_(std::move(webrtc_manager)) {}

AcceleratorControlClient::~AcceleratorControlClient() {
  Stop();
}

void AcceleratorControlClient::Stop() {
  stop_requested_ = true;
}

void AcceleratorControlClient::Run() {
  spdlog::info("[AcceleratorControl] Starting outbound connection loop to {}",
               config_.control_addr);
  int delay_s = 1;
  while (!stop_requested_) {
    if (RunOnce()) {
      // Clean shutdown.
      break;
    }
    if (stop_requested_) {
      break;
    }
    spdlog::warn("[AcceleratorControl] Reconnecting in {}s...", delay_s);
    for (int i = 0; i < delay_s * 10 && !stop_requested_; ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }
    delay_s = std::min(delay_s * 2, config_.max_reconnect_delay_s);
  }
  spdlog::info("[AcceleratorControl] Connection loop exited");
}

bool AcceleratorControlClient::RunOnce() {
  // Build channel credentials.
  std::shared_ptr<grpc::ChannelCredentials> creds;
  if (!config_.client_cert_file.empty() && !config_.client_key_file.empty() &&
      !config_.ca_cert_file.empty()) {
    std::string cert = ReadFile(config_.client_cert_file);
    std::string key  = ReadFile(config_.client_key_file);
    std::string ca   = ReadFile(config_.ca_cert_file);
    if (cert.empty() || key.empty() || ca.empty()) {
      spdlog::error("[AcceleratorControl] Failed to read mTLS credential files");
      return false;
    }
    grpc::SslCredentialsOptions opts;
    opts.pem_root_certs    = ca;
    opts.pem_cert_chain    = cert;
    opts.pem_private_key   = key;
    creds = grpc::SslCredentials(opts);
    spdlog::info("[AcceleratorControl] mTLS credentials loaded");
  } else {
    spdlog::warn("[AcceleratorControl] No mTLS certs configured — using insecure channel");
    creds = grpc::InsecureChannelCredentials();
  }

  auto channel = grpc::CreateChannel(config_.control_addr, creds);
  auto stub = cuda_learning::AcceleratorControlService::NewStub(channel);

  grpc::ClientContext ctx;
  auto stream = stub->Connect(&ctx);

  // Stash pointers so Send() can use them (cleared on exit).
  {
    std::lock_guard<std::mutex> lk(write_mutex_);
    ctx_    = &ctx;
    stream_ = stream.get();
  }

  auto cleanup = [&]() {
    std::lock_guard<std::mutex> lk(write_mutex_);
    ctx_    = nullptr;
    stream_ = nullptr;
  };

  // Send Register.
  if (!Send(BuildRegisterMessage())) {
    if (ctx_ != nullptr) {
      spdlog::error("[AcceleratorControl] Failed to send Register — {}",
                    ctx_->debug_error_string());
    } else {
      spdlog::error("[AcceleratorControl] Failed to send Register");
    }
    cleanup();
    return stop_requested_;
  }

  // Wait for RegisterAck.
  cuda_learning::ConnectResponse ack_resp;
  if (!stream->Read(&ack_resp)) {
    spdlog::error("[AcceleratorControl] Stream closed before RegisterAck");
    cleanup();
    return stop_requested_;
  }
  const auto& ack_msg = ack_resp.message();
  if (!ack_msg.has_register_ack()) {
    spdlog::error("[AcceleratorControl] Expected RegisterAck, got something else");
    cleanup();
    return stop_requested_;
  }
  if (!ack_msg.register_ack().accepted()) {
    spdlog::error("[AcceleratorControl] Registration rejected: {}",
                  ack_msg.register_ack().reject_reason());
    cleanup();
    return false;
  }
  spdlog::info("[AcceleratorControl] Registered, session_id={}",
               ack_msg.register_ack().assigned_session_id());

  // Start candidate pump thread.
  std::atomic<bool> pump_stop{false};
  std::thread pump_thread([&]() {
    while (!pump_stop.load() && !stop_requested_.load()) {
      CandidatePumpLoop();
      std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }
  });

  // Dispatch loop.
  cuda_learning::ConnectResponse resp;
  while (!stop_requested_ && stream->Read(&resp)) {
    Dispatch(resp.message());
  }

  pump_stop = true;
  pump_thread.join();

  stream->WritesDone();
  auto status = stream->Finish();
  if (!status.ok() && !stop_requested_) {
    spdlog::warn("[AcceleratorControl] Stream ended: {} {}",
                 static_cast<int>(status.error_code()), status.error_message());
  }
  cleanup();
  return stop_requested_;
}

bool AcceleratorControlClient::Send(cuda_learning::AcceleratorMessage msg) {
  if (msg.command_id().empty()) {
    msg.set_command_id(NewCommandId());
  }
  std::lock_guard<std::mutex> lk(write_mutex_);
  if (!stream_) {
    return false;
  }
  cuda_learning::ConnectRequest req;
  *req.mutable_message() = std::move(msg);
  return stream_->Write(req);
}

void AcceleratorControlClient::Dispatch(const cuda_learning::AcceleratorMessage& msg) {
  const std::string& cmd_id = msg.command_id();
  switch (msg.payload_case()) {
    case cuda_learning::AcceleratorMessage::kSignalingMessage:
      HandleSignalingMessage(cmd_id, msg.signaling_message());
      break;
    case cuda_learning::AcceleratorMessage::kKeepalive:
      spdlog::debug("[AcceleratorControl] Keepalive received");
      break;
    default:
      spdlog::warn("[AcceleratorControl] Unknown payload type: {}",
                   static_cast<int>(msg.payload_case()));
      break;
  }
}

void AcceleratorControlClient::HandleSignalingMessage(
    const std::string& command_id, const cuda_learning::SignalingMessage& msg) {
  if (!webrtc_manager_ || !webrtc_manager_->IsInitialized()) {
    spdlog::warn("[AcceleratorControl] WebRTCManager unavailable — dropping signaling message");
    return;
  }

  cuda_learning::SignalingMessage response;

  switch (msg.message_case()) {
    case cuda_learning::SignalingMessage::kStartSession: {
      const auto& req = msg.start_session();
      std::string answer;
      std::string error;
      const bool ok = webrtc_manager_->CreateSession(req.session_id(), req.sdp_offer(),
                                                     &answer, &error);
      auto* resp = response.mutable_start_session_response();
      resp->set_session_id(req.session_id());
      if (ok) {
        resp->set_sdp_answer(answer);
        if (req.has_trace_context()) {
          *resp->mutable_trace_context() = req.trace_context();
        }
      } else {
        resp->set_sdp_answer("");
      }

      // Track session id for candidate pump.
      {
        std::lock_guard<std::mutex> lk(session_ids_mutex_);
        active_session_ids_.push_back(req.session_id());
      }

      spdlog::info("[AcceleratorControl] WebRTC session started: {}", req.session_id());
      break;
    }

    case cuda_learning::SignalingMessage::kIceCandidate: {
      const auto& req = msg.ice_candidate();
      std::string error;
      const bool ok = webrtc_manager_->HandleRemoteCandidate(
          req.session_id(), req.candidate().candidate(), req.candidate().sdp_mid(),
          req.candidate().sdp_mline_index(), &error);
      auto* resp = response.mutable_ice_candidate_response();
      resp->set_session_id(req.session_id());
      resp->set_accepted(ok);
      resp->set_message(ok ? "candidate accepted" : error);
      if (req.has_trace_context()) {
        *resp->mutable_trace_context() = req.trace_context();
      }
      break;
    }

    case cuda_learning::SignalingMessage::kCloseSession: {
      const auto& req = msg.close_session();
      std::string error;
      webrtc_manager_->CloseSession(req.session_id(), &error);
      auto* resp = response.mutable_close_session_response();
      resp->set_session_id(req.session_id());
      resp->set_closed(true);
      if (req.has_trace_context()) {
        *resp->mutable_trace_context() = req.trace_context();
      }

      {
        std::lock_guard<std::mutex> lk(session_ids_mutex_);
        active_session_ids_.erase(
            std::remove(active_session_ids_.begin(), active_session_ids_.end(),
                        req.session_id()),
            active_session_ids_.end());
      }

      spdlog::info("[AcceleratorControl] WebRTC session closed: {}", req.session_id());
      break;
    }

    case cuda_learning::SignalingMessage::kKeepAlive:
      spdlog::debug("[AcceleratorControl] Signaling keepalive");
      return;

    default:
      spdlog::warn("[AcceleratorControl] Unknown signaling message type: {}",
                   static_cast<int>(msg.message_case()));
      return;
  }

  cuda_learning::AcceleratorMessage out;
  out.set_command_id(command_id);
  *out.mutable_signaling_message() = std::move(response);
  if (!Send(std::move(out))) {
    spdlog::warn("[AcceleratorControl] Failed to send SignalingMessage response cmd={}",
                 command_id);
  }
}

void AcceleratorControlClient::CandidatePumpLoop() {
  if (!webrtc_manager_) {
    return;
  }

  std::vector<std::string> session_ids;
  {
    std::lock_guard<std::mutex> lk(session_ids_mutex_);
    session_ids = active_session_ids_;
  }

  for (const auto& session_id : session_ids) {
    auto candidates = webrtc_manager_->GetPendingLocalCandidates(session_id);
    for (const auto& candidate : candidates) {
      cuda_learning::SignalingMessage signaling;
      auto* ice = signaling.mutable_ice_candidate();
      ice->set_session_id(session_id);
      ice->mutable_candidate()->set_candidate(candidate.candidate());
      ice->mutable_candidate()->set_sdp_mid(candidate.mid());
      ice->mutable_candidate()->set_sdp_mline_index(0);

      cuda_learning::AcceleratorMessage out;
      *out.mutable_signaling_message() = std::move(signaling);
      if (!Send(std::move(out))) {
        spdlog::warn("[AcceleratorControl] Failed to send local candidate for session {}",
                     session_id);
        break;
      }
    }
  }
}

cuda_learning::AcceleratorMessage AcceleratorControlClient::BuildRegisterMessage() const {
  cuda_learning::Register reg;
  reg.set_device_id(config_.device_id);
  reg.set_display_name(config_.display_name);
  reg.set_accelerator_version(config_.accelerator_version);

  if (engine_) {
    cuda_learning::GetCapabilitiesResponse caps_resp;
    if (engine_->GetCapabilities(&caps_resp)) {
      *reg.mutable_capabilities() = caps_resp.capabilities();
      for (const auto& f : caps_resp.capabilities().filters()) {
        for (const auto acc : f.supported_accelerators()) {
          bool already_added = false;
          for (auto t : reg.supported_accelerator_types()) {
            if (t == acc) {
              already_added = true;
              break;
            }
          }
          if (!already_added) {
            reg.add_supported_accelerator_types(
                static_cast<cuda_learning::AcceleratorType>(acc));
          }
        }
      }
    }
  }

  cuda_learning::AcceleratorMessage msg;
  msg.set_command_id(NewCommandId());
  *msg.mutable_register_() = std::move(reg);
  return msg;
}

}  // namespace jrb::ports::grpc_service
