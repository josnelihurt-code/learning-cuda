#include <cstdlib>
#include <memory>
#include <sstream>
#include <string>

#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wdeprecated-declarations"
#include <spdlog/spdlog.h>
#pragma GCC diagnostic pop

#include "absl/flags/flag.h"
#include "absl/flags/parse.h"

#include "src/cpp_accelerator/adapters/camera/camera_detector.h"
#include "src/cpp_accelerator/adapters/camera/camera_hub.h"
#include "src/cpp_accelerator/adapters/grpc_control/accelerator_control_client.h"
#include "src/cpp_accelerator/adapters/grpc_control/processor_engine_adapter.h"
#include "src/cpp_accelerator/adapters/image_io/image_writer.h"
#include "src/cpp_accelerator/adapters/webrtc/webrtc_manager.h"
#include "src/cpp_accelerator/application/bird_watch/bird_watcher.h"
#include "src/cpp_accelerator/application/engine/processor_engine.h"
#include "src/cpp_accelerator/core/otel_metrics.h"
#include "src/cpp_accelerator/core/signal_handler.h"
#include "src/cpp_accelerator/core/version.h"

ABSL_FLAG(std::string, control_addr, "localhost:60062",
          "Address of the Go cloud control server (host:port).");
ABSL_FLAG(std::string, device_id, "dev-accelerator",
          "Stable device identifier sent during registration.");
ABSL_FLAG(std::string, display_name, "Dev Accelerator", "Human-readable name shown in the UI.");
ABSL_FLAG(int, cuda_device_id, 0, "CUDA device ID to initialize on startup.");
ABSL_FLAG(int, max_message_mb, 64, "Maximum gRPC message size in MiB.");
ABSL_FLAG(std::string, client_cert, ".secrets/dev-accelerator-client.pem",
          "Path to client TLS certificate (PEM).");
ABSL_FLAG(std::string, client_key, ".secrets/dev-accelerator-client-key.pem",
          "Path to client TLS private key (PEM).");
ABSL_FLAG(std::string, ca_cert, ".secrets/accelerator-ca.pem",
          "Path to CA certificate used to verify the server (PEM).");
ABSL_FLAG(int, max_reconnect_delay_s, 60, "Maximum reconnect back-off in seconds.");
ABSL_FLAG(int, keepalive_interval_s, 15,
          "Application keepalive ping interval on the control stream (seconds).");
ABSL_FLAG(int, keepalive_timeout_s, 45,
          "Reconnect if no inbound control-stream message for this long (seconds).");
ABSL_FLAG(std::string, cameras, "0,1",
          "Comma-separated sensor IDs to probe and advertise as remote cameras.");
ABSL_FLAG(std::string, captures_dir, "/tmp/cuda-captures",
          "Directory where captured BMP frames are saved.");
ABSL_FLAG(bool, bird_watch_enabled, true,
          "Background bird detection (CameraHub subscriber; runs with or without UI)");
ABSL_FLAG(double, bird_watch_confidence, 0.4, "YOLO confidence threshold for bird");
ABSL_FLAG(int, bird_watch_idle_interval_s, 3, "Seconds between idle YOLO checks");
ABSL_FLAG(int, bird_watch_alert_frames, 5, "Consecutive frames to trigger capture / reset to idle");
ABSL_FLAG(int, bird_watch_max_per_minute, 5, "Max BMP saves per minute");
ABSL_FLAG(int, bird_watch_min_interval_s, 5, "Min seconds between saves");
ABSL_FLAG(int, bird_watch_camera_id, 0, "Sensor ID to watch");

using cuda_learning::InitRequest;
using cuda_learning::InitResponse;
using jrb::adapters::camera::CameraHub;
using jrb::adapters::camera::DetectCameras;
using jrb::adapters::grpc_control::AcceleratorControlClient;
using jrb::adapters::grpc_control::AcceleratorControlClientConfig;
using jrb::adapters::grpc_control::ProcessorEngineAdapter;
using jrb::adapters::image::ImageWriter;
using jrb::adapters::webrtc::WebRTCManager;
using jrb::adapters::webrtc::WebRTCManagerConfig;
using jrb::application::bird_watch::BirdWatcher;
using jrb::application::bird_watch::BirdWatcherConfig;
using jrb::application::engine::ProcessorEngine;
using jrb::core::SignalHandler;

// This is the main entry point for the accelerator control client.
int main(int argc, char** argv) {
  absl::ParseCommandLine(argc, argv);

  spdlog::info("========================================");
  spdlog::info("CUDA Accelerator Control Client Starting");
  spdlog::info("========================================");
  spdlog::info("Version:      {} (git: {})", kLibraryVersionStr, kLibraryGitHashStr);
  spdlog::info("Control addr: {}", absl::GetFlag(FLAGS_control_addr));
  spdlog::info("Device ID:    {}", absl::GetFlag(FLAGS_device_id));
  spdlog::info("CUDA device:  {}", absl::GetFlag(FLAGS_cuda_device_id));
  spdlog::info("Cameras:      {}", absl::GetFlag(FLAGS_cameras));
  spdlog::info("Captures dir: {}", absl::GetFlag(FLAGS_captures_dir));
  spdlog::info("Bird watch:   {}",
               absl::GetFlag(FLAGS_bird_watch_enabled) ? "enabled" : "disabled");
  spdlog::info("========================================");

  auto engine = std::make_shared<ProcessorEngine>("accelerator-client");

  InitRequest init_req;
  init_req.set_cuda_device_id(absl::GetFlag(FLAGS_cuda_device_id));
  InitResponse init_resp;
  if (!engine->Initialize(init_req, &init_resp) || init_resp.code() != 0) {
    spdlog::error("Failed to initialize processor engine: {}", init_resp.message());
    return 1;
  }

  auto adapter = std::make_shared<ProcessorEngineAdapter>(engine);
  auto camera_hub = CameraHub::Create();
  auto image_sink = std::make_shared<ImageWriter>();
  WebRTCManagerConfig webrtc_cfg;
  webrtc_cfg.engine = engine;
  webrtc_cfg.camera_hub = camera_hub;
  webrtc_cfg.image_sink = image_sink;
  webrtc_cfg.device_id = absl::GetFlag(FLAGS_device_id);
  webrtc_cfg.display_name = absl::GetFlag(FLAGS_display_name);
  webrtc_cfg.captures_dir = absl::GetFlag(FLAGS_captures_dir);
  webrtc_cfg.accelerator_version = kLibraryVersionStr;
  auto webrtc_manager = std::make_shared<WebRTCManager>(webrtc_cfg);
  if (!webrtc_manager->Initialize()) {
    spdlog::warn("WebRTCManager failed to initialize — signaling will be unavailable");
  } else {
    spdlog::info("WebRTCManager ready");
  }

  std::unique_ptr<BirdWatcher> bird_watcher;
  if (absl::GetFlag(FLAGS_bird_watch_enabled)) {
    BirdWatcherConfig bw_cfg;
    bw_cfg.enabled = true;
    bw_cfg.confidence_threshold = static_cast<float>(absl::GetFlag(FLAGS_bird_watch_confidence));
    bw_cfg.idle_interval_s = absl::GetFlag(FLAGS_bird_watch_idle_interval_s);
    bw_cfg.alert_frames = absl::GetFlag(FLAGS_bird_watch_alert_frames);
    bw_cfg.max_per_minute = absl::GetFlag(FLAGS_bird_watch_max_per_minute);
    bw_cfg.min_save_interval_s = absl::GetFlag(FLAGS_bird_watch_min_interval_s);
    bw_cfg.camera_sensor_id = absl::GetFlag(FLAGS_bird_watch_camera_id);
    bw_cfg.captures_dir = absl::GetFlag(FLAGS_captures_dir);
    bird_watcher =
        std::make_unique<BirdWatcher>(bw_cfg, camera_hub, engine.get(), image_sink.get());
    bird_watcher->Start();
    if (bird_watcher->IsSubscribed()) {
      spdlog::info("[BirdWatcher] Started (camera={}, threshold={:.2f})", bw_cfg.camera_sensor_id,
                   bw_cfg.confidence_threshold);
    } else {
      spdlog::warn(
          "[BirdWatcher] Disabled: camera hub did not start (build with --config=v4l2-camera or "
          "Argus, or check device). Bird watch flag remains on but no frames will be processed.");
    }
  }

  AcceleratorControlClientConfig cfg;
  cfg.control_addr = absl::GetFlag(FLAGS_control_addr);
  cfg.device_id = absl::GetFlag(FLAGS_device_id);
  cfg.display_name = absl::GetFlag(FLAGS_display_name);
  cfg.accelerator_version = kLibraryVersionStr;
  cfg.client_cert_file = absl::GetFlag(FLAGS_client_cert);
  cfg.client_key_file = absl::GetFlag(FLAGS_client_key);
  cfg.ca_cert_file = absl::GetFlag(FLAGS_ca_cert);
  cfg.max_reconnect_delay_s = absl::GetFlag(FLAGS_max_reconnect_delay_s);
  cfg.keepalive_interval_s = absl::GetFlag(FLAGS_keepalive_interval_s);
  cfg.keepalive_timeout_s = absl::GetFlag(FLAGS_keepalive_timeout_s);

  // Parse comma-separated sensor IDs and detect cameras.
  {
    const std::string cameras_flag = absl::GetFlag(FLAGS_cameras);
    std::vector<int> sensor_ids;
    std::stringstream ss(cameras_flag);
    std::string token;
    while (std::getline(ss, token, ',')) {
      try {
        const int id = std::stoi(token);
        sensor_ids.push_back(id);
      } catch (...) {
        spdlog::warn("Invalid sensor ID in --cameras: '{}'", token);
      }
    }
    cfg.cameras = DetectCameras(sensor_ids);
    spdlog::info("Detected {} camera(s):", cfg.cameras.size());
    for (const auto& cam : cfg.cameras) {
      spdlog::info("  sensor_id={} display_name='{}' model='{}'", cam.sensor_id(),
                   cam.display_name(), cam.model());
    }
  }

  AcceleratorControlClient client(cfg, adapter, webrtc_manager);

  auto& signal_handler = SignalHandler::GetInstance();
  signal_handler.Initialize([&client]() {
    spdlog::warn("Shutdown signal received — stopping accelerator client");
    client.Stop();
  });

  client.Run();

  if (bird_watcher) {
    bird_watcher->Stop();
  }

  signal_handler.Shutdown();

  spdlog::info("Accelerator client exited gracefully");
  // Teardown of the OpenTelemetry SDK at static-destruction time segfaults
  // after the providers are flushed; flush here and skip the destructors.
  jrb::core::otel::ShutdownMetrics();
  spdlog::default_logger()->flush();
  std::_Exit(0);
}
