#pragma once

// NvBufSurface access for NVMM buffers produced by the Argus pipeline
// (`video/x-raw(memory:NVMM)` appsinks). GPU access happens through the
// EGLImage interop in GpuFrameProcessor; these helpers only extract the
// NvBufSurface handle from the GstBuffer.
#include <gst/gst.h>

struct NvBufSurface;

namespace jrb::adapters::camera {

// Map an NVMM GstBuffer far enough to obtain its NvBufSurface handle (no CPU
// plane mapping).  Returns nullptr on failure.  The caller must call
// gst_buffer_unmap(buf, map_info) when done with the surface.
NvBufSurface* GetNvmmSurface(GstBuffer* buf, GstMapInfo* map_info);

}  // namespace jrb::adapters::camera
