#pragma once

#include <CL/cl.h>

#include "src/cpp_accelerator/cmd/hello-world-opencl/opencl_program.h"
#include "src/cpp_accelerator/cmd/hello-world-opencl/opencl_runtime.h"

namespace hw_opencl {

struct VectorAddResult {
  cl_int error_code = CL_SUCCESS;
  const char* error_message = "ok";
};

class OpenClVectorAddProgram {
public:
  OpenClVectorAddProgram();
  ~OpenClVectorAddProgram();

  bool Initialize();
  VectorAddResult Execute(const float* a, const float* b, float* c, int n);
  bool UsesEmbeddedIl() const;
  const char* LastErrorMessage() const;
  cl_int LastErrorCode() const;

private:
  VectorAddResult MakeError(cl_int code, const char* message);
  bool SetLastError(cl_int code, const char* message);

  OpenClRuntime runtime_;
  OpenClProgram program_;
  cl_int last_error_code_;
  const char* last_error_message_;
};

}  // namespace hw_opencl
