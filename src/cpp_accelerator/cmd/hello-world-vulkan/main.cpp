#include <vulkan/vulkan.hpp>

#include <cmath>
#include <iostream>
#include <vector>

#include "src/cpp_accelerator/cmd/hello-world-vulkan/vector_add.h"

int main() {
  try {
    vk::ApplicationInfo appInfo("Hello World Vulkan", VK_MAKE_VERSION(1, 0, 0), "No Engine",
                                VK_MAKE_VERSION(1, 0, 0), VK_API_VERSION_1_2);

    std::vector<const char*> extensions = {
        VK_KHR_GET_PHYSICAL_DEVICE_PROPERTIES_2_EXTENSION_NAME,
    };

    vk::InstanceCreateInfo createInfo({}, &appInfo, 0, nullptr,
                                      static_cast<uint32_t>(extensions.size()), extensions.data());

    vk::Instance instance = vk::createInstance(createInfo);

    std::vector<vk::PhysicalDevice> physicalDevices = instance.enumeratePhysicalDevices();
    if (physicalDevices.empty()) {
      std::cerr << "No Vulkan physical devices found\n";
      return 1;
    }

    vk::PhysicalDevice physicalDevice;
    uint32_t computeQueueFamilyIndex = 0xFFFFFFFF;

    for (size_t i = 0; i < physicalDevices.size(); ++i) {
      auto queueFamilies = physicalDevices[i].getQueueFamilyProperties();
      for (uint32_t j = 0; j < queueFamilies.size(); ++j) {
        if (queueFamilies[j].queueFlags & vk::QueueFlagBits::eCompute) {
          if (computeQueueFamilyIndex == 0xFFFFFFFF) {
            computeQueueFamilyIndex = j;
            physicalDevice = physicalDevices[i];
          }
        }
      }
    }

    if (computeQueueFamilyIndex == 0xFFFFFFFF) {
      std::cerr << "No queue family with compute support found\n";
      instance.destroy();
      return 1;
    }

    float queuePriority = 1.0f;
    vk::DeviceQueueCreateInfo queueCreateInfo({}, computeQueueFamilyIndex, 1, &queuePriority);

    vk::PhysicalDeviceFeatures deviceFeatures{};
    vk::DeviceCreateInfo deviceCreateInfo({}, 1, &queueCreateInfo, 0, nullptr, 0, nullptr,
                                          &deviceFeatures);

    vk::Device device = physicalDevice.createDevice(deviceCreateInfo);
    vk::Queue queue = device.getQueue(computeQueueFamilyIndex, 0);

    const int n = 1024;
    std::vector<float> h_A(n), h_B(n), h_C(n);

    for (int i = 0; i < n; ++i) {
      h_A[i] = static_cast<float>(i);
      h_B[i] = static_cast<float>(i * 2);
    }

    hw_vulkan::VulkanComputeContext vk_ctx{device, physicalDevice, computeQueueFamilyIndex, queue};

    auto kernel_result = hw_vulkan::vector_add(h_A.data(), h_B.data(), h_C.data(), n, &vk_ctx);

    if (kernel_result.result != vk::Result::eSuccess) {
      std::cerr << "vector_add failed: "
                << (kernel_result.error_message ? kernel_result.error_message : "unknown") << " ("
                << vk::to_string(kernel_result.result) << ")\n";
    }

    bool correct = true;
    if (kernel_result.result == vk::Result::eSuccess) {
      for (int i = 0; i < n; ++i) {
        float expected = h_A[i] + h_B[i];
        if (std::abs(h_C[i] - expected) > 1e-5f) {
          std::cerr << "Mismatch at index " << i << ": expected " << expected << ", got " << h_C[i]
                    << "\n";
          correct = false;
          break;
        }
      }
    }

    device.destroy();
    instance.destroy();

    if (kernel_result.result == vk::Result::eSuccess && correct) {
      std::cout << "Vulkan hello world OK (SPIR-V embedded in binary, n=" << n << ")\n";
      return 0;
    }
    return 1;

  } catch (const vk::SystemError& err) {
    std::cerr << "Vulkan error: " << err.what() << "\n";
    return 1;
  } catch (const std::exception& err) {
    std::cerr << "Error: " << err.what() << "\n";
    return 1;
  }
}
