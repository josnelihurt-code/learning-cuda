#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wdeprecated-declarations"
#include <spdlog/spdlog.h>
#pragma GCC diagnostic pop

#include "src/cpp_accelerator/cmd/spike_multi_gpu_backend/cuda_grayscale_backend.h"
#include "src/cpp_accelerator/cmd/spike_multi_gpu_backend/demo_common.h"

int main() {
  spdlog::set_level(spdlog::level::info);
  spdlog::info("binary=spike_cuda (select CUDA by running this executable or the cuda Docker image)");

  jrb::spike::multi_backend::CudaGrayscaleBackend backend;
  return jrb::spike::multi_backend::RunGrayscaleDemo(backend);
}
