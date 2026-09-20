#include "src/cpp_accelerator/application/bird_watch/bird_watcher.h"

#include <cstring>
#include <ctime>
#include <filesystem>
#include <iomanip>
#include <mutex>
#include <optional>
#include <sstream>
#include <string>
#include <string_view>
#include <thread>
#include <utility>

extern "C" {
#include <libavcodec/avcodec.h>
#include <libavutil/error.h>
#include <libavutil/frame.h>
#include <libswscale/swscale.h>
}

#include <spdlog/spdlog.h>

#include "proto/_virtual_imports/common_proto/common.pb.h"
#include "proto/_virtual_imports/image_processor_service_proto/image_processor_service.pb.h"
#include "src/cpp_accelerator/application/engine/processor_engine.h"
#include "src/cpp_accelerator/core/logger.h"
#include "src/cpp_accelerator/core/otel_metrics.h"
#include "src/cpp_accelerator/domain/interfaces/image_sink.h"

namespace jrb::application::bird_watch {
namespace otel = ::jrb::core::otel;

namespace {
constexpr std::string_view kLogPrefix = "[BirdWatcher]";

std::string CurrentThreadIdString() {
  std::ostringstream oss;
  oss << std::this_thread::get_id();
  return oss.str();
}

using cuda_learning::ACCELERATOR_TYPE_CUDA;
using cuda_learning::FILTER_TYPE_MODEL_INFERENCE;
using cuda_learning::ProcessImageRequest;
using cuda_learning::ProcessImageResponse;
using jrb::core::configure_ffmpeg_logging;

std::string AvErrorToString(int error_code) {
  char buffer[AV_ERROR_MAX_STRING_SIZE] = {0};
  av_strerror(error_code, buffer, sizeof(buffer));
  return std::string(buffer);
}

}  // namespace

BirdWatcher::BirdWatcher(BirdWatcherConfig config,
                         std::shared_ptr<jrb::adapters::camera::CameraHub> camera_hub,
                         jrb::application::engine::ProcessorEngine* engine,
                         jrb::domain::interfaces::IImageSink* image_sink)
    : config_(std::move(config)),
      camera_hub_(std::move(camera_hub)),
      engine_(engine),
      image_sink_(image_sink),
      cuda_memory_pool_(std::make_unique<jrb::adapters::compute::cuda::CudaMemoryPool>()) {
  spdlog::info("{} BirdWatcher ctor", kLogPrefix);
}

BirdWatcher::~BirdWatcher() {
  Stop();
  spdlog::info("{} BirdWatcher dtor", kLogPrefix);
}

void BirdWatcher::Start() {
  if (!config_.enabled) {
    spdlog::info("{} BirdWatcher not enabled", kLogPrefix);
    return;
  }
  if (running_.exchange(true)) {
    spdlog::info("{} BirdWatcher already running", kLogPrefix);
    return;
  }
  last_idle_check_ =
      std::chrono::steady_clock::now() - std::chrono::seconds(config_.idle_interval_s);
  state_ = State::Idle;
  consecutive_bird_frames_ = 0;
  consecutive_no_bird_frames_ = 0;

  InitDecoder();

  std::string sub_err;
  subscription_ = camera_hub_->Subscribe(
      config_.camera_sensor_id, config_.capture_width, config_.capture_height, config_.capture_fps,
      [this](const rtc::binary& data, const rtc::FrameInfo& info) { OnH264Frame(data, info); },
      &sub_err);
  if (!subscription_.IsActive()) {
    spdlog::error("{} CameraHub subscribe failed: {}", kLogPrefix, sub_err);
    running_ = false;
    DestroyDecoder();
    return;
  }
  spdlog::info("{} CameraHub subscribed to sensor_id={} {}x{}@{}fps successfully", kLogPrefix,
               config_.camera_sensor_id, config_.capture_width, config_.capture_height,
               config_.capture_fps);
  ConnectGpuPath();

  worker_thread_ = std::thread([this] { WorkerLoop(); });
}

void BirdWatcher::Stop() {
  spdlog::info("{} BirdWatcher stopping", kLogPrefix);
  if (!running_.exchange(false)) {
    subscription_.Reset();
    DestroyDecoder();
    return;
  }
  DisconnectGpuPath();
  queue_cv_.notify_all();
  if (worker_thread_.joinable()) {
    worker_thread_.join();
  }
  subscription_.Reset();
  DestroyDecoder();
  spdlog::info("{} BirdWatcher stopped", kLogPrefix);
}

void BirdWatcher::OnH264Frame(const rtc::binary& data, const rtc::FrameInfo& info) {
  if (!running_.load()) {
    return;
  }
  // On the GPU path we receive RGBA via OnRgbaFrame — ignore H.264 frames.
  if (gpu_processor_ != nullptr) {
    return;
  }
  std::lock_guard<std::mutex> lk(queue_mutex_);
  if (frame_queue_.size() >= kMaxQueueSize) {
    frame_queue_.pop();
  }
  frame_queue_.emplace(rtc::binary(data), info);
  queue_cv_.notify_one();
}

void BirdWatcher::OnRgbaFrame(const std::vector<uint8_t>& rgba, int width, int height) {
  if (!running_.load()) {
    return;
  }
  std::lock_guard<std::mutex> lk(queue_mutex_);
  if (rgba_queue_.size() >= kMaxQueueSize) {
    rgba_queue_.pop();
  }
  rgba_queue_.push({rgba, width, height});
  queue_cv_.notify_one();
}

void BirdWatcher::WorkerLoop() {
  spdlog::info("{} BirdWatcher worker loop started thread_id={}", kLogPrefix,
               CurrentThreadIdString());
  while (running_.load()) {
    std::optional<std::pair<rtc::binary, rtc::FrameInfo>> h264_item;
    std::optional<RgbaItem> rgba_item;
    {
      std::unique_lock<std::mutex> lk(queue_mutex_);
      queue_cv_.wait(
          lk, [&] { return !running_.load() || !frame_queue_.empty() || !rgba_queue_.empty(); });
      if (!running_.load()) {
        spdlog::info("{} BirdWatcher worker loop exiting thread_id={}", kLogPrefix,
                     CurrentThreadIdString());
        break;
      }
      if (!rgba_queue_.empty()) {
        rgba_item.emplace(std::move(rgba_queue_.front()));
        rgba_queue_.pop();
      } else if (!frame_queue_.empty()) {
        h264_item.emplace(std::move(frame_queue_.front()));
        frame_queue_.pop();
      }
    }
    if (rgba_item) {
      ProcessRgbaFrame(std::move(rgba_item->rgba), rgba_item->width, rgba_item->height);
    } else if (h264_item) {
      ProcessQueuedFrame(std::move(h264_item->first), std::move(h264_item->second));
    }
  }
}

bool BirdWatcher::ShouldRunInferenceNow() {
  if (state_ == State::Alert) {
    return true;
  }
  const auto now = std::chrono::steady_clock::now();
  if (now - last_idle_check_ >= std::chrono::seconds(config_.idle_interval_s)) {
    last_idle_check_ = now;
    return true;
  }
  return false;
}

void BirdWatcher::ProcessQueuedFrame(rtc::binary data, rtc::FrameInfo info) {
  (void)info;
  // The H.264 stream from GstCameraSource only emits SPS/PPS at the very first
  // IDR (x264enc default). Skipping any AU permanently desyncs the decoder, so
  // every access unit is fed; YOLO inference is the only thing rate-limited.
  const bool want_rgb = ShouldRunInferenceNow();
  std::vector<uint8_t> rgb;
  int w = 0;
  int h = 0;
  if (!FeedDecoderAndExtractRgb(data, want_rgb, &rgb, &w, &h)) {
    spdlog::debug("{} decoder warming up (au {} bytes)", kLogPrefix, data.size());
    return;
  }
  if (!want_rgb) {
    return;
  }

  const bool bird = DetectBird(rgb, w, h);
  if (state_ == State::Idle) {
    spdlog::debug("{} idle check {}x{} bird={}", kLogPrefix, w, h, bird);
    if (bird) {
      spdlog::info("{} IDLE -> ALERT (bird detected)", kLogPrefix);
      state_ = State::Alert;
      consecutive_bird_frames_ = 0;
      consecutive_no_bird_frames_ = 0;
    }
    return;
  }

  if (bird) {
    consecutive_bird_frames_++;
    consecutive_no_bird_frames_ = 0;
    if (consecutive_bird_frames_ >= config_.alert_frames) {
      MaybeSave(rgb, w, h);
      consecutive_bird_frames_ = 0;
    }
  } else {
    consecutive_bird_frames_ = 0;
    consecutive_no_bird_frames_++;
    if (consecutive_no_bird_frames_ >= config_.alert_frames) {
      spdlog::info("{} ALERT -> IDLE (no bird)", kLogPrefix);
      state_ = State::Idle;
      consecutive_no_bird_frames_ = 0;
      last_idle_check_ = std::chrono::steady_clock::now();
    }
  }
  spdlog::debug("{} alert frame {}x{} bird={} bird_streak={} no_bird_streak={}", kLogPrefix, w, h,
                bird, consecutive_bird_frames_, consecutive_no_bird_frames_);
}

// GPU direct path: RGBA already decoded by GpuFrameProcessor — no libavcodec needed.
void BirdWatcher::ProcessRgbaFrame(std::vector<uint8_t> rgba, int width, int height) {
  if (!ShouldRunInferenceNow()) {
    return;
  }

  // YOLO inference expects RGB (3 channels), but we have RGBA (4 channels).
  // Strip the alpha channel in-place into a separate RGB vector.
  const int num_pixels = width * height;
  std::vector<uint8_t> rgb(static_cast<size_t>(num_pixels) * 3);
  for (int i = 0; i < num_pixels; ++i) {
    rgb[i * 3 + 0] = rgba[i * 4 + 0];
    rgb[i * 3 + 1] = rgba[i * 4 + 1];
    rgb[i * 3 + 2] = rgba[i * 4 + 2];
  }

  const bool bird = DetectBird(rgb, width, height);
  if (state_ == State::Idle) {
    spdlog::debug("{} GPU idle check {}x{} bird={}", kLogPrefix, width, height, bird);
    if (bird) {
      spdlog::info("{} IDLE -> ALERT (bird detected via GPU path)", kLogPrefix);
      state_ = State::Alert;
      consecutive_bird_frames_ = 0;
      consecutive_no_bird_frames_ = 0;
    }
    return;
  }

  if (bird) {
    consecutive_bird_frames_++;
    consecutive_no_bird_frames_ = 0;
    if (consecutive_bird_frames_ >= config_.alert_frames) {
      MaybeSave(rgb, width, height);
      consecutive_bird_frames_ = 0;
    }
  } else {
    consecutive_bird_frames_ = 0;
    consecutive_no_bird_frames_++;
    if (consecutive_no_bird_frames_ >= config_.alert_frames) {
      spdlog::info("{} ALERT -> IDLE (no bird)", kLogPrefix);
      state_ = State::Idle;
      consecutive_no_bird_frames_ = 0;
      last_idle_check_ = std::chrono::steady_clock::now();
    }
  }
  spdlog::debug("{} GPU alert frame {}x{} bird={} bird_streak={} no_bird_streak={}", kLogPrefix,
                width, height, bird, consecutive_bird_frames_, consecutive_no_bird_frames_);
}

void BirdWatcher::InitDecoder() {
  if (decoder_context_ != nullptr) {
    return;
  }

  configure_ffmpeg_logging();
  if (decoded_frame_ == nullptr) {
    decoded_frame_ = av_frame_alloc();
  }
  if (rgb_input_frame_ == nullptr) {
    rgb_input_frame_ = av_frame_alloc();
  }
  if (decode_packet_ == nullptr) {
    decode_packet_ = av_packet_alloc();
  }
  if (!decoded_frame_ || !rgb_input_frame_ || !decode_packet_) {
    spdlog::error("{} FFmpeg alloc failed", kLogPrefix);
    return;
  }

  const AVCodec* decoder = avcodec_find_decoder(AV_CODEC_ID_H264);
  if (decoder == nullptr) {
    spdlog::error("{} H264 decoder not available", kLogPrefix);
    return;
  }
  decoder_context_ = avcodec_alloc_context3(decoder);
  if (decoder_context_ == nullptr) {
    spdlog::error("{} avcodec_alloc_context3 failed", kLogPrefix);
    return;
  }
  const int open_result = avcodec_open2(decoder_context_, decoder, nullptr);
  if (open_result < 0) {
    spdlog::error("{} avcodec_open2: {}", kLogPrefix, AvErrorToString(open_result));
    avcodec_free_context(&decoder_context_);
    decoder_context_ = nullptr;
    return;
  }
  first_decode_logged_ = false;
}

void BirdWatcher::DestroyDecoder() {
  sws_freeContext(decode_to_rgb_context_);
  decode_to_rgb_context_ = nullptr;
  av_frame_free(&rgb_input_frame_);
  av_frame_free(&decoded_frame_);
  av_packet_free(&decode_packet_);
  avcodec_free_context(&decoder_context_);
  frame_width_ = 0;
  frame_height_ = 0;
  input_pixel_format_ = -1;
}

bool BirdWatcher::FeedDecoderAndExtractRgb(const rtc::binary& access_unit, bool want_rgb,
                                           std::vector<uint8_t>* rgb, int* width, int* height) {
  if (decoder_context_ == nullptr || decode_packet_ == nullptr) {
    return false;
  }
  if (access_unit.empty()) {
    return false;
  }

  // GstCameraSource pins Annex-B byte-stream + AU alignment, so each callback
  // delivers exactly one access unit ready to be fed verbatim — same pattern
  // as LiveVideoProcessor::ProcessAccessUnit.
  av_packet_unref(decode_packet_);
  decode_packet_->data = const_cast<uint8_t*>(reinterpret_cast<const uint8_t*>(access_unit.data()));
  decode_packet_->size = static_cast<int>(access_unit.size());

  const int send_result = avcodec_send_packet(decoder_context_, decode_packet_);
  if (send_result < 0) {
    spdlog::debug("{} avcodec_send_packet: {}", kLogPrefix, AvErrorToString(send_result));
    return false;
  }

  bool got_picture = false;
  while (true) {
    const int recv = avcodec_receive_frame(decoder_context_, decoded_frame_);
    if (recv == AVERROR(EAGAIN) || recv == AVERROR_EOF) {
      break;
    }
    if (recv < 0) {
      spdlog::debug("{} avcodec_receive_frame: {}", kLogPrefix, AvErrorToString(recv));
      av_frame_unref(decoded_frame_);
      break;
    }
    if (!first_decode_logged_) {
      spdlog::info("{} first H264 frame decoded {}x{}", kLogPrefix, decoded_frame_->width,
                   decoded_frame_->height);
      first_decode_logged_ = true;
    }
    if (want_rgb && rgb != nullptr && width != nullptr && height != nullptr) {
      if (RgbFromDecodedFrame(rgb, width, height)) {
        got_picture = true;
      }
    } else {
      got_picture = true;
    }
    av_frame_unref(decoded_frame_);
  }
  return got_picture;
}

bool BirdWatcher::RgbFromDecodedFrame(std::vector<uint8_t>* rgb, int* width, int* height) {
  const int w = decoded_frame_->width;
  const int h = decoded_frame_->height;
  const int input_format = decoded_frame_->format;
  if (w <= 0 || h <= 0) {
    return false;
  }

  const bool needs_recreate =
      w != frame_width_ || h != frame_height_ || input_format != input_pixel_format_;
  if (needs_recreate) {
    sws_freeContext(decode_to_rgb_context_);
    decode_to_rgb_context_ = nullptr;
    av_frame_unref(rgb_input_frame_);
    rgb_input_frame_->format = AV_PIX_FMT_RGB24;
    rgb_input_frame_->width = w;
    rgb_input_frame_->height = h;
    if (av_frame_get_buffer(rgb_input_frame_, 32) < 0) {
      spdlog::error("{} av_frame_get_buffer RGB failed", kLogPrefix);
      return false;
    }
    decode_to_rgb_context_ =
        sws_getCachedContext(nullptr, w, h, static_cast<AVPixelFormat>(input_format), w, h,
                             AV_PIX_FMT_RGB24, SWS_BILINEAR, nullptr, nullptr, nullptr);
    if (decode_to_rgb_context_ == nullptr) {
      spdlog::error("{} sws_getCachedContext failed", kLogPrefix);
      return false;
    }
    frame_width_ = w;
    frame_height_ = h;
    input_pixel_format_ = input_format;
  }

  if (av_frame_make_writable(rgb_input_frame_) < 0) {
    return false;
  }
  if (sws_scale(decode_to_rgb_context_, decoded_frame_->data, decoded_frame_->linesize, 0, h,
                rgb_input_frame_->data, rgb_input_frame_->linesize) <= 0) {
    return false;
  }

  rgb->assign(static_cast<size_t>(w) * static_cast<size_t>(h) * 3u, 0);
  for (int row = 0; row < h; ++row) {
    const auto* src = rgb_input_frame_->data[0] + row * rgb_input_frame_->linesize[0];
    auto* dst = rgb->data() + static_cast<size_t>(row) * static_cast<size_t>(w) * 3u;
    std::memcpy(dst, src, static_cast<size_t>(w) * 3u);
  }
  *width = w;
  *height = h;
  return true;
}

bool BirdWatcher::DetectBird(const std::vector<uint8_t>& rgb, int width, int height) {
  if (engine_ == nullptr) {
    return false;
  }
  ProcessImageRequest req;
  req.set_accelerator(ACCELERATOR_TYPE_CUDA);
  req.add_filters(FILTER_TYPE_MODEL_INFERENCE);
  req.mutable_model_params()->set_model_id("yolov10n");
  req.mutable_model_params()->set_confidence_threshold(config_.confidence_threshold);
  req.set_image_data(reinterpret_cast<const char*>(rgb.data()), rgb.size());
  req.set_width(width);
  req.set_height(height);
  req.set_channels(3);
  req.set_api_version("1.1");

  ProcessImageResponse resp;
  const auto inference_start = std::chrono::steady_clock::now();
  const bool ok = engine_->ProcessImage(req, &resp, cuda_memory_pool_.get()) && resp.code() == 0;
  otel::metrics::ObserveMs(
      "accelerator.inference.duration_ms",
      std::chrono::duration<double, std::milli>(std::chrono::steady_clock::now() - inference_start)
          .count());
  if (!ok) {
    spdlog::warn("{} ProcessImage failed: {}", kLogPrefix, resp.message());
    otel::metrics::Count("accelerator.inference.errors", 1, "camera",
                         std::to_string(config_.camera_sensor_id));
    return false;
  }
  otel::metrics::Count("accelerator.frames_inferred", 1, "camera",
                       std::to_string(config_.camera_sensor_id));
  bool hit = false;
  for (const auto& d : resp.detections()) {
    if (d.class_name() == "bird" && d.confidence() >= config_.confidence_threshold) {
      hit = true;
      break;
    }
  }
  if (hit) {
    otel::metrics::Count("accelerator.frames_with_bird", 1, "camera",
                         std::to_string(config_.camera_sensor_id));
  }
  spdlog::debug("{} YOLO {}x{} threshold={:.2f} raw_dets={} bird_hit={}", kLogPrefix, width, height,
                config_.confidence_threshold, resp.detections_size(), hit);
  return hit;
}

bool BirdWatcher::RateGateAllowsSave() {
  const auto now = std::chrono::steady_clock::now();
  while (!capture_times_.empty() && now - capture_times_.front() > std::chrono::seconds(60)) {
    capture_times_.pop_front();
  }
  if (capture_times_.size() >= static_cast<size_t>(config_.max_per_minute)) {
    return false;
  }
  if (had_capture_ &&
      now - last_capture_time_ < std::chrono::seconds(config_.min_save_interval_s)) {
    return false;
  }
  return true;
}

void BirdWatcher::MaybeSave(const std::vector<uint8_t>& rgb, int width, int height) {
  if (!RateGateAllowsSave()) {
    return;
  }
  SaveCapture(rgb, width, height);
}

void BirdWatcher::SaveCapture(const std::vector<uint8_t>& /*rgb*/, int /*width*/, int /*height*/) {
  if (image_sink_ == nullptr || config_.captures_dir.empty()) {
    return;
  }
  spdlog::info("{} Saving capture to {}", kLogPrefix, config_.captures_dir);

  // Pull a full-resolution NV12 frame from the still_sink branch of the Argus
  // pipeline (4056×3040 @ 15fps). This is a blocking pull (≤500 ms) and does a
  // synchronous DMA transfer from NVMM to system memory (~5 ms for a 4K frame).
  int still_w = 0, still_h = 0;
  rtc::binary nv12 = camera_hub_->GrabStillFrame(config_.camera_sensor_id, &still_w, &still_h);

  if (nv12.empty() || still_w <= 0 || still_h <= 0) {
    spdlog::warn("{} GrabStillFrame returned empty; skipping save", kLogPrefix);
    return;
  }

  // Convert NV12 → RGB24 using libswscale (CPU, one-shot for still capture).
  // NV12 layout: Y plane (w×h bytes) followed by interleaved UV plane (w×h/2 bytes).
  const auto* y_plane = reinterpret_cast<const uint8_t*>(nv12.data());
  const uint8_t* src_data[4] = {y_plane, y_plane + still_w * still_h, nullptr, nullptr};
  const int src_linesize[4] = {still_w, still_w, 0, 0};

  std::vector<uint8_t> rgb(static_cast<size_t>(still_w) * still_h * 3);
  uint8_t* dst_data[4] = {rgb.data(), nullptr, nullptr, nullptr};
  const int dst_linesize[4] = {still_w * 3, 0, 0, 0};

  SwsContext* sws = sws_getContext(still_w, still_h, AV_PIX_FMT_NV12, still_w, still_h,
                                   AV_PIX_FMT_RGB24, SWS_BILINEAR, nullptr, nullptr, nullptr);
  if (!sws) {
    spdlog::error("{} sws_getContext failed for NV12→RGB24 conversion", kLogPrefix);
    return;
  }
  sws_scale(sws, src_data, src_linesize, 0, still_h, dst_data, dst_linesize);
  sws_freeContext(sws);

  std::error_code ec;
  std::filesystem::create_directories(config_.captures_dir, ec);
  if (ec) {
    spdlog::error("{} create_directories {}: {}", kLogPrefix, config_.captures_dir, ec.message());
    return;
  }

  const auto sys_now = std::chrono::system_clock::now();
  const std::time_t tt = std::chrono::system_clock::to_time_t(sys_now);
  std::tm tm_buf{};
  gmtime_r(&tt, &tm_buf);
  std::ostringstream name;
  name << std::put_time(&tm_buf, "%Y-%m-%dT%H-%M-%SZ");
  const std::string path = config_.captures_dir + "/" + name.str() + ".bmp";

  if (!image_sink_->writeBmp(path.c_str(), rgb.data(), still_w, still_h, 3)) {
    spdlog::error("{} writeBmp failed: {}", kLogPrefix, path);
    return;
  }

  const auto now = std::chrono::steady_clock::now();
  had_capture_ = true;
  last_capture_time_ = now;
  capture_times_.push_back(now);
  otel::metrics::Count("accelerator.captures", 1, "camera",
                       std::to_string(config_.camera_sensor_id));
  spdlog::info("{} Saved {}x{} still capture: {}", kLogPrefix, still_w, still_h, path);
}

}  // namespace jrb::application::bird_watch
