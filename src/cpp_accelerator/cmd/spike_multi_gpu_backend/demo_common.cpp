#include "src/cpp_accelerator/cmd/spike_multi_gpu_backend/demo_common.h"

#include <vector>

#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wdeprecated-declarations"
#include <spdlog/spdlog.h>
#pragma GCC diagnostic pop

namespace jrb::spike::multi_backend {

namespace {

constexpr int kDemoW = 4;
constexpr int kDemoH = 4;

void fill_checker_rgb(std::vector<uint8_t>& rgb) {
  rgb.resize(static_cast<size_t>(kDemoW) * static_cast<size_t>(kDemoH) * 3);
  for (int y = 0; y < kDemoH; ++y) {
    for (int x = 0; x < kDemoW; ++x) {
      const size_t p = (static_cast<size_t>(y) * static_cast<size_t>(kDemoW) + static_cast<size_t>(x)) * 3;
      const bool dark = ((x + y) & 1) == 0;
      rgb[p] = dark ? 32 : 200;
      rgb[p + 1] = dark ? 64 : 180;
      rgb[p + 2] = dark ? 96 : 160;
    }
  }
}

uint32_t checksum_gray(const uint8_t* gray, int n) {
  uint32_t s = 0;
  for (int i = 0; i < n; ++i) {
    s = s * 131u + static_cast<uint32_t>(gray[i]);
  }
  return s;
}

}  // namespace

int RunGrayscaleDemo(IGrayscaleBackend& backend) {
  spdlog::info("spike_multi_gpu_backend: backend={}", backend.name());

  std::vector<uint8_t> rgb;
  fill_checker_rgb(rgb);
  std::vector<uint8_t> gray(static_cast<size_t>(kDemoW) * static_cast<size_t>(kDemoH), 0);

  if (!backend.RunRgb888(rgb.data(), kDemoW, kDemoH, gray.data())) {
    spdlog::error("grayscale run failed");
    return 1;
  }

  spdlog::info("output checksum={:08x} first_row=[{:02x} {:02x} {:02x} {:02x}]",
                static_cast<unsigned>(checksum_gray(gray.data(), kDemoW * kDemoH)), gray[0], gray[1],
                gray[2], gray[3]);
  return 0;
}

}  // namespace jrb::spike::multi_backend
