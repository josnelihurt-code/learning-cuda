#include "src/cpp_accelerator/adapters/compute/cuda/kernels/blur/device_utils.cuh"

namespace jrb::infrastructure::cuda {

// 2D non-separable blur: applies a 2D outer product kernel in a single pass.
// Simpler than the two-pass separable approach, at the cost of O(r^2) multiplies per pixel.
__global__ void ApplyFullBlurKernel(const unsigned char* input, unsigned char* output, int width,
                                    int height, int channels, const float* kernel, int kernel_size,
                                    BorderMode border_mode) {
  int x = blockIdx.x * blockDim.x + threadIdx.x;
  int y = blockIdx.y * blockDim.y + threadIdx.y;

  if (x >= width || y >= height) {
    return;
  }

  int radius = kernel_size / 2;
  int output_idx = (y * width + x) * channels;

  for (int c = 0; c < channels; c++) {
    float sum = 0.0F;
    for (int ky = -radius; ky <= radius; ky++) {
      for (int kx = -radius; kx <= radius; kx++) {
        int pixel_x = x + kx;
        int pixel_y = y + ky;
        float weight_y = kernel[ky + radius];
        float weight_x = kernel[kx + radius];
        float weight = weight_x * weight_y;
        sum +=
            GetPixelChannelValue(input, pixel_x, pixel_y, width, height, channels, c, border_mode) *
            weight;
      }
    }
    output[output_idx + c] = static_cast<unsigned char>(max(0.0F, min(sum, 255.0F)));
  }
}

extern "C" cudaError_t cuda_apply_gaussian_blur_non_separable(
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
  dim3 block_size(16, 16);
  dim3 grid_size((width + block_size.x - 1) / block_size.x,
                 (height + block_size.y - 1) / block_size.y);
  ApplyFullBlurKernel<<<grid_size, block_size>>>(d_input, d_output, width, height, channels,
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
