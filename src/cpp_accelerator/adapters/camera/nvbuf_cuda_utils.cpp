#include "src/cpp_accelerator/adapters/camera/nvbuf_cuda_utils.h"

#include <nvbufsurface.h>
#include <spdlog/spdlog.h>
#include <string_view>

namespace jrb::adapters::camera {
constexpr std::string_view kLogPrefix = "[NvBufCudaUtils]";

NvBufSurface* GetNvmmSurface(GstBuffer* buf, GstMapInfo* map_info) {
  if (!buf || !map_info) {
    return nullptr;
  }

  // gst_buffer_map on an NVMM buffer: map_info->data points to the NvBufSurface.
  if (!gst_buffer_map(buf, map_info, GST_MAP_READ)) {
    spdlog::error("{} gst_buffer_map failed", kLogPrefix);
    return nullptr;
  }

  auto* surface = reinterpret_cast<NvBufSurface*>(map_info->data);
  if (!surface || surface->numFilled == 0 || surface->batchSize < 1) {
    spdlog::error("{} NvBufSurface is null or empty", kLogPrefix);
    gst_buffer_unmap(buf, map_info);
    return nullptr;
  }
  return surface;
}

}  // namespace jrb::adapters::camera
