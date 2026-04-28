#pragma once

#include <CL/cl.h>
#include <cstddef>

#include "src/cpp_accelerator/cmd/hello-world-opencl/opencl_runtime.h"

namespace hw_opencl {

class OpenClProgram {
public:
  OpenClProgram();
  ~OpenClProgram();

  bool InitializeFromEmbedded(const OpenClRuntime& runtime, const void* il, size_t il_size, const char* src,
                              size_t src_len);
  void Cleanup();

  cl_program Handle() const;
  bool UsesEmbeddedIl() const;
  const char* LastErrorMessage() const;
  cl_int LastErrorCode() const;

private:
  bool SetLastError(cl_int code, const char* message);

  cl_program program_;
  bool from_il_;
  cl_int last_error_code_;
  const char* last_error_message_;
};

}  // namespace hw_opencl
