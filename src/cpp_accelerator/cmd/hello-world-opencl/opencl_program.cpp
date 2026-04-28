#include "src/cpp_accelerator/cmd/hello-world-opencl/opencl_program.h"

namespace hw_opencl {

OpenClProgram::OpenClProgram()
    : program_(nullptr), from_il_(false), last_error_code_(CL_SUCCESS), last_error_message_("ok") {}

OpenClProgram::~OpenClProgram() {
  Cleanup();
}

bool OpenClProgram::InitializeFromEmbedded(const OpenClRuntime& runtime, const void* il, size_t il_size,
                                           const char* src, size_t src_len) {
  Cleanup();
  if (!runtime.Context() || !runtime.Device()) {
    return SetLastError(CL_INVALID_CONTEXT, "OpenCL runtime is not initialized");
  }

  cl_int err = CL_SUCCESS;
  if (il && il_size > 0 && (il_size % sizeof(cl_uint)) == 0) {
    program_ = clCreateProgramWithIL(runtime.Context(), il, il_size, &err);
    if (err == CL_SUCCESS && program_) {
      from_il_ = true;
    } else {
      program_ = nullptr;
    }
  }

  if (!program_) {
    if (!src || src_len == 0) {
      return SetLastError(CL_INVALID_BINARY, "Embedded OpenCL C source is empty");
    }
    program_ = clCreateProgramWithSource(runtime.Context(), 1, &src, &src_len, &err);
    if (err != CL_SUCCESS || !program_) {
      return SetLastError(err, "clCreateProgramWithSource failed");
    }
    from_il_ = false;
  }

  cl_device_id device = runtime.Device();
  err = clBuildProgram(program_, 1, &device, nullptr, nullptr, nullptr);
  if (err != CL_SUCCESS) {
    return SetLastError(err, "clBuildProgram failed");
  }

  return SetLastError(CL_SUCCESS, "ok");
}

void OpenClProgram::Cleanup() {
  if (program_) clReleaseProgram(program_);
  program_ = nullptr;
  from_il_ = false;
}

cl_program OpenClProgram::Handle() const {
  return program_;
}

bool OpenClProgram::UsesEmbeddedIl() const {
  return from_il_;
}

const char* OpenClProgram::LastErrorMessage() const {
  return last_error_message_;
}

cl_int OpenClProgram::LastErrorCode() const {
  return last_error_code_;
}

bool OpenClProgram::SetLastError(cl_int code, const char* message) {
  last_error_code_ = code;
  last_error_message_ = message;
  return code == CL_SUCCESS;
}

}  // namespace hw_opencl
