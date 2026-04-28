#pragma once

#include <cstddef>
#include <vulkan/vulkan.hpp>

namespace hw_vulkan {

class VulkanRuntime;

class VulkanProgram {
 public:
  VulkanProgram();
  ~VulkanProgram();

  bool InitializeFromEmbedded(const VulkanRuntime& runtime, const void* spirv,
                              size_t size_bytes);
  void Cleanup();

  vk::ShaderModule Handle() const;
  int LastErrorCode() const;
  const char* LastErrorMessage() const;

 private:
  bool SetLastError(vk::Result code, const char* message);

  vk::Device device_;
  vk::ShaderModule shader_module_;
  int last_error_code_;
  const char* last_error_message_;
};

}  // namespace hw_vulkan
