/* Minimal NvBufSurface API stub for CI ARM64 compilation.
 *
 * On real Jetson (JetPack 6), the system header at
 * /usr/src/jetson_multimedia_api/include/nvbufsurface.h provides the full
 * implementation backed by nvidia-l4t-multimedia packages.
 *
 * This stub reproduces the EXACT struct layout of the JetPack 6 / L4T R36
 * NvBufSurface API so that code compiled against it accesses fields at the
 * correct byte offsets at runtime.  Verified sizes (aarch64, JetPack 6.x):
 *   sizeof(NvBufSurface)           =  64
 *   sizeof(NvBufSurfaceParams)     = 384
 *   sizeof(NvBufSurfaceMappedAddr) =  72
 *   sizeof(NvBufSurfacePlaneParams)= 232
 *
 * Reference: NVIDIA JetPack 6 / L4T R36 public API
 * https://docs.nvidia.com/jetson/archives/r36.4.3/api_ref/group__NvBufSurface__Group.html
 */
#pragma once

#include <stdint.h>
#include <stdbool.h>

#ifdef __cplusplus
extern "C" {
#endif

#define NVBUF_MAX_PLANES   4
#define STRUCTURE_PADDING  4

/* Mapping flags — values match JetPack 6 (0-based enum). */
typedef enum {
  NVBUF_MAP_READ       = 0,
  NVBUF_MAP_WRITE      = 1,
  NVBUF_MAP_READ_WRITE = 2,
} NvBufSurfaceMemMapFlags;

/* Placeholder enums — only their size (int = 4 bytes) matters for layout. */
typedef enum { NVBUF_COLOR_FORMAT_INVALID = 0 } NvBufSurfaceColorFormat;
typedef enum { NVBUF_LAYOUT_PITCH = 0 }         NvBufSurfaceLayout;
typedef enum {
  NVBUF_MEM_DEFAULT      = 0,
  NVBUF_MEM_CUDA_PINNED  = 1,
  NVBUF_MEM_CUDA_DEVICE  = 2,
  NVBUF_MEM_CUDA_UNIFIED = 3,
  NVBUF_MEM_SURFACE_ARRAY = 4,
  NVBUF_MEM_HANDLE        = 5,
  NVBUF_MEM_SYSTEM        = 6,
} NvBufSurfaceMemType;

/*
 * NvBufSurfacePlaneParams — sizeof = 232 bytes (aarch64).
 *
 * Layout:
 *   uint32_t num_planes          4
 *   uint32_t width[4]           16   offset  4
 *   uint32_t height[4]          16   offset 20
 *   uint32_t pitch[4]           16   offset 36
 *   uint32_t offset[4]          16   offset 52
 *   uint32_t psize[4]           16   offset 68
 *   uint32_t bytesPerPix[4]     16   offset 84
 *   padding                      4   offset 100 → align to 104
 *   void*  _reserved[16]       128   offset 104
 *   total = 232
 */
typedef struct {
  uint32_t num_planes;
  uint32_t width[NVBUF_MAX_PLANES];
  uint32_t height[NVBUF_MAX_PLANES];
  uint32_t pitch[NVBUF_MAX_PLANES];
  uint32_t offset[NVBUF_MAX_PLANES];
  uint32_t psize[NVBUF_MAX_PLANES];
  uint32_t bytesPerPix[NVBUF_MAX_PLANES];
  void *_reserved[STRUCTURE_PADDING * NVBUF_MAX_PLANES];
} NvBufSurfacePlaneParams;

/*
 * NvBufSurfaceMappedAddr — sizeof = 72 bytes (aarch64).
 *
 * Layout:
 *   void* addr[4]        32   offset  0
 *   void* eglImage        8   offset 32
 *   void* _reserved[4]  32   offset 40
 *   total = 72
 */
typedef struct {
  void *addr[NVBUF_MAX_PLANES];
  void *eglImage;
  void *_reserved[STRUCTURE_PADDING];
} NvBufSurfaceMappedAddr;

/* Forward declaration for NvBufSurfaceParamsEx pointer in NvBufSurfaceParams. */
typedef struct NvBufSurfaceParamsEx NvBufSurfaceParamsEx;

/*
 * NvBufSurfaceParams — sizeof = 384 bytes (aarch64).
 *
 * Layout:
 *   uint32_t width              4   offset   0
 *   uint32_t height             4   offset   4
 *   uint32_t pitch              4   offset   8
 *   NvBufSurfaceColorFormat     4   offset  12
 *   NvBufSurfaceLayout          4   offset  16
 *   padding                     4   offset  20 → align uint64_t to 24
 *   uint64_t bufferDesc         8   offset  24
 *   uint32_t dataSize           4   offset  32
 *   padding                     4   offset  36 → align void* to 40
 *   void* dataPtr               8   offset  40
 *   NvBufSurfacePlaneParams   232   offset  48
 *   NvBufSurfaceMappedAddr     72   offset 280
 *   NvBufSurfaceParamsEx*       8   offset 352
 *   void* _reserved[3]         24   offset 360
 *   total = 384
 */
typedef struct {
  uint32_t width;
  uint32_t height;
  uint32_t pitch;
  NvBufSurfaceColorFormat colorFormat;
  NvBufSurfaceLayout layout;
  uint32_t _pad0;
  uint64_t bufferDesc;
  uint32_t dataSize;
  uint32_t _pad1;
  void *dataPtr;
  NvBufSurfacePlaneParams planeParams;
  NvBufSurfaceMappedAddr  mappedAddr;
  NvBufSurfaceParamsEx   *paramex;
  void *_reserved[STRUCTURE_PADDING - 1];
} NvBufSurfaceParams;

/*
 * NvBufSurface — sizeof = 64 bytes (aarch64).
 *
 * Layout:
 *   uint32_t gpuId          4   offset  0
 *   uint32_t batchSize      4   offset  4
 *   uint32_t numFilled      4   offset  8
 *   bool isContiguous       1   offset 12  (+3 padding)
 *   NvBufSurfaceMemType     4   offset 16
 *   padding                 4   offset 20 → align pointer to 24
 *   NvBufSurfaceParams*     8   offset 24
 *   void* _reserved[4]     32   offset 32
 *   total = 64
 */
typedef struct NvBufSurface {
  uint32_t           gpuId;
  uint32_t           batchSize;
  uint32_t           numFilled;
  bool               isContiguous;
  NvBufSurfaceMemType memType;
  uint32_t           _pad0;
  NvBufSurfaceParams *surfaceList;
  void               *_reserved[STRUCTURE_PADDING];
} NvBufSurface;

int NvBufSurfaceMap(NvBufSurface *surf, int index, int plane,
                    NvBufSurfaceMemMapFlags type);
int NvBufSurfaceUnMap(NvBufSurface *surf, int index, int plane);
int NvBufSurfaceSyncForDevice(NvBufSurface *surf, int index, int plane);
int NvBufSurfaceSyncForCpu(NvBufSurface *surf, int index, int plane);
int NvBufSurfaceMapEglImage(NvBufSurface *surf, int index);
int NvBufSurfaceUnMapEglImage(NvBufSurface *surf, int index);

#ifdef __cplusplus
}
#endif
