#include "src/cpp_accelerator/cmd/hello-world-opencl/opencl_runtime.h"

namespace hw_opencl {

OpenClRuntime::OpenClRuntime()
    : platform_(nullptr),
      device_(nullptr),
      context_(nullptr),
      queue_(nullptr),
      last_error_code_(CL_SUCCESS),
      last_error_message_("ok") {}

OpenClRuntime::~OpenClRuntime() {
  Cleanup();
}

bool OpenClRuntime::Initialize() {
  Cleanup();

  cl_int err = clGetPlatformIDs(1, &platform_, nullptr);
  if (err != CL_SUCCESS || !platform_) {
    return SetLastError(err, "clGetPlatformIDs failed");
  }

  err = clGetDeviceIDs(platform_, CL_DEVICE_TYPE_DEFAULT, 1, &device_, nullptr);
  if (err != CL_SUCCESS || !device_) {
    return SetLastError(err, "clGetDeviceIDs failed");
  }

  context_ = clCreateContext(nullptr, 1, &device_, nullptr, nullptr, &err);
  if (err != CL_SUCCESS || !context_) {
    return SetLastError(err, "clCreateContext failed");
  }

  queue_ = clCreateCommandQueueWithProperties(context_, device_, nullptr, &err);
  if (err != CL_SUCCESS || !queue_) {
    return SetLastError(err, "clCreateCommandQueueWithProperties failed");
  }

  return SetLastError(CL_SUCCESS, "ok");
}

void OpenClRuntime::Cleanup() {
  if (queue_) clReleaseCommandQueue(queue_);
  if (context_) clReleaseContext(context_);
  platform_ = nullptr;
  device_ = nullptr;
  context_ = nullptr;
  queue_ = nullptr;
}

cl_context OpenClRuntime::Context() const {
  return context_;
}

cl_device_id OpenClRuntime::Device() const {
  return device_;
}

cl_command_queue OpenClRuntime::Queue() const {
  return queue_;
}

const char* OpenClRuntime::LastErrorMessage() const {
  return last_error_message_;
}

cl_int OpenClRuntime::LastErrorCode() const {
  return last_error_code_;
}

bool OpenClRuntime::SetLastError(cl_int code, const char* message) {
  last_error_code_ = code;
  last_error_message_ = message;
  return code == CL_SUCCESS;
}

}  // namespace hw_opencl
