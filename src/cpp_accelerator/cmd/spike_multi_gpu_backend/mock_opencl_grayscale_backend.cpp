#include "src/cpp_accelerator/cmd/spike_multi_gpu_backend/mock_opencl_grayscale_backend.h"

#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wdeprecated-declarations"
#include <spdlog/spdlog.h>
#pragma GCC diagnostic pop

namespace jrb::spike::multi_backend {

namespace {

unsigned char bt601_gray(unsigned char r, unsigned char g, unsigned char b) {
  return static_cast<unsigned char>(0.299f * static_cast<float>(r) + 0.587f * static_cast<float>(g) +
                                     0.114f * static_cast<float>(b));
}

}  // namespace

const char* MockOpenClGrayscaleBackend::name() const {
  return "opencl_mock";
}

bool MockOpenClGrayscaleBackend::RunImpl(const uint8_t* rgb, int width, int height,
                                         uint8_t* gray_out) {
  spdlog::info(
      "[opencl_mock] would enqueue grayscale kernel (BT601), grid {}x{}, channels=3 -> 1", width,
      height);

  for (int y = 0; y < height; ++y) {
    for (int x = 0; x < width; ++x) {
      const int p = y * width + x;
      const int i = p * 3;
      gray_out[p] = bt601_gray(rgb[i], rgb[i + 1], rgb[i + 2]);
    }
  }
  return true;
}

}  // namespace jrb::spike::multi_backend
