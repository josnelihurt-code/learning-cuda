#include "src/cpp_accelerator/cmd/spike_multi_gpu_backend/cuda_grayscale_backend.h"

#include "src/cpp_accelerator/adapters/compute/cuda/kernels/grayscale_kernel.h"

namespace jrb::spike::multi_backend {

const char* CudaGrayscaleBackend::name() const {
  return "cuda";
}

bool CudaGrayscaleBackend::RunImpl(const uint8_t* rgb, int width, int height, uint8_t* gray_out) {
  const cudaError_t err = jrb::infrastructure::cuda::cuda_convert_to_grayscale(
      rgb, gray_out, width, height, 3, 0 /* BT601, matches mock */);
  return err == cudaSuccess;
}

}  // namespace jrb::spike::multi_backend
