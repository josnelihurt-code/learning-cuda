#include <cmath>
#include <iostream>
#include <vector>

#include "src/cpp_accelerator/cmd/hello-world-vulkan/vector_add.h"

int main() {
  hw_vulkan::VulkanVectorAddProgram program;
  if (!program.Initialize()) {
    std::cerr << program.LastErrorMessage() << ", Vulkan error " << program.LastErrorCode()
              << "\n";
    return 1;
  }

  constexpr int kN = 1024;
  std::vector<float> h_a(kN);
  std::vector<float> h_b(kN);
  std::vector<float> h_c(kN);
  for (int i = 0; i < kN; ++i) {
    h_a[i] = static_cast<float>(i);
    h_b[i] = static_cast<float>(i * 2);
  }

  hw_vulkan::VectorAddResult exec_result =
      program.Execute(h_a.data(), h_b.data(), h_c.data(), kN);
  if (exec_result.error_code != 0) {
    std::cerr << exec_result.error_message << ", Vulkan error " << exec_result.error_code << "\n";
    return 1;
  }

  for (int i = 0; i < kN; ++i) {
    float want = h_a[i] + h_b[i];
    if (std::abs(h_c[i] - want) > 1e-5F) {
      std::cerr << "Mismatch at " << i << ": want " << want << " got " << h_c[i] << "\n";
      return 1;
    }
  }

  std::cout << "Vulkan hello world OK (SPIR-V embedded in binary, n=" << kN << ")\n";
  return 0;
}
