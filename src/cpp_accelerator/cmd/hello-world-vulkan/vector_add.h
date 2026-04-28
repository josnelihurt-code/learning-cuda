#pragma once

#include <vulkan/vulkan.hpp>

namespace hw_vulkan {

struct VulkanComputeContext {
  vk::Device device;
  vk::PhysicalDevice physical_device;
  uint32_t compute_queue_family_index;
  vk::Queue queue;
};

struct VectorAddResult {
  vk::Result result;
  const char* error_message;
};

VectorAddResult vector_add(const float* A, const float* B, float* C, int n,
                           const VulkanComputeContext* ctx);

}  // namespace hw_vulkan
