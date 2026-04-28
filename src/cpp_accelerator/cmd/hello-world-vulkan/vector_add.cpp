#include "src/cpp_accelerator/cmd/hello-world-vulkan/vector_add.hpp"

#include <cstring>
#include <vector>

#include "src/cpp_accelerator/cmd/hello-world-vulkan/vector_add_kernel_blob.h"

namespace hw_vulkan {

static std::vector<uint32_t> LoadSpirvBinary() {
  const size_t n = vector_add_kernel_blob::spirv_size_bytes();
  if (n == 0 || n % sizeof(uint32_t) != 0) return {};
  std::vector<uint32_t> spirv(n / sizeof(uint32_t));
  std::memcpy(spirv.data(), vector_add_kernel_blob::spirv(), n);
  return spirv;
}

static VectorAddResult MakeError(vk::Result r, const char* msg) {
  return VectorAddResult{r, msg};
}

VectorAddResult vector_add(const float* A, const float* B, float* C, int n, const VulkanComputeContext* ctx) {
  auto spirv = LoadSpirvBinary();
  if (spirv.empty()) {
    return MakeError(vk::Result::eErrorInitializationFailed, "Invalid embedded SPIR-V");
  }

  vk::ShaderModule shader_module = nullptr;
  vk::DescriptorSetLayout descriptor_set_layout = nullptr;
  vk::PipelineLayout pipeline_layout = nullptr;
  vk::Pipeline compute_pipeline = nullptr;
  vk::DescriptorPool descriptor_pool = nullptr;
  vk::CommandPool cmd_pool = nullptr;
  vk::Fence fence = nullptr;
  vk::Buffer buf_a = nullptr, buf_b = nullptr, buf_c = nullptr;
  vk::DeviceMemory mem_a = nullptr, mem_b = nullptr, mem_c = nullptr;

  auto cleanup = [&]() {
    if (fence) ctx->device.destroyFence(fence);
    if (cmd_pool) ctx->device.destroyCommandPool(cmd_pool);
    if (mem_c) ctx->device.freeMemory(mem_c);
    if (mem_b) ctx->device.freeMemory(mem_b);
    if (mem_a) ctx->device.freeMemory(mem_a);
    if (buf_c) ctx->device.destroyBuffer(buf_c);
    if (buf_b) ctx->device.destroyBuffer(buf_b);
    if (buf_a) ctx->device.destroyBuffer(buf_a);
    if (descriptor_pool) ctx->device.destroyDescriptorPool(descriptor_pool);
    if (compute_pipeline) ctx->device.destroyPipeline(compute_pipeline);
    if (pipeline_layout) ctx->device.destroyPipelineLayout(pipeline_layout);
    if (descriptor_set_layout) ctx->device.destroyDescriptorSetLayout(descriptor_set_layout);
    if (shader_module) ctx->device.destroyShaderModule(shader_module);
  };

  auto make_error = [&](vk::Result r, const char* msg) -> VectorAddResult {
    cleanup();
    return MakeError(r, msg);
  };

  vk::ShaderModuleCreateInfo shader_ci({}, spirv.size() * sizeof(uint32_t), spirv.data());
  try {
    shader_module = ctx->device.createShaderModule(shader_ci);
  } catch (const vk::SystemError&) {
    cleanup();
    return MakeError(vk::Result::eErrorInitializationFailed, "Failed to create shader module");
  }

  std::vector<vk::DescriptorSetLayoutBinding> bindings = {
      {0, vk::DescriptorType::eStorageBuffer, 1, vk::ShaderStageFlagBits::eCompute, nullptr},
      {1, vk::DescriptorType::eStorageBuffer, 1, vk::ShaderStageFlagBits::eCompute, nullptr},
      {2, vk::DescriptorType::eStorageBuffer, 1, vk::ShaderStageFlagBits::eCompute, nullptr},
  };
  try {
    descriptor_set_layout = ctx->device.createDescriptorSetLayout(
        vk::DescriptorSetLayoutCreateInfo({}, bindings));
  } catch (const vk::SystemError&) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to create descriptor set layout");
  }

  vk::PushConstantRange push_range(vk::ShaderStageFlagBits::eCompute, 0, sizeof(uint32_t));
  try {
    pipeline_layout = ctx->device.createPipelineLayout(
        vk::PipelineLayoutCreateInfo({}, 1, &descriptor_set_layout, 1, &push_range));
  } catch (const vk::SystemError&) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to create pipeline layout");
  }

  vk::PipelineShaderStageCreateInfo shader_stage({}, vk::ShaderStageFlagBits::eCompute, shader_module, "main");
  try {
    auto rv = ctx->device.createComputePipeline(
        nullptr, vk::ComputePipelineCreateInfo({}, shader_stage, pipeline_layout));
    compute_pipeline = rv.value;
  } catch (const vk::SystemError&) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to create compute pipeline");
  }

  vk::DescriptorPoolSize pool_sizes[] = {
      vk::DescriptorPoolSize(vk::DescriptorType::eStorageBuffer, 3)};
  try {
    descriptor_pool = ctx->device.createDescriptorPool(
        vk::DescriptorPoolCreateInfo({}, 1, 1, pool_sizes));
  } catch (const vk::SystemError&) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to create descriptor pool");
  }

  std::vector<vk::DescriptorSet> descriptor_sets;
  try {
    descriptor_sets = ctx->device.allocateDescriptorSets(
        vk::DescriptorSetAllocateInfo(descriptor_pool, 1, &descriptor_set_layout));
  } catch (const vk::SystemError&) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to allocate descriptor sets");
  }

  auto find_memory_type = [&](const vk::MemoryRequirements& mem_reqs) -> uint32_t {
    auto mem_props = ctx->physical_device.getMemoryProperties();
    for (uint32_t i = 0; i < mem_props.memoryTypeCount; ++i) {
      if ((mem_reqs.memoryTypeBits & (1 << i)) &&
          (mem_props.memoryTypes[i].propertyFlags &
           (vk::MemoryPropertyFlagBits::eHostVisible | vk::MemoryPropertyFlagBits::eHostCoherent))) {
        return i;
      }
    }
    return 0xFFFFFFFF;
  };

  vk::DeviceSize buf_size = static_cast<vk::DeviceSize>(n) * sizeof(float);

  try {
    buf_a = ctx->device.createBuffer(
        vk::BufferCreateInfo({}, buf_size, vk::BufferUsageFlagBits::eStorageBuffer, vk::SharingMode::eExclusive));
    buf_b = ctx->device.createBuffer(
        vk::BufferCreateInfo({}, buf_size, vk::BufferUsageFlagBits::eStorageBuffer, vk::SharingMode::eExclusive));
    buf_c = ctx->device.createBuffer(
        vk::BufferCreateInfo({}, buf_size, vk::BufferUsageFlagBits::eStorageBuffer, vk::SharingMode::eExclusive));
  } catch (const vk::SystemError&) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to create buffers");
  }

  auto alloc_and_bind = [&](vk::Buffer buf) -> vk::DeviceMemory {
    auto reqs = ctx->device.getBufferMemoryRequirements(buf);
    uint32_t type_idx = find_memory_type(reqs);
    if (type_idx == 0xFFFFFFFF) return nullptr;
    vk::DeviceMemory mem = ctx->device.allocateMemory(vk::MemoryAllocateInfo(reqs.size, type_idx));
    ctx->device.bindBufferMemory(buf, mem, 0);
    return mem;
  };

  auto upload = [&](vk::DeviceMemory mem, const void* data, size_t size) -> bool {
    void* mapped = ctx->device.mapMemory(mem, 0, size);
    if (!mapped) return false;
    std::memcpy(mapped, data, size);
    ctx->device.unmapMemory(mem);
    return true;
  };

  try {
    mem_a = alloc_and_bind(buf_a);
    mem_b = alloc_and_bind(buf_b);
    mem_c = alloc_and_bind(buf_c);
  } catch (const vk::SystemError&) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to allocate memory");
  }

  if (!mem_a || !mem_b || !mem_c || !upload(mem_a, A, buf_size) || !upload(mem_b, B, buf_size)) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to upload data");
  }

  vk::DescriptorBufferInfo buf_info_a(buf_a, 0, buf_size);
  vk::DescriptorBufferInfo buf_info_b(buf_b, 0, buf_size);
  vk::DescriptorBufferInfo buf_info_c(buf_c, 0, buf_size);

  vk::WriteDescriptorSet writes[] = {
      {descriptor_sets[0], 0, 0, 1, vk::DescriptorType::eStorageBuffer, nullptr, &buf_info_a},
      {descriptor_sets[0], 1, 0, 1, vk::DescriptorType::eStorageBuffer, nullptr, &buf_info_b},
      {descriptor_sets[0], 2, 0, 1, vk::DescriptorType::eStorageBuffer, nullptr, &buf_info_c},
  };
  ctx->device.updateDescriptorSets(3, writes, 0, nullptr);

  try {
    cmd_pool = ctx->device.createCommandPool(
        vk::CommandPoolCreateInfo({}, ctx->compute_queue_family_index));
  } catch (const vk::SystemError&) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to create command pool");
  }

  std::vector<vk::CommandBuffer> cmd_buffers;
  try {
    cmd_buffers = ctx->device.allocateCommandBuffers(
        vk::CommandBufferAllocateInfo(cmd_pool, vk::CommandBufferLevel::ePrimary, 1));
  } catch (const vk::SystemError&) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to allocate command buffers");
  }

  uint32_t n_u32 = static_cast<uint32_t>(n);
  cmd_buffers[0].begin(vk::CommandBufferBeginInfo(vk::CommandBufferUsageFlagBits::eOneTimeSubmit));
  cmd_buffers[0].bindPipeline(vk::PipelineBindPoint::eCompute, compute_pipeline);
  cmd_buffers[0].bindDescriptorSets(vk::PipelineBindPoint::eCompute, pipeline_layout, 0, 1,
                                    &descriptor_sets[0], 0, nullptr);
  cmd_buffers[0].pushConstants(pipeline_layout, vk::ShaderStageFlagBits::eCompute, 0, sizeof(uint32_t), &n_u32);
  uint32_t group_count = (static_cast<uint32_t>(n) + 63) / 64;
  cmd_buffers[0].dispatch(group_count, 1, 1);
  cmd_buffers[0].end();

  try {
    fence = ctx->device.createFence(vk::FenceCreateInfo());
  } catch (const vk::SystemError&) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to create fence");
  }

  vk::SubmitInfo submit_info(0, nullptr, nullptr, 1, &cmd_buffers[0]);
  ctx->queue.submit(submit_info, fence);

  (void)ctx->device.waitForFences(fence, true, UINT64_MAX);

  void* mapped_c = ctx->device.mapMemory(mem_c, 0, buf_size);
  if (mapped_c) {
    std::memcpy(C, mapped_c, buf_size);
    ctx->device.unmapMemory(mem_c);
  }

  cleanup();

  return VectorAddResult{vk::Result::eSuccess, nullptr};
}

}  // namespace hw_vulkan
