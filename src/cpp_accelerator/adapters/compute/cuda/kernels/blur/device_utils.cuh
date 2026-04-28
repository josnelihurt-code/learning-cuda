#pragma once

#include <cuda_runtime.h>

#include "src/cpp_accelerator/adapters/compute/cuda/memory/cuda_memory_pool.h"

namespace jrb::infrastructure::cuda {

enum class BorderMode : int { CLAMP = 0, REFLECT = 1, WRAP = 2 };
constexpr int K_MAX_KERNEL_SIZE = 63;

__device__ __forceinline__ int ClampX(int x, int width, BorderMode mode) {
  switch (mode) {
    case BorderMode::CLAMP:
      return max(0, min(x, width - 1));
    case BorderMode::REFLECT: {
      if (x < 0) {
        return -x - 1;
      }
      if (x >= width) {
        return 2 * width - x - 1;
      }
      return x;
    }
    case BorderMode::WRAP: {
      return ((x % width) + width) % width;
    }
    default:
      return max(0, min(x, width - 1));
  }
}

__device__ __forceinline__ int ClampY(int y, int height, BorderMode mode) {
  switch (mode) {
    case BorderMode::CLAMP:
      return max(0, min(y, height - 1));
    case BorderMode::REFLECT: {
      if (y < 0) {
        return -y - 1;
      }
      if (y >= height) {
        return 2 * height - y - 1;
      }
      return y;
    }
    case BorderMode::WRAP: {
      return ((y % height) + height) % height;
    }
    default:
      return max(0, min(y, height - 1));
  }
}

__device__ __forceinline__ float GetPixelChannelValue(const unsigned char* data, int x, int y,
                                                      int width, int height, int channels,
                                                      int channel_idx, BorderMode border_mode) {
  int clamped_x = ClampX(x, width, border_mode);
  int clamped_y = ClampY(y, height, border_mode);

  int index = (clamped_y * width + clamped_x) * channels;
  if (channel_idx < channels) {
    return static_cast<float>(data[index + channel_idx]);
  }
  return 0.0F;
}

static void CleanupMemoryPooled(unsigned char* d_input, unsigned char* d_output,
                                unsigned char* d_temp, float* d_kernel, std::size_t data_size,
                                std::size_t kernel_size, CudaMemoryPool* pool = nullptr) {
  if (pool == nullptr) {
    pool = &GetThreadLocalMemoryPool();
  }
  if (d_input != nullptr) {
    pool->Deallocate(d_input, data_size);
  }
  if (d_output != nullptr) {
    pool->Deallocate(d_output, data_size);
  }
  if (d_temp != nullptr) {
    pool->Deallocate(d_temp, data_size);
  }
  if (d_kernel != nullptr) {
    pool->Deallocate(d_kernel, kernel_size * sizeof(float));
  }
}

static cudaError_t AllocateMemoryPooled(int width, int height, int channels, int kernel_size,
                                        unsigned char** d_input, unsigned char** d_output,
                                        unsigned char** d_temp, float** d_kernel,
                                        CudaMemoryPool* pool = nullptr) {
  if (pool == nullptr) {
    pool = &GetThreadLocalMemoryPool();
  }

  size_t data_size = static_cast<size_t>(width) * height * channels;

  *d_input = static_cast<unsigned char*>(pool->Allocate(data_size));
  if (*d_input == nullptr) {
    return cudaErrorMemoryAllocation;
  }

  *d_output = static_cast<unsigned char*>(pool->Allocate(data_size));
  if (*d_output == nullptr) {
    pool->Deallocate(*d_input, data_size);
    return cudaErrorMemoryAllocation;
  }

  if (d_temp != nullptr) {
    *d_temp = static_cast<unsigned char*>(pool->Allocate(data_size));
    if (*d_temp == nullptr) {
      pool->Deallocate(*d_input, data_size);
      pool->Deallocate(*d_output, data_size);
      return cudaErrorMemoryAllocation;
    }
  }

  *d_kernel = static_cast<float*>(pool->Allocate(kernel_size * sizeof(float)));
  if (*d_kernel == nullptr) {
    pool->Deallocate(*d_input, data_size);
    pool->Deallocate(*d_output, data_size);
    if (d_temp != nullptr) {
      pool->Deallocate(*d_temp, data_size);
    }
    return cudaErrorMemoryAllocation;
  }

  return cudaSuccess;
}

}  // namespace jrb::infrastructure::cuda
