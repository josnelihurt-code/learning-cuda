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
      const unsigned char* y_src = static_cast<const unsigned char*>(frame.frame.pPitch[0].ptr);
      const unsigned char* uv_src = static_cast<const unsigned char*>(frame.frame.pPitch[1].ptr);
      const size_t y_pitch = frame.frame.pPitch[0].pitch;
      const size_t uv_pitch = frame.frame.pPitch[1].pitch;
      for (int r = 0; r < h && err == cudaSuccess; r++) {
        err = cudaMemcpy(d_y_in + static_cast<size_t>(r) * w, y_src + r * y_pitch, w,
                         cudaMemcpyDeviceToDevice);
      }
      for (int r = 0; r < h / 2 && err == cudaSuccess; r++) {
        err = cudaMemcpy(d_uv_in + static_cast<size_t>(r) * w, uv_src + r * uv_pitch, w,
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
  if (!impl_->running.load())
    return;

  // Snapshot the callback so we can drop the lock before doing CUDA work.
  RgbCallback cb_snapshot;
  {
    std::lock_guard<std::mutex> lk(impl_->rgb_cb_mutex);
    cb_snapshot = impl_->rgb_cb;
  }
  if (!cb_snapshot) {
    // Nothing consumes RGBA right now; skip the entire pipeline so the
    // streaming path pays no GPU/CPU tax.
    return;
  }

  const EGLDisplay display = AcquireEglDisplay();
  if (display == EGL_NO_DISPLAY) {
    return;
  }

  GstMapInfo map_info{};
  NvBufSurface* surface = GetNvmmSurface(nvmm_buf, &map_info);
  if (surface == nullptr) {
    return;
  }

  // Import the NVMM buffer into CUDA through its EGLImage.  This never CPU-maps
  // the planes: tiled (block-linear) memory is only readable by the GPU.
  if (NvBufSurfaceMapEglImage(surface, 0) != 0) {
    spdlog::warn("{} NvBufSurfaceMapEglImage failed; dropping frame", kLogPrefix);
    gst_buffer_unmap(nvmm_buf, &map_info);
    return;
  }

  cudaGraphicsResource_t resource = nullptr;
  const EGLImageKHR egl_image =
      reinterpret_cast<EGLImageKHR>(surface->surfaceList[0].mappedAddr.eglImage);
  if (cudaGraphicsEGLRegisterImage(&resource, egl_image,
                                   cudaGraphicsRegisterFlagsNone) != cudaSuccess) {
    spdlog::warn("{} cudaGraphicsEGLRegisterImage failed; dropping frame", kLogPrefix);
    NvBufSurfaceUnMapEglImage(surface, 0);
    gst_buffer_unmap(nvmm_buf, &map_info);
    return;
  }

  cudaEglFrame frame{};
  bool ok = cudaGraphicsResourceGetMappedEglFrame(&frame, resource, 0, 0) == cudaSuccess;
  if (ok) {
    const int w = static_cast<int>(surface->surfaceList[0].width);
    const int h = static_cast<int>(surface->surfaceList[0].height);
    if (impl_->EnsureScratch(w, h) && impl_->CopyPlanesToDevice(frame, w, h)) {
      const cudaError_t conv_err =
          cuda_nv12_to_rgba_device(impl_->d_y_in, impl_->d_uv_in, w, impl_->d_rgba, w, h);
      if (conv_err != cudaSuccess) {
        spdlog::error("{} cuda_nv12_to_rgba_device: {}", kLogPrefix,
                      cudaGetErrorString(conv_err));
        ok = false;
      } else {
        const size_t rgba_bytes = static_cast<size_t>(w) * h * 4;
        ok = cudaMemcpy(impl_->h_rgba.data(), impl_->d_rgba, rgba_bytes,
                        cudaMemcpyDeviceToHost) == cudaSuccess;
        if (ok) {
          try {
            cb_snapshot(impl_->h_rgba, w, h);
          } catch (const std::exception& e) {
            spdlog::warn("{} RgbCallback threw: {}", kLogPrefix, e.what());
          }
        }
      }
    } else {
      ok = false;
    }
  } else {
    spdlog::warn("{} cudaGraphicsResourceGetMappedEglFrame failed", kLogPrefix);
  }

  cudaGraphicsUnregisterResource(resource);
  NvBufSurfaceUnMapEglImage(surface, 0);
  gst_buffer_unmap(nvmm_buf, &map_info);
}

}  // namespace jrb::adapters::camera
