#include <CL/cl.h>

#include <cmath>
#include <iostream>
#include <vector>

#include "src/cpp_accelerator/cmd/hello-world-opencl/vector_add.h"

int main() {
  hw_opencl::OpenClVectorAddProgram program;
  if (!program.Initialize()) {
    std::cerr << program.LastErrorMessage() << ", OpenCL error " << program.LastErrorCode() << "\n";
    return 1;
  }

  constexpr int n = 8;
  std::vector<float> h_a(n);
  std::vector<float> h_b(n);
  std::vector<float> h_c(n);
  for (int i = 0; i < n; ++i) {
    h_a[i] = static_cast<float>(i);
    h_b[i] = static_cast<float>(2 * i);
  }

  const hw_opencl::VectorAddResult exec_result =
      program.Execute(h_a.data(), h_b.data(), h_c.data(), n);
  if (exec_result.error_code != CL_SUCCESS) {
    std::cerr << exec_result.error_message << ", OpenCL error " << exec_result.error_code << "\n";
    return 1;
  }

  for (int i = 0; i < n; ++i) {
    const float want = h_a[i] + h_b[i];
    if (std::abs(h_c[i] - want) > 1e-5F) {
      std::cerr << "Mismatch at " << i << ": want " << want << " got " << h_c[i] << "\n";
      return 1;
    }
  }

  std::cout << "OpenCL hello world OK (n=" << n << ") — ";
  if (program.UsesEmbeddedIl()) {
    std::cout << "embedded SPIR-V IL (clCreateProgramWithIL)\n";
  } else {
    std::cout << "embedded OpenCL C (clCreateProgramWithSource)\n";
  }
  return 0;
}
