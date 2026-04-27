#pragma once

#include "src/cpp_accelerator/cmd/spike_multi_gpu_backend/grayscale_backend.h"

namespace jrb::spike::multi_backend {

int RunGrayscaleDemo(IGrayscaleBackend& backend);

}  // namespace jrb::spike::multi_backend
