#pragma once

#include <atomic>
#include <functional>
#include <memory>
#include <mutex>
#include <string>
#include <thread>

#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wdeprecated-declarations"
#include <grpcpp/grpcpp.h>
#pragma GCC diagnostic pop

#include "proto/_virtual_imports/accelerator_control_proto/accelerator_control.pb.h"
#include "proto/_virtual_imports/accelerator_control_proto/accelerator_control.grpc.pb.h"
#include "src/cpp_accelerator/ports/grpc/processor_engine_provider.h"
#include "src/cpp_accelerator/ports/grpc/webrtc_manager.h"

namespace jrb::ports::grpc_service {

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
};

// AcceleratorControlClient dials the Go cloud control server, authenticates
// with mTLS, registers this accelerator, then dispatches commands from the
// server to the local engine and WebRTCManager for the lifetime of the process.
//
// Run() blocks until Stop() is called or a fatal error occurs.
class AcceleratorControlClient {
 public:
  AcceleratorControlClient(AcceleratorControlClientConfig config,
                           std::shared_ptr<ProcessorEngineProvider> engine,
                           std::shared_ptr<WebRTCManager> webrtc_manager);
  ~AcceleratorControlClient();

  // Blocks, reconnecting on transient failure. Returns when Stop() is called.
  void Run();

  // Signals Run() to return.
  void Stop();

 private:
  // Executes one connect-register-dispatch cycle. Returns true if the cycle
  // ended cleanly (i.e., Stop() was called); false on any error.
  bool RunOnce();

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

  // Builds the Register message from engine capabilities.
  cuda_learning::AcceleratorMessage BuildRegisterMessage() const;

  AcceleratorControlClientConfig config_;
  std::shared_ptr<ProcessorEngineProvider> engine_;
  std::shared_ptr<WebRTCManager> webrtc_manager_;

  std::atomic<bool> stop_requested_{false};

  // Per-connection state — reset on each reconnect.
  std::mutex write_mutex_;
  grpc::ClientContext* ctx_{nullptr};

  using BidiStream = grpc::ClientReaderWriter<cuda_learning::ConnectRequest,
                                              cuda_learning::ConnectResponse>;
  BidiStream* stream_{nullptr};

  // Set of active WebRTC session IDs (for candidate pump).
  std::mutex session_ids_mutex_;
  std::vector<std::string> active_session_ids_;
};

}  // namespace jrb::ports::grpc_service
