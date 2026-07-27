#include "src/cpp_accelerator/adapters/grpc_control/accelerator_control_client.h"

#include <algorithm>
#include <chrono>
#include <cstdlib>
#include <fstream>
#include <random>
#include <sstream>
#include <string>
#include <string_view>
#include <thread>
#include <vector>

#include <spdlog/spdlog.h>

#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wdeprecated-declarations"
#include <grpcpp/channel.h>
#include <grpcpp/security/credentials.h>
#include <grpcpp/support/channel_arguments.h>
#pragma GCC diagnostic pop

#include <grpc/grpc.h>

#include "proto/_virtual_imports/common_proto/common.pb.h"
#include "proto/_virtual_imports/image_processor_service_proto/image_processor_service.pb.h"

namespace jrb::adapters::grpc_control {

using cuda_learning::AcceleratorControlService;
using cuda_learning::AcceleratorMessage;
using cuda_learning::AcceleratorType;
using cuda_learning::ConnectRequest;
using cuda_learning::ConnectResponse;
using cuda_learning::GetCapabilitiesResponse;
using cuda_learning::Register;
using cuda_learning::SignalingMessage;
using jrb::adapters::webrtc::WebRTCManager;
namespace {

constexpr std::string_view kLogPrefix = "[AcceleratorControl]";

int64_t SteadyNowMs() {
  return std::chrono::duration_cast<std::chrono::milliseconds>(
             std::chrono::steady_clock::now().time_since_epoch())
      .count();
}

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
  snprintf(buf, sizeof(buf), "%08x-%04x-%04x-%04x-%012llx", static_cast<uint32_t>(hi >> 32),
           static_cast<uint16_t>(hi >> 16), static_cast<uint16_t>(hi),
           static_cast<uint16_t>(lo >> 48),
           static_cast<unsigned long long>(lo & 0x0000FFFFFFFFFFFFULL));
  return buf;
}

void Log(const AcceleratorControlClientConfig& cfg) {
  spdlog::info(
      "[AcceleratorControlClientConfig] control_addr={} device_id={} display_name={} "
      "accelerator_version={} client_cert_file={} client_key_file={} ca_cert_file={} "
      "max_reconnect_delay_s={} keepalive_interval_s={} keepalive_timeout_s={} camera_count={}",
      cfg.control_addr, cfg.device_id, cfg.display_name, cfg.accelerator_version,
      cfg.client_cert_file, cfg.client_key_file, cfg.ca_cert_file, cfg.max_reconnect_delay_s,
      cfg.keepalive_interval_s, cfg.keepalive_timeout_s, cfg.cameras.size());
}

}  // namespace

AcceleratorControlClient::AcceleratorControlClient(AcceleratorControlClientConfig config,
                                                   std::shared_ptr<ProcessorEngineProvider> engine,
                                                   std::shared_ptr<WebRTCManager> webrtc_manager)
    : config_(std::move(config)),
      engine_(std::move(engine)),
      webrtc_manager_(std::move(webrtc_manager)) {
  Log(config_);
}

AcceleratorControlClient::~AcceleratorControlClient() {
  Stop();
}

void AcceleratorControlClient::Stop() {
  stop_requested_ = true;
  CancelStream();
}

void AcceleratorControlClient::CancelStream() {
  // Must not take write_mutex_: a Send() blocked in stream_->Write() holds it
  // for as long as the peer refuses to drain, and TryCancel() is exactly the
  // call that unblocks that write. Gating it on write_mutex_ deadlocks the
  // watchdog, Stop() and cleanup(). TryCancel() is itself thread-safe.
  std::lock_guard<std::mutex> lk(ctx_mutex_);
  if (ctx_ != nullptr) {
    spdlog::info("{} Cancelling control stream", kLogPrefix);
    ctx_->TryCancel();
    ArmTeardownWatchdog();
  }
}

void AcceleratorControlClient::ArmTeardownWatchdog() {
  if (teardown_watchdog_armed_.exchange(true)) {
    return;  // already armed for this connection
  }

  const char* disabled = std::getenv("ACCELERATOR_DISABLE_TEARDOWN_WATCHDOG");
  if (disabled != nullptr && (std::string(disabled) == "1" || std::string(disabled) == "true")) {
    spdlog::warn("{} Teardown watchdog disabled by environment", kLogPrefix);
    return;
  }

  const uint64_t generation = connection_generation_.load();
  const int grace_s = std::max(30, config_.keepalive_timeout_s * 2);

  std::thread([this, generation, grace_s]() {
    for (int i = 0; i < grace_s * 10; ++i) {
      if (connection_generation_.load() != generation || stop_requested_.load()) {
        return;  // teardown completed (or shutdown requested) — nothing to do
      }
      std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }
    if (connection_generation_.load() != generation || stop_requested_.load()) {
      return;
    }
    spdlog::critical(
        "{} Control stream did not tear down {}s after cancel — aborting so the "
        "supervisor restarts the accelerator (see teardown stage logs for the wedged call)",
        kLogPrefix, grace_s);
    spdlog::default_logger()->flush();
    std::abort();
  }).detach();
}

void AcceleratorControlClient::TouchLastRx() {
  last_rx_ms_.store(SteadyNowMs());
}

bool AcceleratorControlClient::IsRxStale() const {
  const int64_t elapsed_ms = SteadyNowMs() - last_rx_ms_.load();
  return elapsed_ms > static_cast<int64_t>(config_.keepalive_timeout_s) * 1000;
}

void AcceleratorControlClient::Run() {
  spdlog::info("{} Starting outbound connection loop to {}", kLogPrefix, config_.control_addr);
  int delay_s = 1;
  while (!stop_requested_) {
    if (RunOnce(&delay_s)) {
      spdlog::info("{} Connection loop exited due to RunOnce() returning true", kLogPrefix);
      break;
    }
    if (stop_requested_) {
      spdlog::info("{} Connection loop exited due to stop request", kLogPrefix);
      break;
    }
    spdlog::warn("{} Reconnecting in {}s...", kLogPrefix, delay_s);
    for (int i = 0; i < delay_s * 10 && !stop_requested_; ++i) {
      std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }
    delay_s = std::min(delay_s * 2, config_.max_reconnect_delay_s);
  }
  spdlog::info("{} Connection loop exited", kLogPrefix);
}

bool AcceleratorControlClient::RunOnce(int* reconnect_delay_s) {
  stream_failed_ = false;
  teardown_watchdog_armed_ = false;
  TouchLastRx();

  // Advancing the generation on every exit path is what tells an armed teardown
  // watchdog that this cycle completed.
  struct GenerationBump {
    std::atomic<uint64_t>* generation;
    ~GenerationBump() { generation->fetch_add(1); }
  } generation_bump{&connection_generation_};

  // Build channel credentials.
  std::shared_ptr<grpc::ChannelCredentials> creds;
  if (!config_.client_cert_file.empty() && !config_.client_key_file.empty() &&
      !config_.ca_cert_file.empty()) {
    std::string cert = ReadFile(config_.client_cert_file);
    std::string key = ReadFile(config_.client_key_file);
    std::string ca = ReadFile(config_.ca_cert_file);
    if (cert.empty() || key.empty() || ca.empty()) {
      spdlog::error("{} Failed to read mTLS credential files", kLogPrefix);
      return false;
    }
    grpc::SslCredentialsOptions opts;
    opts.pem_root_certs = ca;
    opts.pem_cert_chain = cert;
    opts.pem_private_key = key;
    creds = grpc::SslCredentials(opts);
    spdlog::info("{} mTLS credentials loaded", kLogPrefix);
  } else {
    spdlog::warn("{} No mTLS certs configured — using insecure channel", kLogPrefix);
    creds = grpc::InsecureChannelCredentials();
  }

  // Keepalive args keep the long-lived stream alive through NAT/firewall idle timeouts.
  grpc::ChannelArguments ch_args;
  ch_args.SetInt(GRPC_ARG_KEEPALIVE_TIME_MS, 30'000);
  ch_args.SetInt(GRPC_ARG_KEEPALIVE_TIMEOUT_MS, 10'000);
  ch_args.SetInt(GRPC_ARG_KEEPALIVE_PERMIT_WITHOUT_CALLS, 1);
  auto channel = grpc::CreateCustomChannel(config_.control_addr, creds, ch_args);
  auto stub = AcceleratorControlService::NewStub(channel);

  grpc::ClientContext ctx;
  auto stream = stub->Connect(&ctx);

  // Stash pointers so Send() can use them (cleared on exit).
  {
    std::lock_guard<std::mutex> lk(ctx_mutex_);
    ctx_ = &ctx;
  }
  {
    std::lock_guard<std::mutex> lk(write_mutex_);
    stream_ = stream.get();
  }

  auto cleanup = [&]() {
    CancelStream();
    TeardownLocalSessions();
    {
      std::lock_guard<std::mutex> lk(ctx_mutex_);
      ctx_ = nullptr;
    }
    std::lock_guard<std::mutex> lk(write_mutex_);
    stream_ = nullptr;
  };

  // Send Register.
  if (!Send(BuildRegisterMessage())) {
    spdlog::error("{} Failed to send Register — {}", kLogPrefix, ctx.debug_error_string());
    cleanup();
    return stop_requested_;
  }

  // Wait for RegisterAck.
  ConnectResponse ack_resp;
  if (!stream->Read(&ack_resp)) {
    spdlog::error("{} Stream closed before RegisterAck", kLogPrefix);
    cleanup();
    return stop_requested_;
  }
  TouchLastRx();

  const auto& ack_msg = ack_resp.message();
  if (!ack_msg.has_register_ack()) {
    spdlog::error("{} Expected RegisterAck, got something else", kLogPrefix);
    cleanup();
    return stop_requested_;
  }
  if (!ack_msg.register_ack().accepted()) {
    spdlog::error("{} Registration rejected: {}", kLogPrefix,
                  ack_msg.register_ack().reject_reason());
    cleanup();
    return false;
  }
  spdlog::info("{} Registered, session_id={}", kLogPrefix,
               ack_msg.register_ack().assigned_session_id());

  if (reconnect_delay_s != nullptr) {
    *reconnect_delay_s = 1;
  }

  std::atomic<bool> connection_stop{false};

  std::thread keepalive_thread([&]() { KeepaliveLoop(connection_stop); });

  std::thread pump_thread([&]() {
    while (!connection_stop.load() && !stop_requested_.load()) {
      CandidatePumpLoop();
      std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }
  });

  // Dispatch loop.
  //
  // A successful Read proves the stream is alive, so the RX timer is touched
  // before dispatching. Dispatch() runs handlers inline (CreateSession /
  // CloseSession can take seconds), and the old code re-checked staleness on
  // the freshly-read message — a slow handler then tore down a healthy stream
  // on the very next read.
  ConnectResponse resp;
  while (!stop_requested_ && !stream_failed_.load() && stream->Read(&resp)) {
    TouchLastRx();
    Dispatch(resp.message());
    TouchLastRx();
  }

  if (stream_failed_.load() && !stop_requested_) {
    spdlog::warn("{} Stream failed — reconnecting", kLogPrefix);
  }
  spdlog::info("{} Teardown stage: read loop exited", kLogPrefix);

  connection_stop = true;
  keepalive_thread.join();
  spdlog::info("{} Teardown stage: keepalive thread joined", kLogPrefix);
  pump_thread.join();
  spdlog::info("{} Teardown stage: pump thread joined", kLogPrefix);

  stream->WritesDone();
  spdlog::info("{} Teardown stage: WritesDone returned", kLogPrefix);
  auto status = stream->Finish();
  if (!status.ok() && !stop_requested_) {
    spdlog::warn("{} Stream ended: {} {}", kLogPrefix, static_cast<int>(status.error_code()),
                 status.error_message());
  }
  spdlog::info("{} Teardown stage: Finish returned", kLogPrefix);
  cleanup();
  spdlog::info("{} Teardown stage: cleanup complete", kLogPrefix);
  return stop_requested_;
}

bool AcceleratorControlClient::Send(AcceleratorMessage msg) {
  if (msg.command_id().empty()) {
    msg.set_command_id(NewCommandId());
  }
  std::lock_guard<std::mutex> lk(write_mutex_);
  if (!stream_) {
    return false;
  }
  ConnectRequest req;
  *req.mutable_message() = std::move(msg);
  return stream_->Write(req);
}

void AcceleratorControlClient::Dispatch(const AcceleratorMessage& msg) {
  TouchLastRx();
  const std::string& cmd_id = msg.command_id();
  switch (msg.payload_case()) {
    case AcceleratorMessage::kSignalingMessage:
      HandleSignalingMessage(cmd_id, msg.signaling_message());
      break;
    case AcceleratorMessage::kKeepalive:
      spdlog::debug("{} Keepalive received", kLogPrefix);
      break;
    default:
      spdlog::warn("{} Unknown payload type: {}", kLogPrefix, static_cast<int>(msg.payload_case()));
      break;
  }
}

void AcceleratorControlClient::KeepaliveLoop(std::atomic<bool>& connection_stop) {
  const auto tick = std::chrono::milliseconds(100);
  const int ticks_per_interval = std::max(1, config_.keepalive_interval_s * 10);

  while (!connection_stop.load() && !stop_requested_.load()) {
    for (int i = 0; i < ticks_per_interval && !connection_stop.load() && !stop_requested_.load();
         ++i) {
      if (IsRxStale()) {
        spdlog::warn("{} No inbound message for {}s — reconnecting", kLogPrefix,
                     config_.keepalive_timeout_s);
        stream_failed_ = true;
        CancelStream();
        connection_stop = true;
        return;
      }
      std::this_thread::sleep_for(tick);
    }

    if (connection_stop.load() || stop_requested_.load()) {
      return;
    }

    if (!Send(BuildKeepaliveMessage())) {
      spdlog::warn("{} Keepalive send failed — reconnecting", kLogPrefix);
      stream_failed_ = true;
      CancelStream();
      connection_stop = true;
      return;
    }
    spdlog::debug("{} Keepalive sent", kLogPrefix);
  }
}

void AcceleratorControlClient::TeardownLocalSessions() {
  std::vector<std::string> session_ids;
  {
    std::lock_guard<std::mutex> lk(session_ids_mutex_);
    session_ids = std::move(active_session_ids_);
    active_session_ids_.clear();
  }

  if (!webrtc_manager_) {
    return;
  }

  for (const auto& session_id : session_ids) {
    std::string error;
    webrtc_manager_->CloseSession(session_id, &error);
    if (!error.empty()) {
      spdlog::warn("{} CloseSession({}) during teardown: {}", kLogPrefix, session_id, error);
    } else {
      spdlog::info("{} Closed WebRTC session {} on disconnect", kLogPrefix, session_id);
    }
  }
}

void AcceleratorControlClient::HandleSignalingMessage(const std::string& command_id,
                                                      const SignalingMessage& msg) {
  if (!webrtc_manager_ || !webrtc_manager_->IsInitialized()) {
    spdlog::warn("{} WebRTCManager unavailable — dropping signaling message", kLogPrefix);
    return;
  }

  SignalingMessage response;

  switch (msg.message_case()) {
    case SignalingMessage::kStartSession: {
      const auto& req = msg.start_session();
      std::string answer;
      std::string error;
      const bool ok =
          webrtc_manager_->CreateSession(req.session_id(), req.sdp_offer(), &answer, &error);
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

      {
        std::lock_guard<std::mutex> lk(session_ids_mutex_);
        active_session_ids_.push_back(req.session_id());
      }

      spdlog::info("{} WebRTC session started: {}", kLogPrefix, req.session_id());
      break;
    }

    case SignalingMessage::kIceCandidate: {
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

    case SignalingMessage::kCloseSession: {
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
            std::remove(active_session_ids_.begin(), active_session_ids_.end(), req.session_id()),
            active_session_ids_.end());
      }

      spdlog::info("{} WebRTC session closed: {}", kLogPrefix, req.session_id());
      break;
    }

    case SignalingMessage::kKeepAlive:
      spdlog::debug("{} Signaling keepalive", kLogPrefix);
      return;

    default:
      spdlog::warn("{} Unknown signaling message type: {}", kLogPrefix,
                   static_cast<int>(msg.message_case()));
      return;
  }

  AcceleratorMessage out;
  out.set_command_id(command_id);
  *out.mutable_signaling_message() = std::move(response);
  if (!Send(std::move(out))) {
    spdlog::warn("{} Failed to send SignalingMessage response cmd={}", kLogPrefix, command_id);
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
      SignalingMessage signaling;
      auto* ice = signaling.mutable_ice_candidate();
      ice->set_session_id(session_id);
      ice->mutable_candidate()->set_candidate(candidate.candidate());
      ice->mutable_candidate()->set_sdp_mid(candidate.mid());
      ice->mutable_candidate()->set_sdp_mline_index(0);

      AcceleratorMessage out;
      *out.mutable_signaling_message() = std::move(signaling);
      if (!Send(std::move(out))) {
        spdlog::warn("{} Failed to send local candidate for session {}", kLogPrefix, session_id);
        break;
      }
    }
  }
}

AcceleratorMessage AcceleratorControlClient::BuildKeepaliveMessage() const {
  AcceleratorMessage msg;
  msg.set_command_id(NewCommandId());
  const auto unix_ms = std::chrono::duration_cast<std::chrono::milliseconds>(
                           std::chrono::system_clock::now().time_since_epoch())
                           .count();
  msg.mutable_keepalive()->set_sender_unix_ms(unix_ms);
  return msg;
}

AcceleratorMessage AcceleratorControlClient::BuildRegisterMessage() const {
  Register reg;
  reg.set_device_id(config_.device_id);
  reg.set_display_name(config_.display_name);
  reg.set_accelerator_version(config_.accelerator_version);

  for (const auto& cam : config_.cameras) {
    *reg.add_cameras() = cam;
  }

  if (engine_) {
    GetCapabilitiesResponse caps_resp;
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
            reg.add_supported_accelerator_types(static_cast<AcceleratorType>(acc));
          }
        }
      }
    }
  }

  AcceleratorMessage msg;
  msg.set_command_id(NewCommandId());
  *msg.mutable_register_() = std::move(reg);
  return msg;
}

}  // namespace jrb::adapters::grpc_control
