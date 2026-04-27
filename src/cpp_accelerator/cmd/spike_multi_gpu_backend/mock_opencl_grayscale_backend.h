#pragma once

#include "src/cpp_accelerator/cmd/spike_multi_gpu_backend/grayscale_backend.h"

namespace jrb::spike::multi_backend {

class MockOpenClGrayscaleBackend final : public IGrayscaleBackend {
 public:
  const char* name() const override;

 protected:
  bool RunImpl(const uint8_t* rgb, int width, int height, uint8_t* gray_out) override;
};

}  // namespace jrb::spike::multi_backend
