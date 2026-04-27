#pragma once

#include <cstdint>

namespace jrb::spike::multi_backend {

class IGrayscaleBackend {
 public:
  virtual ~IGrayscaleBackend() = default;

  virtual const char* name() const = 0;

  bool RunRgb888(const uint8_t* rgb, int width, int height, uint8_t* gray_out);

 protected:
  virtual bool RunImpl(const uint8_t* rgb, int width, int height, uint8_t* gray_out) = 0;
};

}  // namespace jrb::spike::multi_backend
