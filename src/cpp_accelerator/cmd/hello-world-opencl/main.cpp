#include <CL/cl.h>

#include <cmath>
#include <cstring>
#include <iostream>
#include <vector>

#include "src/cpp_accelerator/cmd/hello-world-opencl/vector_add_kernel_blob.h"

static void die_cl(cl_int err, const char* what) {
  std::cerr << what << " failed, OpenCL error " << err << "\n";
  std::exit(1);
}

int main() {
  cl_int err;

  cl_platform_id platform = nullptr;
  err = clGetPlatformIDs(1, &platform, nullptr);
  if (err != CL_SUCCESS || !platform) die_cl(err, "clGetPlatformIDs");

  cl_device_id device = nullptr;
  err = clGetDeviceIDs(platform, CL_DEVICE_TYPE_DEFAULT, 1, &device, nullptr);
  if (err != CL_SUCCESS || !device) die_cl(err, "clGetDeviceIDs");

  cl_context ctx = clCreateContext(nullptr, 1, &device, nullptr, nullptr, &err);
  if (err != CL_SUCCESS || !ctx) die_cl(err, "clCreateContext");

  cl_command_queue queue = clCreateCommandQueueWithProperties(ctx, device, nullptr, &err);
  if (err != CL_SUCCESS || !queue) die_cl(err, "clCreateCommandQueueWithProperties");

  cl_program program = nullptr;
  bool from_il = false;

  const size_t il_size = vector_add_kernel_blob::spirv_size_bytes();
  if (il_size > 0 && (il_size % 4) == 0) {
    program = clCreateProgramWithIL(ctx, vector_add_kernel_blob::spirv(), il_size, &err);
    if (err == CL_SUCCESS && program) {
      from_il = true;
    } else {
      program = nullptr;
    }
  }

  if (!program) {
    const size_t src_len = vector_add_kernel_blob::cl_src_size_bytes();
    if (src_len == 0) {
      std::cerr << "Embedded OpenCL C source is empty\n";
      clReleaseCommandQueue(queue);
      clReleaseContext(ctx);
      return 1;
    }
    const char* src_ptr = vector_add_kernel_blob::cl_src();
    program = clCreateProgramWithSource(ctx, 1, &src_ptr, &src_len, &err);
    if (err != CL_SUCCESS || !program) die_cl(err, "clCreateProgramWithSource");
    from_il = false;
  }

  err = clBuildProgram(program, 1, &device, nullptr, nullptr, nullptr);
  if (err != CL_SUCCESS) {
    std::cerr << "clBuildProgram failed, error " << err << "\n";
    clReleaseProgram(program);
    clReleaseCommandQueue(queue);
    clReleaseContext(ctx);
    return 1;
  }

  constexpr int n = 8;
  std::vector<float> h_a(n), h_b(n), h_c(n);
  for (int i = 0; i < n; ++i) {
    h_a[i] = static_cast<float>(i);
    h_b[i] = static_cast<float>(2 * i);
  }

  cl_mem d_a = clCreateBuffer(ctx, CL_MEM_READ_ONLY | CL_MEM_COPY_HOST_PTR, n * sizeof(float),
                              h_a.data(), &err);
  if (err != CL_SUCCESS) die_cl(err, "clCreateBuffer A");
  cl_mem d_b = clCreateBuffer(ctx, CL_MEM_READ_ONLY | CL_MEM_COPY_HOST_PTR, n * sizeof(float),
                              h_b.data(), &err);
  if (err != CL_SUCCESS) {
    clReleaseMemObject(d_a);
    die_cl(err, "clCreateBuffer B");
  }
  cl_mem d_c = clCreateBuffer(ctx, CL_MEM_WRITE_ONLY, n * sizeof(float), nullptr, &err);
  if (err != CL_SUCCESS) {
    clReleaseMemObject(d_a);
    clReleaseMemObject(d_b);
    die_cl(err, "clCreateBuffer C");
  }

  cl_kernel k = clCreateKernel(program, "vector_add_kernel", &err);
  if (err != CL_SUCCESS || !k) {
    clReleaseMemObject(d_a);
    clReleaseMemObject(d_b);
    clReleaseMemObject(d_c);
    clReleaseProgram(program);
    die_cl(err, "clCreateKernel");
  }

  clSetKernelArg(k, 0, sizeof(cl_mem), &d_a);
  clSetKernelArg(k, 1, sizeof(cl_mem), &d_b);
  clSetKernelArg(k, 2, sizeof(cl_mem), &d_c);
  clSetKernelArg(k, 3, sizeof(int), &n);

  const size_t global = static_cast<size_t>(n);
  err = clEnqueueNDRangeKernel(queue, k, 1, nullptr, &global, nullptr, 0, nullptr, nullptr);
  if (err != CL_SUCCESS) {
    clReleaseKernel(k);
    clReleaseMemObject(d_a);
    clReleaseMemObject(d_b);
    clReleaseMemObject(d_c);
    clReleaseProgram(program);
    clReleaseCommandQueue(queue);
    clReleaseContext(ctx);
    die_cl(err, "clEnqueueNDRangeKernel");
  }

  err = clEnqueueReadBuffer(queue, d_c, CL_TRUE, 0, n * sizeof(float), h_c.data(), 0, nullptr,
                            nullptr);
  clFinish(queue);

  clReleaseKernel(k);
  clReleaseMemObject(d_a);
  clReleaseMemObject(d_b);
  clReleaseMemObject(d_c);
  clReleaseProgram(program);
  clReleaseCommandQueue(queue);
  clReleaseContext(ctx);

  if (err != CL_SUCCESS) die_cl(err, "clEnqueueReadBuffer");

  for (int i = 0; i < n; ++i) {
    const float want = h_a[i] + h_b[i];
    if (std::abs(h_c[i] - want) > 1e-5f) {
      std::cerr << "Mismatch at " << i << ": want " << want << " got " << h_c[i] << "\n";
      return 1;
    }
  }

  std::cout << "OpenCL hello world OK (n=" << n << ") — ";
  if (from_il) {
    std::cout << "embedded SPIR-V IL (clCreateProgramWithIL)\n";
  } else {
    std::cout << "embedded OpenCL C (clCreateProgramWithSource)\n";
  }
  return 0;
}
