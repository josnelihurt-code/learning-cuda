#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wdeprecated-declarations"
#include <spdlog/spdlog.h>
#pragma GCC diagnostic pop

#include "src/cpp_accelerator/cmd/spike_multi_gpu_backend/demo_common.h"
#include "src/cpp_accelerator/cmd/spike_multi_gpu_backend/mock_opencl_grayscale_backend.h"

int main() {
  spdlog::set_level(spdlog::level::info);
  spdlog::info(
      "binary=spike_opencl_mock (select mock OpenCL path by running this executable or the "
      "opencl-mock Docker image; no OpenCL SDK)");

  jrb::spike::multi_backend::MockOpenClGrayscaleBackend backend;
  return jrb::spike::multi_backend::RunGrayscaleDemo(backend);
}
