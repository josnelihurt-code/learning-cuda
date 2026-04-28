#include "src/cpp_accelerator/cmd/hello-world-vulkan/vulkan_program.h"

#include "src/cpp_accelerator/cmd/hello-world-vulkan/vulkan_runtime.h"

namespace hw_vulkan {

VulkanProgram::VulkanProgram()
    : last_error_code_(0), last_error_message_("ok") {}

VulkanProgram::~VulkanProgram() { Cleanup(); }

bool VulkanProgram::InitializeFromEmbedded(const VulkanRuntime& runtime, const void* spirv,
                                           size_t size_bytes) {
  Cleanup();

  if (!runtime.Device()) {
    return SetLastError(vk::Result::eErrorInitializationFailed,
                        "VulkanRuntime is not initialized");
  }

  if (spirv == nullptr || size_bytes == 0 || size_bytes % sizeof(uint32_t) != 0) {
    return SetLastError(vk::Result::eErrorInitializationFailed, "Invalid embedded SPIR-V");
  }

  device_ = runtime.Device();

  vk::ShaderModuleCreateInfo ci({}, size_bytes,
                                reinterpret_cast<const uint32_t*>(spirv));
  try {
    shader_module_ = device_.createShaderModule(ci);
  } catch (const vk::SystemError&) {
    return SetLastError(vk::Result::eErrorInitializationFailed,
                        "Failed to create shader module");
  }

  return SetLastError(vk::Result::eSuccess, "ok");
}

void VulkanProgram::Cleanup() {
  if (device_ && shader_module_) {
    device_.destroyShaderModule(shader_module_);
    shader_module_ = nullptr;
  }
  device_ = nullptr;
}

vk::ShaderModule VulkanProgram::Handle() const { return shader_module_; }
int VulkanProgram::LastErrorCode() const { return last_error_code_; }
const char* VulkanProgram::LastErrorMessage() const { return last_error_message_; }

bool VulkanProgram::SetLastError(vk::Result code, const char* message) {
  last_error_code_ = static_cast<int>(code);
  last_error_message_ = message;
  return code == vk::Result::eSuccess;
}

}  // namespace hw_vulkan
