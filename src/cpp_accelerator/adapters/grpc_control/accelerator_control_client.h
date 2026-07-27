#pragma once

#include <atomic>
#include <cstdint>
#include <memory>
#include <mutex>
#include <string>
#include <thread>
#include <vector>

#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wdeprecated-declarations"
#include <grpcpp/grpcpp.h>
#pragma GCC diagnostic pop

#include "proto/_virtual_imports/accelerator_control_proto/accelerator_control.grpc.pb.h"
#include "proto/_virtual_imports/accelerator_control_proto/accelerator_control.pb.h"
#include "src/cpp_accelerator/adapters/grpc_control/processor_engine_provider.h"
#include "src/cpp_accelerator/adapters/webrtc/webrtc_manager.h"
#include "src/cpp_accelerator/ports/control/i_control_port.h"

namespace jrb::adapters::grpc_control {

struct AcceleratorControlClientConfig {
  // Remote address of the Go cloud control server, e.g. "my-server.example.com:60062"
  std::string control_addr;
  // Stable device identifier sent in the Register message.
  std::string device_id;
  // Human-friendly label shown in the UI.
  std::string display_name;
  // Build version string for this accelerator binary.
  std::string accelerator_version;
  // mTLS credentials paths. All three must be non-empty to enable mTLS.
  std::string client_cert_file;
  std::string client_key_file;
  std::string ca_cert_file;
  // Backoff ceiling for reconnect attempts (seconds).
  int max_reconnect_delay_s{60};
  // Application keepalive ping interval (seconds).
  int keepalive_interval_s{15};
  // Reconnect if no inbound stream message for this long (seconds).
  int keepalive_timeout_s{45};
  // Cameras detected at startup, populated from CLI --cameras flag.
  std::vector<cuda_learning::RemoteCameraInfo> cameras;
};

// AcceleratorControlClient dials the Go cloud control server, authenticates
// with mTLS, registers this accelerator, then dispatches commands from the
// server to the local engine and WebRTCManager for the lifetime of the process.
//
// Run() blocks until Stop() is called or a fatal error occurs.
class AcceleratorControlClient : public jrb::ports::control::IControlPort {
public:
  AcceleratorControlClient(AcceleratorControlClientConfig config,
                           std::shared_ptr<ProcessorEngineProvider> engine,
                           std::shared_ptr<jrb::adapters::webrtc::WebRTCManager> webrtc_manager);
  ~AcceleratorControlClient();

  // Blocks, reconnecting on transient failure. Returns when Stop() is called.
  void Run();

  // Signals Run() to return.
  void Stop();

private:
  // Executes one connect-register-dispatch cycle. Returns true if the cycle
  // ended cleanly (i.e., Stop() was called); false on any error.
  // Resets *reconnect_delay_s to 1 after a successful RegisterAck when non-null.
  bool RunOnce(int* reconnect_delay_s);

  // Sends a ConnectRequest envelope with the given AcceleratorMessage.
  // Thread-safe via write_mutex_.
  bool Send(cuda_learning::AcceleratorMessage msg);

  // Handles a single incoming ConnectResponse, routing it to the right handler.
  void Dispatch(const cuda_learning::AcceleratorMessage& msg);

  // Handlers for each command type.
  void HandleSignalingMessage(const std::string& command_id,
                              const cuda_learning::SignalingMessage& msg);

  // Thread that polls WebRTCManager for local ICE candidates and sends them.
  void CandidatePumpLoop();

  // Sends periodic Keepalive messages and enforces inbound timeout.
  void KeepaliveLoop(std::atomic<bool>& connection_stop);

  void TouchLastRx();
  bool IsRxStale() const;
  void CancelStream();
  void TeardownLocalSessions();

  // Last-resort escape. Armed on the first CancelStream() of a connection: if
  // the connection has not been torn down (generation unchanged) after the
  // grace period, the process is aborted so the supervisor restarts it. A
  // cancelled-but-wedged stream means the accelerator is unregistered and
  // unreachable anyway; a crash loop is recoverable, silent limbo is not.
  // Disable with ACCELERATOR_DISABLE_TEARDOWN_WATCHDOG=1.
  void ArmTeardownWatchdog();

  cuda_learning::AcceleratorMessage BuildRegisterMessage() const;
  cuda_learning::AcceleratorMessage BuildKeepaliveMessage() const;

  AcceleratorControlClientConfig config_;
  std::shared_ptr<ProcessorEngineProvider> engine_;
  std::shared_ptr<jrb::adapters::webrtc::WebRTCManager> webrtc_manager_;

  std::atomic<bool> stop_requested_{false};

  // Per-connection state — reset on each reconnect.
  //
  // Two locks, deliberately: write_mutex_ is held across the blocking
  // stream_->Write() to enforce gRPC's single-writer rule, so it can be held
  // for an unbounded time when the peer stops draining. ctx_mutex_ guards only
  // the context pointer and is never held across a blocking call, which is what
  // lets CancelStream() reach TryCancel() while a writer is wedged.
  std::mutex write_mutex_;
  std::mutex ctx_mutex_;
  grpc::ClientContext* ctx_{nullptr};

  using BidiStream =
      grpc::ClientReaderWriter<cuda_learning::ConnectRequest, cuda_learning::ConnectResponse>;
  BidiStream* stream_{nullptr};

  std::atomic<bool> stream_failed_{false};
  std::atomic<int64_t> last_rx_ms_{0};

  // Bumped once per RunOnce cycle; the teardown watchdog compares against it to
  // tell "this connection finished" from "this connection is still wedged".
  std::atomic<uint64_t> connection_generation_{0};
  std::atomic<bool> teardown_watchdog_armed_{false};

  // Set of active WebRTC session IDs (for candidate pump).
  std::mutex session_ids_mutex_;
  std::vector<std::string> active_session_ids_;
};

}  // namespace jrb::adapters::grpc_control
