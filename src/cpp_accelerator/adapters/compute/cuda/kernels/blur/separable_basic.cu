#include "src/cpp_accelerator/adapters/compute/cuda/kernels/blur/device_utils.cuh"

namespace jrb::infrastructure::cuda {

// Basic 1D horizontal blur: one thread per column, iterates over all rows.
// Simpler than the tiled version but lacks shared memory reuse.
__global__ void ApplyHorizontalBlurKernel(const unsigned char* input, unsigned char* output,
                                          int width, int height, int channels, const float* kernel,
                                          int kernel_size, BorderMode border_mode) {
  int x = blockIdx.x * blockDim.x + threadIdx.x;

  if (x >= width) {
    return;
  }

  int radius = kernel_size / 2;

  for (int y = 0; y < height; y++) {
    int output_idx = (y * width + x) * channels;

    for (int c = 0; c < channels; c++) {
      float sum = 0.0F;
      for (int k = -radius; k <= radius; k++) {
        int pixel_x = x + k;
        float weight = kernel[k + radius];
        sum += GetPixelChannelValue(input, pixel_x, y, width, height, channels, c, border_mode) *
               weight;
      }
      output[output_idx + c] = static_cast<unsigned char>(max(0.0F, min(sum, 255.0F)));
    }
  }
}

// Basic 1D vertical blur: one thread per row, iterates over all columns.
// Simpler than the tiled version but lacks shared memory reuse.
__global__ void ApplyVerticalBlurKernel(const unsigned char* input, unsigned char* output,
                                        int width, int height, int channels, const float* kernel,
                                        int kernel_size, BorderMode border_mode) {
  int y = blockIdx.y * blockDim.y + threadIdx.y;

  if (y >= height) {
    return;
  }

  int radius = kernel_size / 2;

  for (int x = 0; x < width; x++) {
    int output_idx = (y * width + x) * channels;

    for (int c = 0; c < channels; c++) {
      float sum = 0.0F;
      for (int k = -radius; k <= radius; k++) {
        int pixel_y = y + k;
        float weight = kernel[k + radius];
        sum += GetPixelChannelValue(input, x, pixel_y, width, height, channels, c, border_mode) *
               weight;
      }
      output[output_idx + c] = static_cast<unsigned char>(max(0.0F, min(sum, 255.0F)));
    }
  }
}

extern "C" cudaError_t cuda_apply_gaussian_blur_1d_horizontal(
    const unsigned char* input, unsigned char* output, int width, int height, int channels,
    const float* kernel, int kernel_size, int border_mode, void* pool = nullptr) {
  unsigned char* d_input = nullptr;
  unsigned char* d_output = nullptr;
  float* d_kernel = nullptr;

  size_t data_size = static_cast<size_t>(width) * height * channels;

  cudaError_t error =
      AllocateMemoryPooled(width, height, channels, kernel_size, &d_input, &d_output, nullptr,
                           &d_kernel, static_cast<CudaMemoryPool*>(pool));
  if (error != cudaSuccess)
    return error;

  error = cudaMemcpy(d_input, input, data_size, cudaMemcpyHostToDevice);
  if (error != cudaSuccess) {
    CleanupMemoryPooled(d_input, d_output, nullptr, d_kernel, data_size, kernel_size,
                        static_cast<CudaMemoryPool*>(pool));
    return error;
  }

  error = cudaMemcpy(d_kernel, kernel, kernel_size * sizeof(float), cudaMemcpyHostToDevice);
  if (error != cudaSuccess) {
    CleanupMemoryPooled(d_input, d_output, nullptr, d_kernel, data_size, kernel_size,
                        static_cast<CudaMemoryPool*>(pool));
    return error;
  }

  BorderMode kernel_border_mode = static_cast<BorderMode>(border_mode);
  dim3 block_size(256);
  dim3 grid_size((width + block_size.x - 1) / block_size.x);
  ApplyHorizontalBlurKernel<<<grid_size, block_size>>>(d_input, d_output, width, height, channels,
                                                       d_kernel, kernel_size, kernel_border_mode);

  error = cudaDeviceSynchronize();
  if (error != cudaSuccess) {
    CleanupMemoryPooled(d_input, d_output, nullptr, d_kernel, data_size, kernel_size,
                        static_cast<CudaMemoryPool*>(pool));
    return error;
  }

  error = cudaGetLastError();
  if (error != cudaSuccess) {
    CleanupMemoryPooled(d_input, d_output, nullptr, d_kernel, data_size, kernel_size,
                        static_cast<CudaMemoryPool*>(pool));
    return error;
  }

  error = cudaMemcpy(output, d_output, data_size, cudaMemcpyDeviceToHost);
  CleanupMemoryPooled(d_input, d_output, nullptr, d_kernel, data_size, kernel_size,
                      static_cast<CudaMemoryPool*>(pool));
  return error;
}

extern "C" cudaError_t cuda_apply_gaussian_blur_1d_vertical(const unsigned char* input,
                                                            unsigned char* output, int width,
                                                            int height, int channels,
                                                            const float* kernel, int kernel_size,
                                                            int border_mode, void* pool = nullptr) {
  unsigned char* d_input = nullptr;
  unsigned char* d_output = nullptr;
  float* d_kernel = nullptr;

  size_t data_size = static_cast<size_t>(width) * height * channels;

  cudaError_t error =
      AllocateMemoryPooled(width, height, channels, kernel_size, &d_input, &d_output, nullptr,
                           &d_kernel, static_cast<CudaMemoryPool*>(pool));
  if (error != cudaSuccess)
    return error;

  error = cudaMemcpy(d_input, input, data_size, cudaMemcpyHostToDevice);
  if (error != cudaSuccess) {
    CleanupMemoryPooled(d_input, d_output, nullptr, d_kernel, data_size, kernel_size,
                        static_cast<CudaMemoryPool*>(pool));
    return error;
  }

  error = cudaMemcpy(d_kernel, kernel, kernel_size * sizeof(float), cudaMemcpyHostToDevice);
  if (error != cudaSuccess) {
    CleanupMemoryPooled(d_input, d_output, nullptr, d_kernel, data_size, kernel_size,
                        static_cast<CudaMemoryPool*>(pool));
    return error;
  }

  BorderMode kernel_border_mode = static_cast<BorderMode>(border_mode);
  dim3 block_size(16, 16);
  dim3 grid_size((width + block_size.x - 1) / block_size.x,
                 (height + block_size.y - 1) / block_size.y);
  ApplyVerticalBlurKernel<<<grid_size, block_size>>>(d_input, d_output, width, height, channels,
                                                     d_kernel, kernel_size, kernel_border_mode);

  error = cudaDeviceSynchronize();
  if (error != cudaSuccess) {
    CleanupMemoryPooled(d_input, d_output, nullptr, d_kernel, data_size, kernel_size,
                        static_cast<CudaMemoryPool*>(pool));
    return error;
  }

  error = cudaGetLastError();
  if (error != cudaSuccess) {
    CleanupMemoryPooled(d_input, d_output, nullptr, d_kernel, data_size, kernel_size,
                        static_cast<CudaMemoryPool*>(pool));
    return error;
  }

  error = cudaMemcpy(output, d_output, data_size, cudaMemcpyDeviceToHost);
  CleanupMemoryPooled(d_input, d_output, nullptr, d_kernel, data_size, kernel_size,
                      static_cast<CudaMemoryPool*>(pool));
  return error;
}

}  // namespace jrb::infrastructure::cuda
