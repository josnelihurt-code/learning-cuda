#include "src/cpp_accelerator/cmd/hello-world-opencl/vector_add.h"

#include <cstddef>

#include "src/cpp_accelerator/cmd/hello-world-opencl/vector_add_kernel_blob.h"

namespace hw_opencl {

OpenClVectorAddProgram::OpenClVectorAddProgram()
    : runtime_(), program_(), last_error_code_(CL_SUCCESS), last_error_message_("ok") {}

OpenClVectorAddProgram::~OpenClVectorAddProgram() = default;

bool OpenClVectorAddProgram::Initialize() {
  if (!runtime_.Initialize())
    return SetLastError(runtime_.LastErrorCode(), runtime_.LastErrorMessage());

  const size_t il_size = vector_add_kernel_blob::spirv_size_bytes();
  const void* il_ptr = vector_add_kernel_blob::spirv();
  const size_t src_len = vector_add_kernel_blob::cl_src_size_bytes();
  const char* src_ptr = vector_add_kernel_blob::cl_src();

  if (!program_.InitializeFromEmbedded(runtime_, il_ptr, il_size, src_ptr, src_len)) {
    return SetLastError(program_.LastErrorCode(), program_.LastErrorMessage());
  }
  return SetLastError(CL_SUCCESS, "ok");
}

VectorAddResult OpenClVectorAddProgram::Execute(const float* a, const float* b, float* c, int n) {
  if (!runtime_.Context() || !runtime_.Queue() || !program_.Handle()) {
    return MakeError(CL_INVALID_PROGRAM, "Program is not initialized");
  }
  if (!a || !b || !c || n <= 0) {
    return MakeError(CL_INVALID_VALUE, "Invalid execute arguments");
  }

  cl_int err = CL_SUCCESS;
  cl_mem d_a = clCreateBuffer(runtime_.Context(), CL_MEM_READ_ONLY | CL_MEM_COPY_HOST_PTR,
                              static_cast<size_t>(n) * sizeof(float), const_cast<float*>(a), &err);
  if (err != CL_SUCCESS || !d_a)
    return MakeError(err, "clCreateBuffer A failed");

  cl_mem d_b = clCreateBuffer(runtime_.Context(), CL_MEM_READ_ONLY | CL_MEM_COPY_HOST_PTR,
                              static_cast<size_t>(n) * sizeof(float), const_cast<float*>(b), &err);
  if (err != CL_SUCCESS || !d_b) {
    clReleaseMemObject(d_a);
    return MakeError(err, "clCreateBuffer B failed");
  }

  cl_mem d_c = clCreateBuffer(runtime_.Context(), CL_MEM_WRITE_ONLY,
                              static_cast<size_t>(n) * sizeof(float), nullptr, &err);
  if (err != CL_SUCCESS || !d_c) {
    clReleaseMemObject(d_a);
    clReleaseMemObject(d_b);
    return MakeError(err, "clCreateBuffer C failed");
  }

  cl_kernel kernel = clCreateKernel(program_.Handle(), "vector_add_kernel", &err);
  if (err != CL_SUCCESS || !kernel) {
    clReleaseMemObject(d_a);
    clReleaseMemObject(d_b);
    clReleaseMemObject(d_c);
    return MakeError(err, "clCreateKernel failed");
  }

  err = clSetKernelArg(kernel, 0, sizeof(cl_mem), &d_a);
  if (err == CL_SUCCESS)
    err = clSetKernelArg(kernel, 1, sizeof(cl_mem), &d_b);
  if (err == CL_SUCCESS)
    err = clSetKernelArg(kernel, 2, sizeof(cl_mem), &d_c);
  if (err == CL_SUCCESS)
    err = clSetKernelArg(kernel, 3, sizeof(int), &n);
  if (err != CL_SUCCESS) {
    clReleaseKernel(kernel);
    clReleaseMemObject(d_a);
    clReleaseMemObject(d_b);
    clReleaseMemObject(d_c);
    return MakeError(err, "clSetKernelArg failed");
  }

  const size_t global = static_cast<size_t>(n);
  err = clEnqueueNDRangeKernel(runtime_.Queue(), kernel, 1, nullptr, &global, nullptr, 0, nullptr,
                               nullptr);
  if (err == CL_SUCCESS) {
    err = clEnqueueReadBuffer(runtime_.Queue(), d_c, CL_TRUE, 0,
                              static_cast<size_t>(n) * sizeof(float), c, 0, nullptr, nullptr);
  }
  if (err == CL_SUCCESS) {
    err = clFinish(runtime_.Queue());
  }

  clReleaseKernel(kernel);
  clReleaseMemObject(d_a);
  clReleaseMemObject(d_b);
  clReleaseMemObject(d_c);

  if (err != CL_SUCCESS)
    return MakeError(err, "OpenCL execution failed");

  return MakeError(CL_SUCCESS, "ok");
}

bool OpenClVectorAddProgram::UsesEmbeddedIl() const {
  return program_.UsesEmbeddedIl();
}

const char* OpenClVectorAddProgram::LastErrorMessage() const {
  return last_error_message_;
}

cl_int OpenClVectorAddProgram::LastErrorCode() const {
  return last_error_code_;
}

VectorAddResult OpenClVectorAddProgram::MakeError(cl_int code, const char* message) {
  SetLastError(code, message);
  return VectorAddResult{code, message};
}

bool OpenClVectorAddProgram::SetLastError(cl_int code, const char* message) {
  last_error_code_ = code;
  last_error_message_ = message;
  return code == CL_SUCCESS;
}

}  // namespace hw_opencl
