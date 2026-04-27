#include <vulkan/vulkan.hpp>
#include <iostream>
#include <vector>
#include <stdexcept>
#include <spdlog/spdlog.h>

const char* computeShaderCode = R"(
#version 450

layout(local_size_x = 64) in;

layout(binding = 0) buffer BufferA {
    float data_A[];
};

layout(binding = 1) buffer BufferB {
    float data_B[];
};

layout(binding = 2) buffer BufferC {
    float data_C[];
};

void main() {
    uint i = gl_GlobalInvocationID.x;
    data_C[i] = data_A[i] + data_B[i];
}
)";

int main() {
    spdlog::info("Vulkan Hello World - Vector Addition");

    try {
        vk::ApplicationInfo appInfo(
            "Hello World Vulkan",
            VK_MAKE_VERSION(1, 0, 0),
            "No Engine",
            VK_MAKE_VERSION(1, 0, 0),
            VK_API_VERSION_1_2
        );

        std::vector<const char*> extensions = {
            VK_KHR_GET_PHYSICAL_DEVICE_PROPERTIES_2_EXTENSION_NAME
        };

        vk::InstanceCreateInfo createInfo(
            {},
            &appInfo,
            0,
            nullptr,
            static_cast<uint32_t>(extensions.size()),
            extensions.data()
        );

        vk::Instance instance = vk::createInstance(createInfo);
        spdlog::info("Created Vulkan instance");

        std::vector<vk::PhysicalDevice> physicalDevices = instance.enumeratePhysicalDevices();
        if (physicalDevices.empty()) {
            spdlog::error("No Vulkan physical devices found");
            return 1;
        }

        spdlog::info("Found {} physical device(s)", physicalDevices.size());

        vk::PhysicalDevice physicalDevice;
        uint32_t computeQueueFamilyIndex = 0xFFFFFFFF;

        for (size_t i = 0; i < physicalDevices.size(); ++i) {
            vk::PhysicalDeviceProperties props = physicalDevices[i].getProperties();
            spdlog::info("Device {}: {}", i, props.deviceName);

            auto queueFamilies = physicalDevices[i].getQueueFamilyProperties();
            spdlog::info("  Found {} queue family(ies)", queueFamilies.size());

            for (uint32_t j = 0; j < queueFamilies.size(); ++j) {
                if (queueFamilies[j].queueFlags & vk::QueueFlagBits::eCompute) {
                    spdlog::info("  Queue family {}: supports compute", j);
                    if (computeQueueFamilyIndex == 0xFFFFFFFF) {
                        computeQueueFamilyIndex = j;
                        physicalDevice = physicalDevices[i];
                    }
                }
            }
        }

        if (computeQueueFamilyIndex == 0xFFFFFFFF) {
            spdlog::error("No queue family with compute support found");
            return 1;
        }

        spdlog::info("Selected device and queue family: {}", computeQueueFamilyIndex);

        float queuePriority = 1.0f;
        vk::DeviceQueueCreateInfo queueCreateInfo(
            {},
            computeQueueFamilyIndex,
            1,
            &queuePriority
        );

        [[maybe_unused]] const char* deviceExtensions[] = {
            VK_KHR_SHADER_FLOAT16_INT8_EXTENSION_NAME,
        };

        vk::PhysicalDeviceFeatures deviceFeatures{};
        vk::DeviceCreateInfo deviceCreateInfo(
            {},
            1,
            &queueCreateInfo,
            0,
            nullptr,
            0,
            nullptr,
            &deviceFeatures
        );

        vk::Device device = physicalDevice.createDevice(deviceCreateInfo);
        spdlog::info("Created logical device");

        vk::Queue queue = device.getQueue(computeQueueFamilyIndex, 0);

        const int n = 1024;
        std::vector<float> h_A(n), h_B(n), h_C(n);

        for (int i = 0; i < n; ++i) {
            h_A[i] = static_cast<float>(i);
            h_B[i] = static_cast<float>(i * 2);
        }

        vk::BufferCreateInfo bufferCreateInfoA(
            {},
            n * sizeof(float),
            vk::BufferUsageFlagBits::eStorageBuffer,
            vk::SharingMode::eExclusive
        );
        vk::Buffer bufferA = device.createBuffer(bufferCreateInfoA);

        vk::BufferCreateInfo bufferCreateInfoB(
            {},
            n * sizeof(float),
            vk::BufferUsageFlagBits::eStorageBuffer,
            vk::SharingMode::eExclusive
        );
        vk::Buffer bufferB = device.createBuffer(bufferCreateInfoB);

        vk::BufferCreateInfo bufferCreateInfoC(
            {},
            n * sizeof(float),
            vk::BufferUsageFlagBits::eStorageBuffer,
            vk::SharingMode::eExclusive
        );
        vk::Buffer bufferC = device.createBuffer(bufferCreateInfoC);
        spdlog::info("Created buffers");

        vk::MemoryRequirements memReqsA = device.getBufferMemoryRequirements(bufferA);
        vk::MemoryRequirements memReqsB = device.getBufferMemoryRequirements(bufferB);
        vk::MemoryRequirements memReqsC = device.getBufferMemoryRequirements(bufferC);

        auto memoryProperties = physicalDevice.getMemoryProperties();

        uint32_t memoryTypeIndex = 0xFFFFFFFF;
        for (uint32_t i = 0; i < memoryProperties.memoryTypeCount; ++i) {
            if ((memReqsA.memoryTypeBits & (1 << i)) &&
                (memoryProperties.memoryTypes[i].propertyFlags & vk::MemoryPropertyFlagBits::eHostVisible) &&
                (memoryProperties.memoryTypes[i].propertyFlags & vk::MemoryPropertyFlagBits::eHostCoherent)) {
                memoryTypeIndex = i;
                break;
            }
        }

        if (memoryTypeIndex == 0xFFFFFFFF) {
            spdlog::error("Could not find suitable memory type");
            return 1;
        }

        vk::MemoryAllocateInfo allocateInfoA(
            memReqsA.size,
            memoryTypeIndex
        );
        vk::DeviceMemory memoryA = device.allocateMemory(allocateInfoA);

        vk::MemoryAllocateInfo allocateInfoB(
            memReqsB.size,
            memoryTypeIndex
        );
        vk::DeviceMemory memoryB = device.allocateMemory(allocateInfoB);

        vk::MemoryAllocateInfo allocateInfoC(
            memReqsC.size,
            memoryTypeIndex
        );
        vk::DeviceMemory memoryC = device.allocateMemory(allocateInfoC);

        device.bindBufferMemory(bufferA, memoryA, 0);
        device.bindBufferMemory(bufferB, memoryB, 0);
        device.bindBufferMemory(bufferC, memoryC, 0);

        void* dataA = device.mapMemory(memoryA, 0, n * sizeof(float));
        memcpy(dataA, h_A.data(), n * sizeof(float));
        device.unmapMemory(memoryA);

        void* dataB = device.mapMemory(memoryB, 0, n * sizeof(float));
        memcpy(dataB, h_B.data(), n * sizeof(float));
        device.unmapMemory(memoryB);
        spdlog::info("Uploaded data to device buffers");

        vk::ShaderModuleCreateInfo shaderCreateInfo(
            {},
            sizeof(computeShaderCode),
            reinterpret_cast<const uint32_t*>(computeShaderCode)
        );
        vk::ShaderModule shaderModule = device.createShaderModule(shaderCreateInfo);
        spdlog::info("Created compute shader module");

        vk::DescriptorSetLayoutBinding bindings[3] = {
            vk::DescriptorSetLayoutBinding(
                0,
                vk::DescriptorType::eStorageBuffer,
                1,
                vk::ShaderStageFlagBits::eCompute,
                nullptr
            ),
            vk::DescriptorSetLayoutBinding(
                1,
                vk::DescriptorType::eStorageBuffer,
                1,
                vk::ShaderStageFlagBits::eCompute,
                nullptr
            ),
            vk::DescriptorSetLayoutBinding(
                2,
                vk::DescriptorType::eStorageBuffer,
                1,
                vk::ShaderStageFlagBits::eCompute,
                nullptr
            )
        };

        vk::DescriptorSetLayoutCreateInfo descriptorSetLayoutCreateInfo(
            {},
            3,
            bindings
        );
        vk::DescriptorSetLayout descriptorSetLayout = device.createDescriptorSetLayout(descriptorSetLayoutCreateInfo);

        vk::PipelineLayoutCreateInfo pipelineLayoutCreateInfo(
            {},
            1,
            &descriptorSetLayout
        );
        vk::PipelineLayout pipelineLayout = device.createPipelineLayout(pipelineLayoutCreateInfo);
        spdlog::info("Created pipeline layout");

        vk::DescriptorPoolSize poolSizes[] = {
            vk::DescriptorPoolSize(vk::DescriptorType::eStorageBuffer, 3)
        };

        vk::DescriptorPoolCreateInfo descriptorPoolCreateInfo(
            {},
            1,
            1,
            poolSizes
        );
        vk::DescriptorPool descriptorPool = device.createDescriptorPool(descriptorPoolCreateInfo);

        vk::DescriptorSetAllocateInfo descriptorSetAllocateInfo(
            descriptorPool,
            1,
            &descriptorSetLayout
        );
        std::vector<vk::DescriptorSet> descriptorSets = device.allocateDescriptorSets(descriptorSetAllocateInfo);

        vk::DescriptorBufferInfo bufferInfoA(bufferA, 0, n * sizeof(float));
        vk::DescriptorBufferInfo bufferInfoB(bufferB, 0, n * sizeof(float));
        vk::DescriptorBufferInfo bufferInfoC(bufferC, 0, n * sizeof(float));

        vk::WriteDescriptorSet writeDescriptorSets[3] = {
            vk::WriteDescriptorSet(
                descriptorSets[0],
                0,
                0,
                1,
                vk::DescriptorType::eStorageBuffer,
                nullptr,
                &bufferInfoA,
                nullptr
            ),
            vk::WriteDescriptorSet(
                descriptorSets[0],
                1,
                0,
                1,
                vk::DescriptorType::eStorageBuffer,
                nullptr,
                &bufferInfoB,
                nullptr
            ),
            vk::WriteDescriptorSet(
                descriptorSets[0],
                2,
                0,
                1,
                vk::DescriptorType::eStorageBuffer,
                nullptr,
                &bufferInfoC,
                nullptr
            )
        };

        device.updateDescriptorSets(3, writeDescriptorSets, 0, nullptr);
        spdlog::info("Configured descriptor sets");

        vk::PipelineShaderStageCreateInfo shaderStageCreateInfo(
            {},
            vk::ShaderStageFlagBits::eCompute,
            shaderModule,
            "main",
            nullptr
        );

        vk::ComputePipelineCreateInfo computePipelineCreateInfo(
            {},
            shaderStageCreateInfo,
            pipelineLayout,
            nullptr,
            0
        );

        vk::ResultValue<vk::Pipeline> pipelineResult = device.createComputePipeline(nullptr, computePipelineCreateInfo);
        if (pipelineResult.result != vk::Result::eSuccess) {
            spdlog::error("Failed to create compute pipeline: {}", vk::to_string(pipelineResult.result));
            throw std::runtime_error("Failed to create compute pipeline");
        }
        vk::Pipeline computePipeline = pipelineResult.value;
        spdlog::info("Created compute pipeline");

        vk::CommandPoolCreateInfo commandPoolCreateInfo(
            {},
            computeQueueFamilyIndex
        );
        vk::CommandPool commandPool = device.createCommandPool(commandPoolCreateInfo);

        vk::CommandBufferAllocateInfo commandBufferAllocateInfo(
            commandPool,
            vk::CommandBufferLevel::ePrimary,
            1
        );
        std::vector<vk::CommandBuffer> commandBuffers = device.allocateCommandBuffers(commandBufferAllocateInfo);

        vk::CommandBufferBeginInfo commandBufferBeginInfo(
            vk::CommandBufferUsageFlagBits::eOneTimeSubmit
        );

        commandBuffers[0].begin(commandBufferBeginInfo);
        commandBuffers[0].bindPipeline(vk::PipelineBindPoint::eCompute, computePipeline);
        commandBuffers[0].bindDescriptorSets(vk::PipelineBindPoint::eCompute, pipelineLayout, 0, 1, &descriptorSets[0], 0, nullptr);
        commandBuffers[0].dispatch(n, 1, 1);
        commandBuffers[0].end();
        spdlog::info("Recorded command buffer");

        vk::SubmitInfo submitInfo(
            0,
            nullptr,
            nullptr,
            1,
            &commandBuffers[0]
        );

        vk::Fence fence = device.createFence(vk::FenceCreateInfo());
        queue.submit(submitInfo, fence);
        spdlog::info("Submitted command buffer to queue");

        [[maybe_unused]] vk::Result waitResult = device.waitForFences(fence, true, UINT64_MAX);
        spdlog::info("Kernel execution completed");

        void* dataC = device.mapMemory(memoryC, 0, n * sizeof(float));
        memcpy(h_C.data(), dataC, n * sizeof(float));
        device.unmapMemory(memoryC);
        spdlog::info("Read results back to host");

        bool correct = true;
        for (int i = 0; i < n; ++i) {
            float expected = h_A[i] + h_B[i];
            if (std::abs(h_C[i] - expected) > 1e-5) {
                spdlog::error("Mismatch at index {}: expected {}, got {}", i, expected, h_C[i]);
                correct = false;
                break;
            }
        }

        if (correct) {
            spdlog::info("Vector addition verification: PASSED");
        } else {
            spdlog::error("Vector addition verification: FAILED");
        }

        device.destroyFence(fence);
        device.freeCommandBuffers(commandPool, commandBuffers);
        device.destroyCommandPool(commandPool);
        device.destroyPipeline(computePipeline);
        device.destroyDescriptorPool(descriptorPool);
        device.destroyPipelineLayout(pipelineLayout);
        device.destroyDescriptorSetLayout(descriptorSetLayout);
        device.destroyShaderModule(shaderModule);
        device.freeMemory(memoryC);
        device.freeMemory(memoryB);
        device.freeMemory(memoryA);
        device.destroyBuffer(bufferC);
        device.destroyBuffer(bufferB);
        device.destroyBuffer(bufferA);
        instance.destroy();

        spdlog::info("Vulkan resources cleaned up successfully");

        return correct ? 0 : 1;

    } catch (const vk::SystemError& err) {
        spdlog::error("Vulkan error: {}", err.what());
        return 1;
    } catch (const std::exception& err) {
        spdlog::error("Error: {}", err.what());
        return 1;
    }
}
