#include "src/cpp_accelerator/cmd/hello-world-vulkan/vector_add.h"

#include <array>
#include <cstring>
#include <vector>

#include "src/cpp_accelerator/cmd/hello-world-vulkan/vector_add_kernel_blob.h"

namespace hw_vulkan {

VulkanVectorAddProgram::VulkanVectorAddProgram()
    : last_error_code_(0), last_error_message_("ok") {}

VulkanVectorAddProgram::~VulkanVectorAddProgram() {}

bool VulkanVectorAddProgram::Initialize() {
  if (!runtime_.Initialize()) {
    return SetLastError(static_cast<vk::Result>(runtime_.LastErrorCode()),
                        runtime_.LastErrorMessage());
  }

  const void* spirv = vector_add_kernel_blob::spirv();
  size_t spirv_size = vector_add_kernel_blob::spirv_size_bytes();

  if (!program_.InitializeFromEmbedded(runtime_, spirv, spirv_size)) {
    return SetLastError(static_cast<vk::Result>(program_.LastErrorCode()),
                        program_.LastErrorMessage());
  }

  return SetLastError(vk::Result::eSuccess, "ok");
}

VectorAddResult VulkanVectorAddProgram::Execute(const float* a, const float* b, float* c, int n) {
  if (!runtime_.Device()) {
    return MakeError(vk::Result::eErrorInitializationFailed, "Runtime not initialized");
  }
  if (!program_.Handle()) {
    return MakeError(vk::Result::eErrorInitializationFailed, "Program not initialized");
  }

  vk::Device device = runtime_.Device();

  vk::DescriptorSetLayout descriptor_set_layout = nullptr;
  vk::PipelineLayout pipeline_layout = nullptr;
  vk::Pipeline compute_pipeline = nullptr;
  vk::DescriptorPool descriptor_pool = nullptr;
  vk::CommandPool cmd_pool = nullptr;
  vk::Fence fence = nullptr;
  vk::Buffer buf_a = nullptr;
  vk::Buffer buf_b = nullptr;
  vk::Buffer buf_c = nullptr;
  vk::DeviceMemory mem_a = nullptr;
  vk::DeviceMemory mem_b = nullptr;
  vk::DeviceMemory mem_c = nullptr;

  auto cleanup = [&]() {
    if (fence) {
      device.destroyFence(fence);
    }
    if (cmd_pool) {
      device.destroyCommandPool(cmd_pool);
    }
    if (mem_c) {
      device.freeMemory(mem_c);
    }
    if (mem_b) {
      device.freeMemory(mem_b);
    }
    if (mem_a) {
      device.freeMemory(mem_a);
    }
    if (buf_c) {
      device.destroyBuffer(buf_c);
    }
    if (buf_b) {
      device.destroyBuffer(buf_b);
    }
    if (buf_a) {
      device.destroyBuffer(buf_a);
    }
    if (descriptor_pool) {
      device.destroyDescriptorPool(descriptor_pool);
    }
    if (compute_pipeline) {
      device.destroyPipeline(compute_pipeline);
    }
    if (pipeline_layout) {
      device.destroyPipelineLayout(pipeline_layout);
    }
    if (descriptor_set_layout) {
      device.destroyDescriptorSetLayout(descriptor_set_layout);
    }
  };

  auto make_error = [&](vk::Result r, const char* msg) -> VectorAddResult {
    cleanup();
    return MakeError(r, msg);
  };

  std::vector<vk::DescriptorSetLayoutBinding> bindings = {
      {0, vk::DescriptorType::eStorageBuffer, 1, vk::ShaderStageFlagBits::eCompute, nullptr},
      {1, vk::DescriptorType::eStorageBuffer, 1, vk::ShaderStageFlagBits::eCompute, nullptr},
      {2, vk::DescriptorType::eStorageBuffer, 1, vk::ShaderStageFlagBits::eCompute, nullptr},
  };
  try {
    descriptor_set_layout =
        device.createDescriptorSetLayout(vk::DescriptorSetLayoutCreateInfo({}, bindings));
  } catch (const vk::SystemError&) {
    return make_error(vk::Result::eErrorInitializationFailed,
                      "Failed to create descriptor set layout");
  }

  vk::PushConstantRange push_range(vk::ShaderStageFlagBits::eCompute, 0, sizeof(uint32_t));
  try {
    pipeline_layout = device.createPipelineLayout(
        vk::PipelineLayoutCreateInfo({}, 1, &descriptor_set_layout, 1, &push_range));
  } catch (const vk::SystemError&) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to create pipeline layout");
  }

  vk::PipelineShaderStageCreateInfo shader_stage({}, vk::ShaderStageFlagBits::eCompute,
                                                 program_.Handle(), "main");
  try {
    auto rv = device.createComputePipeline(
        nullptr, vk::ComputePipelineCreateInfo({}, shader_stage, pipeline_layout));
    compute_pipeline = rv.value;
  } catch (const vk::SystemError&) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to create compute pipeline");
  }

  std::array<vk::DescriptorPoolSize, 1> pool_sizes = {
      vk::DescriptorPoolSize(vk::DescriptorType::eStorageBuffer, 3)};
  try {
    descriptor_pool = device.createDescriptorPool(
        vk::DescriptorPoolCreateInfo({}, 1, pool_sizes.size(), pool_sizes.data()));
  } catch (const vk::SystemError&) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to create descriptor pool");
  }

  std::vector<vk::DescriptorSet> descriptor_sets;
  try {
    descriptor_sets = device.allocateDescriptorSets(
        vk::DescriptorSetAllocateInfo(descriptor_pool, 1, &descriptor_set_layout));
  } catch (const vk::SystemError&) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to allocate descriptor sets");
  }

  auto find_memory_type = [&](const vk::MemoryRequirements& mem_reqs) -> uint32_t {
    auto mem_props = runtime_.PhysicalDevice().getMemoryProperties();
    for (uint32_t i = 0; i < mem_props.memoryTypeCount; ++i) {
      if (((mem_reqs.memoryTypeBits & (1U << i)) != 0U) &&
          ((mem_props.memoryTypes.at(i).propertyFlags &
            (vk::MemoryPropertyFlagBits::eHostVisible |
             vk::MemoryPropertyFlagBits::eHostCoherent)) != vk::MemoryPropertyFlags{})) {
        return i;
      }
    }
    return 0xFFFFFFFF;
  };

  vk::DeviceSize buf_size = static_cast<vk::DeviceSize>(n) * sizeof(float);

  try {
    buf_a = device.createBuffer(vk::BufferCreateInfo(
        {}, buf_size, vk::BufferUsageFlagBits::eStorageBuffer, vk::SharingMode::eExclusive));
    buf_b = device.createBuffer(vk::BufferCreateInfo(
        {}, buf_size, vk::BufferUsageFlagBits::eStorageBuffer, vk::SharingMode::eExclusive));
    buf_c = device.createBuffer(vk::BufferCreateInfo(
        {}, buf_size, vk::BufferUsageFlagBits::eStorageBuffer, vk::SharingMode::eExclusive));
  } catch (const vk::SystemError&) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to create buffers");
  }

  auto alloc_and_bind = [&](vk::Buffer buf) -> vk::DeviceMemory {
    auto reqs = device.getBufferMemoryRequirements(buf);
    uint32_t type_idx = find_memory_type(reqs);
    if (type_idx == 0xFFFFFFFF) {
      return nullptr;
    }
    vk::DeviceMemory mem = device.allocateMemory(vk::MemoryAllocateInfo(reqs.size, type_idx));
    device.bindBufferMemory(buf, mem, 0);
    return mem;
  };

  auto upload = [&](vk::DeviceMemory mem, const void* data, size_t size) -> bool {
    void* mapped = device.mapMemory(mem, 0, size);
    if (mapped == nullptr) {
      return false;
    }
    std::memcpy(mapped, data, size);
    device.unmapMemory(mem);
    return true;
  };

  try {
    mem_a = alloc_and_bind(buf_a);
    mem_b = alloc_and_bind(buf_b);
    mem_c = alloc_and_bind(buf_c);
  } catch (const vk::SystemError&) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to allocate memory");
  }

  if ((mem_a == nullptr) || (mem_b == nullptr) || (mem_c == nullptr) ||
      !upload(mem_a, a, buf_size) || !upload(mem_b, b, buf_size)) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to upload data");
  }

  vk::DescriptorBufferInfo buf_info_a(buf_a, 0, buf_size);
  vk::DescriptorBufferInfo buf_info_b(buf_b, 0, buf_size);
  vk::DescriptorBufferInfo buf_info_c(buf_c, 0, buf_size);

  std::array<vk::WriteDescriptorSet, 3> writes = {
      vk::WriteDescriptorSet{descriptor_sets[0], 0, 0, 1, vk::DescriptorType::eStorageBuffer,
                             nullptr, &buf_info_a},
      vk::WriteDescriptorSet{descriptor_sets[0], 1, 0, 1, vk::DescriptorType::eStorageBuffer,
                             nullptr, &buf_info_b},
      vk::WriteDescriptorSet{descriptor_sets[0], 2, 0, 1, vk::DescriptorType::eStorageBuffer,
                             nullptr, &buf_info_c},
  };
  device.updateDescriptorSets(writes.size(), writes.data(), 0, nullptr);

  try {
    cmd_pool = device.createCommandPool(
        vk::CommandPoolCreateInfo({}, runtime_.ComputeQueueFamilyIndex()));
  } catch (const vk::SystemError&) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to create command pool");
  }

  std::vector<vk::CommandBuffer> cmd_buffers;
  try {
    cmd_buffers = device.allocateCommandBuffers(
        vk::CommandBufferAllocateInfo(cmd_pool, vk::CommandBufferLevel::ePrimary, 1));
  } catch (const vk::SystemError&) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to allocate command buffers");
  }

  uint32_t n_u32 = static_cast<uint32_t>(n);
  cmd_buffers[0].begin(vk::CommandBufferBeginInfo(vk::CommandBufferUsageFlagBits::eOneTimeSubmit));
  cmd_buffers[0].bindPipeline(vk::PipelineBindPoint::eCompute, compute_pipeline);
  cmd_buffers[0].bindDescriptorSets(vk::PipelineBindPoint::eCompute, pipeline_layout, 0, 1,
                                    descriptor_sets.data(), 0, nullptr);
  cmd_buffers[0].pushConstants(pipeline_layout, vk::ShaderStageFlagBits::eCompute, 0,
                               sizeof(uint32_t), &n_u32);
  uint32_t group_count = (static_cast<uint32_t>(n) + 63U) / 64U;
  cmd_buffers[0].dispatch(group_count, 1, 1);
  cmd_buffers[0].end();

  try {
    fence = device.createFence(vk::FenceCreateInfo());
  } catch (const vk::SystemError&) {
    return make_error(vk::Result::eErrorInitializationFailed, "Failed to create fence");
  }

  vk::SubmitInfo submit_info(0, nullptr, nullptr, 1, cmd_buffers.data());
  runtime_.Queue().submit(submit_info, fence);
  (void)device.waitForFences(fence, vk::True, UINT64_MAX);

  void* mapped_c = device.mapMemory(mem_c, 0, buf_size);
  if (mapped_c != nullptr) {
    std::memcpy(c, mapped_c, buf_size);
    device.unmapMemory(mem_c);
  }

  cleanup();
  return VectorAddResult{0, "ok"};
}

int VulkanVectorAddProgram::LastErrorCode() const { return last_error_code_; }
const char* VulkanVectorAddProgram::LastErrorMessage() const { return last_error_message_; }

VectorAddResult VulkanVectorAddProgram::MakeError(vk::Result code, const char* message) {
  SetLastError(code, message);
  return VectorAddResult{static_cast<int>(code), message};
}

bool VulkanVectorAddProgram::SetLastError(vk::Result code, const char* message) {
  last_error_code_ = static_cast<int>(code);
  last_error_message_ = message;
  return code == vk::Result::eSuccess;
}

}  // namespace hw_vulkan
