#include "src/cpp_accelerator/adapters/camera/gpu_frame_processor.h"

#include <EGL/egl.h>
#include <EGL/eglext.h>
#include <cuda_egl_interop.h>
#include <cuda_runtime.h>
#include <nvbufsurface.h>
#include <atomic>
#include <mutex>
#include <string_view>

#include <spdlog/spdlog.h>

#include "src/cpp_accelerator/adapters/camera/nvbuf_cuda_utils.h"
#include "src/cpp_accelerator/adapters/compute/cuda/kernels/nv12_utils_kernel.h"

#ifndef PFN_eglQueryDevicesEXT_t
typedef EGLBoolean (*PFN_eglQueryDevicesEXT_t)(EGLint, EGLDeviceEXT *, EGLint *);
#endif
#ifndef PFN_eglGetPlatformDisplayEXT_t
typedef EGLDisplay (*PFN_eglGetPlatformDisplayEXT_t)(EGLenum, void *, const EGLint *);
#endif

namespace jrb::adapters::camera {
constexpr std::string_view kLogPrefix = "[GpuFrameProcessor]";

// The appsink streaming thread is the only user of the EGL display; the
// device platform works headless (no window system) and, unlike the default
// display, exposes configs without an X/GBM surface.
static EGLDisplay AcquireEglDisplay() {
  static EGLDisplay display = EGL_NO_DISPLAY;
  static std::once_flag once;
  std::call_once(once, [] {
    auto query_devices = reinterpret_cast<PFN_eglQueryDevicesEXT_t>(
        eglGetProcAddress("eglQueryDevicesEXT"));
    auto get_platform_display = reinterpret_cast<PFN_eglGetPlatformDisplayEXT_t>(
        eglGetProcAddress("eglGetPlatformDisplayEXT"));
    if (query_devices && get_platform_display) {
      EGLDeviceEXT devices[4];
      EGLint count = 0;
      if (query_devices(4, devices, &count) && count > 0) {
        display = get_platform_display(EGL_PLATFORM_DEVICE_EXT, devices[0], nullptr);
      }
    }
    if (display == EGL_NO_DISPLAY) {
      display = eglGetDisplay(EGL_DEFAULT_DISPLAY);
    }
    if (display == EGL_NO_DISPLAY || !eglInitialize(display, nullptr, nullptr)) {
      spdlog::error("{} EGL display initialization failed", kLogPrefix);
      display = EGL_NO_DISPLAY;
      return;
    }
    spdlog::info("{} EGL display initialized (device platform preferred)", kLogPrefix);
  });
  return display;
}

// Owns the per-frame EGL import chain (NvBufSurface handle, EGLImage mapping,
// CUDA registration) and unwinds exactly the steps that succeeded.
namespace {

class EglFrameImport {
 public:
  explicit EglFrameImport(GstBuffer* buf) : buf_(buf) { ok_ = Import(); }

  ~EglFrameImport() {
    if (resource_ != nullptr) {
      cudaGraphicsUnregisterResource(resource_);
    }
    if (egl_mapped_) {
      NvBufSurfaceUnMapEglImage(surface_, 0);
    }
    if (surface_ != nullptr) {
      gst_buffer_unmap(buf_, &map_info_);
    }
  }

  EglFrameImport(const EglFrameImport&) = delete;
  EglFrameImport& operator=(const EglFrameImport&) = delete;

  bool ok() const { return ok_; }
  const cudaEglFrame& frame() const { return frame_; }
  int width() const { return width_; }
  int height() const { return height_; }

 private:
  bool Import() {
    surface_ = GetNvmmSurface(buf_, &map_info_);
    if (surface_ == nullptr) {
      return false;
    }
    if (NvBufSurfaceMapEglImage(surface_, 0) != 0) {
      spdlog::warn("{} NvBufSurfaceMapEglImage failed; dropping frame", kLogPrefix);
      return false;
    }
    egl_mapped_ = true;

    const EGLImageKHR egl_image =
        reinterpret_cast<EGLImageKHR>(surface_->surfaceList[0].mappedAddr.eglImage);
    if (cudaGraphicsEGLRegisterImage(&resource_, egl_image,
                                     cudaGraphicsRegisterFlagsNone) != cudaSuccess) {
      spdlog::warn("{} cudaGraphicsEGLRegisterImage failed; dropping frame", kLogPrefix);
      return false;
    }
    if (cudaGraphicsResourceGetMappedEglFrame(&frame_, resource_, 0, 0) != cudaSuccess) {
      spdlog::warn("{} cudaGraphicsResourceGetMappedEglFrame failed", kLogPrefix);
      return false;
    }

    width_ = static_cast<int>(surface_->surfaceList[0].width);
    height_ = static_cast<int>(surface_->surfaceList[0].height);
    return true;
  }

  GstBuffer* buf_;
  GstMapInfo map_info_{};
  NvBufSurface* surface_ = nullptr;
  bool egl_mapped_ = false;
  cudaGraphicsResource_t resource_ = nullptr;
  cudaEglFrame frame_{};
  int width_ = 0;
  int height_ = 0;
  bool ok_ = false;
};

}  // namespace

static void InvokeCallback(const GpuFrameProcessor::RgbCallback& cb,
                           const std::vector<uint8_t>& rgba, int width, int height) {
  try {
    cb(rgba, width, height);
  } catch (const std::exception& e) {
    spdlog::warn("{} RgbCallback threw: {}", kLogPrefix, e.what());
  }
}

struct GpuFrameProcessor::Impl {
  std::atomic<bool> running{false};

  std::mutex rgb_cb_mutex;
  RgbCallback rgb_cb;

  int configured_width = 0;
  int configured_height = 0;

  // Packed scratch buffers (pitch == width), used only when an RgbCallback is
  // active.  Filled device-side from the EGL-mapped frame so the kernel reads
  // a layout-independent linear copy of both NV12 planes.
  uint8_t* d_y_in = nullptr;   // width * height
  uint8_t* d_uv_in = nullptr;  // width * (height / 2)
  uint8_t* d_rgba = nullptr;   // width * height * 4

  int alloc_width = 0;
  int alloc_height = 0;

  std::vector<uint8_t> h_rgba;

  RgbCallback SnapshotCallback() {
    std::lock_guard<std::mutex> lk(rgb_cb_mutex);
    return rgb_cb;
  }

  bool EnsureScratch(int w, int h) {
    if (d_rgba && w == alloc_width && h == alloc_height) {
      return true;
    }
    FreeScratch();
    const size_t y_bytes = static_cast<size_t>(w) * h;
    const size_t uv_bytes = static_cast<size_t>(w) * (h / 2);
    const size_t rgba_bytes = static_cast<size_t>(w) * h * 4;

    if (cudaMalloc(&d_y_in, y_bytes) != cudaSuccess ||
        cudaMalloc(&d_uv_in, uv_bytes) != cudaSuccess ||
        cudaMalloc(&d_rgba, rgba_bytes) != cudaSuccess) {
      spdlog::error("{} cudaMalloc failed for scratch buffers", kLogPrefix);
      FreeScratch();
      return false;
    }
    h_rgba.resize(rgba_bytes);
    alloc_width = w;
    alloc_height = h;
    return true;
  }

  void FreeScratch() {
    if (d_y_in) {
      cudaFree(d_y_in);
      d_y_in = nullptr;
    }
    if (d_uv_in) {
      cudaFree(d_uv_in);
      d_uv_in = nullptr;
    }
    if (d_rgba) {
      cudaFree(d_rgba);
      d_rgba = nullptr;
    }
    h_rgba.clear();
    h_rgba.shrink_to_fit();
    alloc_width = alloc_height = 0;
  }

  // Copy the Y and UV planes of an EGL-mapped frame into the packed scratch
  // buffers, entirely on the GPU.  Tiled (block-linear) surfaces surface as
  // CUDA arrays, which only cudaMemcpy2DFromArray can read layout-safely;
  // pitch-linear surfaces surface as plain device pointers.
  bool CopyPlanesToDevice(const cudaEglFrame& frame, int w, int h) {
    cudaError_t err = cudaSuccess;
    if (frame.frameType == cudaEglFrameTypePitch) {
      err = cudaMemcpy2D(d_y_in, w, frame.frame.pPitch[0].ptr,
                         frame.frame.pPitch[0].pitch, w, h, cudaMemcpyDeviceToDevice);
      if (err == cudaSuccess) {
        err = cudaMemcpy2D(d_uv_in, w, frame.frame.pPitch[1].ptr,
                           frame.frame.pPitch[1].pitch, w, h / 2,
                           cudaMemcpyDeviceToDevice);
      }
    } else {
      err = cudaMemcpy2DFromArray(d_y_in, w, frame.frame.pArray[0], 0, 0, w, h,
                                  cudaMemcpyDeviceToDevice);
      if (err == cudaSuccess) {
        err = cudaMemcpy2DFromArray(d_uv_in, w, frame.frame.pArray[1], 0, 0, w, h / 2,
                                    cudaMemcpyDeviceToDevice);
      }
    }
    if (err != cudaSuccess) {
      spdlog::error("{} NV12 plane copy from EGL frame failed: {}", kLogPrefix,
                    cudaGetErrorString(err));
      return false;
    }
    return true;
  }

  // NV12 scratch -> RGBA scratch -> host buffer.
  bool ConvertToRgba(int w, int h) {
    const cudaError_t conv_err = cuda_nv12_to_rgba_device(d_y_in, d_uv_in, w, d_rgba, w, h);
    if (conv_err != cudaSuccess) {
      spdlog::error("{} cuda_nv12_to_rgba_device: {}", kLogPrefix,
                    cudaGetErrorString(conv_err));
      return false;
    }
    if (cudaMemcpy(h_rgba.data(), d_rgba, static_cast<size_t>(w) * h * 4,
                   cudaMemcpyDeviceToHost) != cudaSuccess) {
      spdlog::error("{} RGBA D->H copy failed", kLogPrefix);
      return false;
    }
    return true;
  }
};

GpuFrameProcessor::GpuFrameProcessor() : impl_(std::make_unique<Impl>()) {}
GpuFrameProcessor::~GpuFrameProcessor() {
  Stop();
}

bool GpuFrameProcessor::Start(int width, int height, std::string* /*error_message*/) {
  impl_->configured_width = width;
  impl_->configured_height = height;
  impl_->running = true;
  return true;
}

void GpuFrameProcessor::Stop() {
  impl_->running = false;
  {
    std::lock_guard<std::mutex> lk(impl_->rgb_cb_mutex);
    impl_->rgb_cb = nullptr;
  }
  impl_->FreeScratch();
  impl_->configured_width = 0;
  impl_->configured_height = 0;
}

bool GpuFrameProcessor::IsRunning() const {
  return impl_->running.load();
}

void GpuFrameProcessor::SetRgbCallback(RgbCallback cb) {
  std::lock_guard<std::mutex> lk(impl_->rgb_cb_mutex);
  impl_->rgb_cb = std::move(cb);
}

void GpuFrameProcessor::Process(GstBuffer* nvmm_buf, uint32_t /*rtp_ts*/) {
  if (!impl_->running.load()) {
    return;
  }
  const RgbCallback cb = impl_->SnapshotCallback();
  if (!cb) {
    // Nothing consumes RGBA right now; skip the entire pipeline so the
    // streaming path pays no GPU/CPU tax.
    return;
  }
  if (AcquireEglDisplay() == EGL_NO_DISPLAY) {
    return;
  }

  EglFrameImport imported(nvmm_buf);
  if (!imported.ok()) {
    return;
  }

  const int w = imported.width();
  const int h = imported.height();
  if (!impl_->EnsureScratch(w, h) || !impl_->CopyPlanesToDevice(imported.frame(), w, h) ||
      !impl_->ConvertToRgba(w, h)) {
    return;
  }
  InvokeCallback(cb, impl_->h_rgba, w, h);
}

}  // namespace jrb::adapters::camera
