#include "src/cpp_accelerator/cmd/spike_multi_gpu_backend/grayscale_backend.h"

namespace jrb::spike::multi_backend {

bool IGrayscaleBackend::RunRgb888(const uint8_t* rgb, int width, int height, uint8_t* gray_out) {
  if (width <= 0 || height <= 0 || rgb == nullptr || gray_out == nullptr) {
    return false;
  }
  return RunImpl(rgb, width, height, gray_out);
}

}  // namespace jrb::spike::multi_backend
