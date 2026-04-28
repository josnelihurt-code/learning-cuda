#pragma once

#include <cstdint>
#include <vulkan/vulkan.hpp>

namespace hw_vulkan {

class VulkanRuntime {
 public:
  VulkanRuntime();
  ~VulkanRuntime();

  bool Initialize();
  void Cleanup();

  vk::Device Device() const;
  vk::PhysicalDevice PhysicalDevice() const;
  vk::Queue Queue() const;
  uint32_t ComputeQueueFamilyIndex() const;
  int LastErrorCode() const;
  const char* LastErrorMessage() const;

 private:
  bool SetLastError(vk::Result code, const char* message);

  vk::Instance instance_;
  vk::PhysicalDevice physical_device_;
  vk::Device device_;
  vk::Queue queue_;
  uint32_t compute_queue_family_index_;
  int last_error_code_;
  const char* last_error_message_;
};

}  // namespace hw_vulkan
