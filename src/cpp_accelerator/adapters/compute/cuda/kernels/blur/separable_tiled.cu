#include "src/cpp_accelerator/adapters/compute/cuda/kernels/blur/device_utils.cuh"

namespace jrb::infrastructure::cuda {

__constant__ float c_gaussian_kernel[K_MAX_KERNEL_SIZE];

static cudaError_t UploadKernelToConstantMemory(const float* kernel, int kernel_size) {
  if (kernel_size <= 0 || kernel_size > K_MAX_KERNEL_SIZE) {
    return cudaErrorInvalidValue;
  }
  return cudaMemcpyToSymbol(c_gaussian_kernel, kernel, kernel_size * sizeof(float), 0,
                            cudaMemcpyHostToDevice);
}

__global__ void PackRgbToRgbaKernel(const unsigned char* input_rgb, unsigned char* output_rgba,
                                    int width, int height) {
  int x = blockIdx.x * blockDim.x + threadIdx.x;
  int y = blockIdx.y * blockDim.y + threadIdx.y;
  if (x >= width || y >= height) {
    return;
  }

  int rgb_idx = (y * width + x) * 3;
  int rgba_idx = (y * width + x) * 4;
  output_rgba[rgba_idx] = input_rgb[rgb_idx];
  output_rgba[rgba_idx + 1] = input_rgb[rgb_idx + 1];
  output_rgba[rgba_idx + 2] = input_rgb[rgb_idx + 2];
  output_rgba[rgba_idx + 3] = 255;
}

__global__ void UnpackRgbaToRgbKernel(const unsigned char* input_rgba, unsigned char* output_rgb,
                                      int width, int height) {
  int x = blockIdx.x * blockDim.x + threadIdx.x;
  int y = blockIdx.y * blockDim.y + threadIdx.y;
  if (x >= width || y >= height) {
    return;
  }

  int rgba_idx = (y * width + x) * 4;
  int rgb_idx = (y * width + x) * 3;
  output_rgb[rgb_idx] = input_rgba[rgba_idx];
  output_rgb[rgb_idx + 1] = input_rgba[rgba_idx + 1];
  output_rgb[rgb_idx + 2] = input_rgba[rgba_idx + 2];
}

// Horizontal pass: loads a tile into shared memory with halo pixels on each side.
__global__ void ApplyHorizontalBlurTiledKernel(const unsigned char* input, unsigned char* output,
                                               int width, int height, int channels, int radius,
                                               BorderMode border_mode) {
  extern __shared__ unsigned char tile[];
  const int tile_width = blockDim.x + 2 * radius;
  const int tile_height = blockDim.y;

  int x = blockIdx.x * blockDim.x + threadIdx.x;
  int y = blockIdx.y * blockDim.y + threadIdx.y;

  if (threadIdx.y < tile_height) {
    for (int sx = threadIdx.x; sx < tile_width; sx += blockDim.x) {
      int src_x = blockIdx.x * blockDim.x + sx - radius;
      int src_y = y;
      if (src_y < height) {
        int clamped_x = ClampX(src_x, width, border_mode);
        int src_idx = (src_y * width + clamped_x) * channels;
        int dst_idx = (threadIdx.y * tile_width + sx) * channels;
        for (int c = 0; c < channels; ++c) {
          tile[dst_idx + c] = input[src_idx + c];
        }
      }
    }
  }

  __syncthreads();

  if (x >= width || y >= height) {
    return;
  }

  int output_idx = (y * width + x) * channels;
  bool is_interior = (x >= radius) && (x < width - radius);
  for (int c = 0; c < channels; ++c) {
    float sum = 0.0F;
    if (is_interior) {
      int tile_base = (threadIdx.y * tile_width + threadIdx.x) * channels;
      for (int k = -radius; k <= radius; ++k) {
        int tile_idx = tile_base + (k + radius) * channels + c;
        sum += static_cast<float>(tile[tile_idx]) * c_gaussian_kernel[k + radius];
      }
    } else {
      for (int k = -radius; k <= radius; ++k) {
        int px = ClampX(x + k, width, border_mode);
        int idx = (y * width + px) * channels + c;
        sum += static_cast<float>(input[idx]) * c_gaussian_kernel[k + radius];
      }
    }

    output[output_idx + c] = static_cast<unsigned char>(max(0.0F, min(sum, 255.0F)));
  }
}

// Vertical pass: loads a tile into shared memory with halo rows on top and bottom.
__global__ void ApplyVerticalBlurTiledKernel(const unsigned char* input, unsigned char* output,
                                             int width, int height, int channels, int radius,
                                             BorderMode border_mode) {
  extern __shared__ unsigned char tile[];
  const int tile_width = blockDim.x;
  const int tile_height = blockDim.y + 2 * radius;

  int x = blockIdx.x * blockDim.x + threadIdx.x;
  int y = blockIdx.y * blockDim.y + threadIdx.y;

  if (threadIdx.x < tile_width) {
    for (int sy = threadIdx.y; sy < tile_height; sy += blockDim.y) {
      int src_x = x;
      int src_y = blockIdx.y * blockDim.y + sy - radius;
      if (src_x < width) {
        int clamped_y = ClampY(src_y, height, border_mode);
        int src_idx = (clamped_y * width + src_x) * channels;
        int dst_idx = (sy * tile_width + threadIdx.x) * channels;
        for (int c = 0; c < channels; ++c) {
          tile[dst_idx + c] = input[src_idx + c];
        }
      }
    }
  }

  __syncthreads();

  if (x >= width || y >= height) {
    return;
  }

  int output_idx = (y * width + x) * channels;
  bool is_interior = (y >= radius) && (y < height - radius);
  for (int c = 0; c < channels; ++c) {
    float sum = 0.0F;
    if (is_interior) {
      int tile_base = (threadIdx.y * tile_width + threadIdx.x) * channels;
      for (int k = -radius; k <= radius; ++k) {
        int tile_idx = tile_base + (k + radius) * tile_width * channels + c;
        sum += static_cast<float>(tile[tile_idx]) * c_gaussian_kernel[k + radius];
      }
    } else {
      for (int k = -radius; k <= radius; ++k) {
        int py = ClampY(y + k, height, border_mode);
        int idx = (py * width + x) * channels + c;
        sum += static_cast<float>(input[idx]) * c_gaussian_kernel[k + radius];
      }
    }

    output[output_idx + c] = static_cast<unsigned char>(max(0.0F, min(sum, 255.0F)));
  }
}

extern "C" cudaError_t cuda_apply_gaussian_blur_separable(const unsigned char* input,
                                                          unsigned char* output, int width,
                                                          int height, int channels,
                                                          const float* kernel, int kernel_size,
                                                          int border_mode, void* pool = nullptr) {
  unsigned char* d_input = nullptr;
  unsigned char* d_output = nullptr;
  unsigned char* d_temp = nullptr;
  float* d_kernel = nullptr;

  size_t data_size = static_cast<size_t>(width) * height * channels;

  cudaError_t error =
      AllocateMemoryPooled(width, height, channels, kernel_size, &d_input, &d_output, &d_temp,
                           &d_kernel, static_cast<CudaMemoryPool*>(pool));
  if (error != cudaSuccess) {
    return error;
  }

  error = cudaMemcpy(d_input, input, data_size, cudaMemcpyHostToDevice);
  if (error != cudaSuccess) {
    CleanupMemoryPooled(d_input, d_output, d_temp, d_kernel, data_size, kernel_size,
                        static_cast<CudaMemoryPool*>(pool));
    return error;
  }

  error = UploadKernelToConstantMemory(kernel, kernel_size);
  if (error != cudaSuccess) {
    CleanupMemoryPooled(d_input, d_output, d_temp, d_kernel, data_size, kernel_size,
                        static_cast<CudaMemoryPool*>(pool));
    return error;
  }

  BorderMode kernel_border_mode = static_cast<BorderMode>(border_mode);
  int radius = kernel_size / 2;
  dim3 block_size(32, 8);
  dim3 grid_size((width + block_size.x - 1) / block_size.x,
                 (height + block_size.y - 1) / block_size.y);
  size_t shared_horizontal =
      static_cast<size_t>(block_size.y) * (block_size.x + 2 * radius) * channels;
  size_t shared_vertical =
      static_cast<size_t>(block_size.x) * (block_size.y + 2 * radius) * channels;
  size_t shared_horizontal_rgba =
      static_cast<size_t>(block_size.y) * (block_size.x + 2 * radius) * 4;
  size_t shared_vertical_rgba = static_cast<size_t>(block_size.x) * (block_size.y + 2 * radius) * 4;

  bool use_packed_rgba = (channels == 3);
  unsigned char* d_input_rgba = nullptr;
  unsigned char* d_temp_rgba = nullptr;
  unsigned char* d_output_rgba = nullptr;
  size_t rgba_size = static_cast<size_t>(width) * height * 4;

  if (use_packed_rgba) {
    CudaMemoryPool* memory_pool = static_cast<CudaMemoryPool*>(pool);
    if (memory_pool == nullptr) {
      memory_pool = &GetThreadLocalMemoryPool();
    }
    d_input_rgba = static_cast<unsigned char*>(memory_pool->Allocate(rgba_size));
    d_temp_rgba = static_cast<unsigned char*>(memory_pool->Allocate(rgba_size));
    d_output_rgba = static_cast<unsigned char*>(memory_pool->Allocate(rgba_size));
    if (d_input_rgba == nullptr || d_temp_rgba == nullptr || d_output_rgba == nullptr) {
      if (d_input_rgba != nullptr) {
        memory_pool->Deallocate(d_input_rgba, rgba_size);
      }
      if (d_temp_rgba != nullptr) {
        memory_pool->Deallocate(d_temp_rgba, rgba_size);
      }
      if (d_output_rgba != nullptr) {
        memory_pool->Deallocate(d_output_rgba, rgba_size);
      }
      CleanupMemoryPooled(d_input, d_output, d_temp, d_kernel, data_size, kernel_size, memory_pool);
      return cudaErrorMemoryAllocation;
    }

    PackRgbToRgbaKernel<<<grid_size, block_size>>>(d_input, d_input_rgba, width, height);
    error = cudaGetLastError();
    if (error != cudaSuccess) {
      memory_pool->Deallocate(d_input_rgba, rgba_size);
      memory_pool->Deallocate(d_temp_rgba, rgba_size);
      memory_pool->Deallocate(d_output_rgba, rgba_size);
      CleanupMemoryPooled(d_input, d_output, d_temp, d_kernel, data_size, kernel_size, memory_pool);
      return error;
    }

    ApplyHorizontalBlurTiledKernel<<<grid_size, block_size, shared_horizontal_rgba>>>(
        d_input_rgba, d_temp_rgba, width, height, 4, radius, kernel_border_mode);

    error = cudaDeviceSynchronize();
    if (error != cudaSuccess) {
      memory_pool->Deallocate(d_input_rgba, rgba_size);
      memory_pool->Deallocate(d_temp_rgba, rgba_size);
      memory_pool->Deallocate(d_output_rgba, rgba_size);
      CleanupMemoryPooled(d_input, d_output, d_temp, d_kernel, data_size, kernel_size, memory_pool);
      return error;
    }

    ApplyVerticalBlurTiledKernel<<<grid_size, block_size, shared_vertical_rgba>>>(
        d_temp_rgba, d_output_rgba, width, height, 4, radius, kernel_border_mode);

    error = cudaDeviceSynchronize();
    if (error != cudaSuccess) {
      memory_pool->Deallocate(d_input_rgba, rgba_size);
      memory_pool->Deallocate(d_temp_rgba, rgba_size);
      memory_pool->Deallocate(d_output_rgba, rgba_size);
      CleanupMemoryPooled(d_input, d_output, d_temp, d_kernel, data_size, kernel_size, memory_pool);
      return error;
    }

    UnpackRgbaToRgbKernel<<<grid_size, block_size>>>(d_output_rgba, d_output, width, height);
    error = cudaGetLastError();
    if (error != cudaSuccess) {
      memory_pool->Deallocate(d_input_rgba, rgba_size);
      memory_pool->Deallocate(d_temp_rgba, rgba_size);
      memory_pool->Deallocate(d_output_rgba, rgba_size);
      CleanupMemoryPooled(d_input, d_output, d_temp, d_kernel, data_size, kernel_size, memory_pool);
      return error;
    }

    memory_pool->Deallocate(d_input_rgba, rgba_size);
    memory_pool->Deallocate(d_temp_rgba, rgba_size);
    memory_pool->Deallocate(d_output_rgba, rgba_size);
  } else {
    ApplyHorizontalBlurTiledKernel<<<grid_size, block_size, shared_horizontal>>>(
        d_input, d_temp, width, height, channels, radius, kernel_border_mode);

    error = cudaDeviceSynchronize();
    if (error != cudaSuccess) {
      CleanupMemoryPooled(d_input, d_output, d_temp, d_kernel, data_size, kernel_size,
                          static_cast<CudaMemoryPool*>(pool));
      return error;
    }

    ApplyVerticalBlurTiledKernel<<<grid_size, block_size, shared_vertical>>>(
        d_temp, d_output, width, height, channels, radius, kernel_border_mode);
  }

  error = cudaDeviceSynchronize();
  if (error != cudaSuccess) {
    CleanupMemoryPooled(d_input, d_output, d_temp, d_kernel, data_size, kernel_size,
                        static_cast<CudaMemoryPool*>(pool));
    return error;
  }

  error = cudaGetLastError();
  if (error != cudaSuccess) {
    CleanupMemoryPooled(d_input, d_output, d_temp, d_kernel, data_size, kernel_size,
                        static_cast<CudaMemoryPool*>(pool));
    return error;
  }

  error = cudaMemcpy(output, d_output, data_size, cudaMemcpyDeviceToHost);
  CleanupMemoryPooled(d_input, d_output, d_temp, d_kernel, data_size, kernel_size,
                      static_cast<CudaMemoryPool*>(pool));
  return error;
}

}  // namespace jrb::infrastructure::cuda
