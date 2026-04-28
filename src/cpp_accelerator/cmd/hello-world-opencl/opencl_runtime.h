#pragma once

#include <CL/cl.h>

namespace hw_opencl {

class OpenClRuntime {
public:
  OpenClRuntime();
  ~OpenClRuntime();

  bool Initialize();
  void Cleanup();

  cl_context Context() const;
  cl_device_id Device() const;
  cl_command_queue Queue() const;
  const char* LastErrorMessage() const;
  cl_int LastErrorCode() const;

private:
  bool SetLastError(cl_int code, const char* message);

  cl_platform_id platform_;
  cl_device_id device_;
  cl_context context_;
  cl_command_queue queue_;
  cl_int last_error_code_;
  const char* last_error_message_;
};

}  // namespace hw_opencl
