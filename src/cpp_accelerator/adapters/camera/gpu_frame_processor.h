#pragma once

// GPU-space NV12 -> RGBA tap for Jetson NvidiaArgusBackend.
// Imports the NV12 NVMM GstBuffer into CUDA through its EGLImage (no CPU
// plane mapping; tiled/block-linear NVMM memory is only GPU-readable), runs
// nv12_to_rgba_kernel, and forwards the host RGBA buffer to an optional
// consumer (e.g. BirdWatcher YOLO).
// Designed to do NO work when no RgbCallback is registered, so the WebRTC
// streaming path pays nothing extra when YOLO inference is idle.

#include <cstdint>
#include <functional>
#include <memory>
#include <string>
#include <vector>

struct _GstBuffer;
typedef struct _GstBuffer GstBuffer;

namespace jrb::adapters::camera {

class GpuFrameProcessor {
 public:
  // Called with a host RGBA buffer (width * height * 4 bytes).
  using RgbCallback =
      std::function<void(const std::vector<uint8_t>& rgba, int width, int height)>;

  GpuFrameProcessor();
  ~GpuFrameProcessor();

  GpuFrameProcessor(const GpuFrameProcessor&) = delete;
  GpuFrameProcessor& operator=(const GpuFrameProcessor&) = delete;

  // Reserves the dimensions of incoming frames.  Actual scratch buffers are
  // allocated lazily on the first Process() call that has an active
  // RgbCallback.
  bool Start(int width, int height, std::string* error_message);
  void Stop();
  bool IsRunning() const;

  // Register / clear the RGB consumer.  When no callback is set, Process()
  // returns immediately without mapping the buffer or running any kernel.
  void SetRgbCallback(RgbCallback cb);

  // Process one NV12 NVMM GstBuffer.  No-op when no RgbCallback is registered.
  void Process(GstBuffer* nvmm_buf, uint32_t rtp_ts);

 private:
  struct Impl;
  std::unique_ptr<Impl> impl_;
};

}  // namespace jrb::adapters::camera
