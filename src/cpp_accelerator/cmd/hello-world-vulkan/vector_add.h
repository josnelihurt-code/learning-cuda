#pragma once

#include <vulkan/vulkan.hpp>

#include "src/cpp_accelerator/cmd/hello-world-vulkan/vulkan_program.h"
#include "src/cpp_accelerator/cmd/hello-world-vulkan/vulkan_runtime.h"

namespace hw_vulkan {

struct VectorAddResult {
  int error_code = 0;
  const char* error_message = "ok";
};

class VulkanVectorAddProgram {
 public:
  VulkanVectorAddProgram();
  ~VulkanVectorAddProgram();

  bool Initialize();
  VectorAddResult Execute(const float* a, const float* b, float* c, int n);
  int LastErrorCode() const;
  const char* LastErrorMessage() const;

 private:
  VectorAddResult MakeError(vk::Result code, const char* message);
  bool SetLastError(vk::Result code, const char* message);

  VulkanRuntime runtime_;
  VulkanProgram program_;
  int last_error_code_;
  const char* last_error_message_;
};

}  // namespace hw_vulkan
