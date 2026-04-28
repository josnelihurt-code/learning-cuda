#include "src/cpp_accelerator/cmd/hello-world-vulkan/vulkan_runtime.h"

#include <vector>

namespace hw_vulkan {

VulkanRuntime::VulkanRuntime()
    : compute_queue_family_index_(0xFFFFFFFF),
      last_error_code_(0),
      last_error_message_("ok") {}

VulkanRuntime::~VulkanRuntime() { Cleanup(); }

bool VulkanRuntime::Initialize() {
  Cleanup();

  vk::ApplicationInfo app_info("Hello World Vulkan", VK_MAKE_VERSION(1, 0, 0), "No Engine",
                               VK_MAKE_VERSION(1, 0, 0), VK_API_VERSION_1_2);

  std::vector<const char*> extensions = {
      VK_KHR_GET_PHYSICAL_DEVICE_PROPERTIES_2_EXTENSION_NAME,
  };

  vk::InstanceCreateInfo instance_ci({}, &app_info, 0, nullptr,
                                     static_cast<uint32_t>(extensions.size()), extensions.data());
  try {
    instance_ = vk::createInstance(instance_ci);
  } catch (const vk::SystemError&) {
    return SetLastError(vk::Result::eErrorInitializationFailed, "vk::createInstance failed");
  }

  std::vector<vk::PhysicalDevice> physical_devices;
  try {
    physical_devices = instance_.enumeratePhysicalDevices();
  } catch (const vk::SystemError&) {
    return SetLastError(vk::Result::eErrorInitializationFailed,
                        "enumeratePhysicalDevices failed");
  }

  if (physical_devices.empty()) {
    return SetLastError(vk::Result::eErrorInitializationFailed,
                        "No Vulkan physical devices found");
  }

  compute_queue_family_index_ = 0xFFFFFFFF;
  for (size_t i = 0; i < physical_devices.size(); ++i) {
    auto queue_families = physical_devices[i].getQueueFamilyProperties();
    for (uint32_t j = 0; j < queue_families.size(); ++j) {
      if (queue_families[j].queueFlags & vk::QueueFlagBits::eCompute) {
        if (compute_queue_family_index_ == 0xFFFFFFFF) {
          compute_queue_family_index_ = j;
          physical_device_ = physical_devices[i];
        }
      }
    }
  }

  if (compute_queue_family_index_ == 0xFFFFFFFF) {
    return SetLastError(vk::Result::eErrorInitializationFailed,
                        "No queue family with compute support found");
  }

  float queue_priority = 1.0F;
  vk::DeviceQueueCreateInfo queue_ci({}, compute_queue_family_index_, 1, &queue_priority);
  vk::PhysicalDeviceFeatures device_features{};
  vk::DeviceCreateInfo device_ci({}, 1, &queue_ci, 0, nullptr, 0, nullptr, &device_features);

  try {
    device_ = physical_device_.createDevice(device_ci);
  } catch (const vk::SystemError&) {
    return SetLastError(vk::Result::eErrorInitializationFailed,
                        "Failed to create logical device");
  }

  queue_ = device_.getQueue(compute_queue_family_index_, 0);
  return SetLastError(vk::Result::eSuccess, "ok");
}

void VulkanRuntime::Cleanup() {
  if (device_) {
    device_.destroy();
    device_ = nullptr;
  }
  if (instance_) {
    instance_.destroy();
    instance_ = nullptr;
  }
  physical_device_ = nullptr;
  queue_ = nullptr;
  compute_queue_family_index_ = 0xFFFFFFFF;
}

vk::Device VulkanRuntime::Device() const { return device_; }
vk::PhysicalDevice VulkanRuntime::PhysicalDevice() const { return physical_device_; }
vk::Queue VulkanRuntime::Queue() const { return queue_; }
uint32_t VulkanRuntime::ComputeQueueFamilyIndex() const { return compute_queue_family_index_; }
int VulkanRuntime::LastErrorCode() const { return last_error_code_; }
const char* VulkanRuntime::LastErrorMessage() const { return last_error_message_; }

bool VulkanRuntime::SetLastError(vk::Result code, const char* message) {
  last_error_code_ = static_cast<int>(code);
  last_error_message_ = message;
  return code == vk::Result::eSuccess;
}

}  // namespace hw_vulkan
