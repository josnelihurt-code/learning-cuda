# Vulkan compute hello world

Minimal compute-shader example: add two float vectors on the GPU with Vulkan and Vulkan-Hpp.
This sample is for learning, not production architecture.

## What is Vulkan?

Vulkan is a low-level graphics and compute API created by the Khronos Group,
first released in 2016 as the successor to OpenGL. While Vulkan is best known
for rendering, it has a full compute pipeline that lets you run arbitrary
parallel workloads on the GPU — similar to CUDA or OpenCL, but with explicit
control over every aspect of GPU operation.

Key versions:

| Version | Year | Highlights |
|---------|------|------------|
| 1.0 | 2016 | Initial release, compute + graphics |
| 1.1 | 2018 | Subgroup operations, protected memory |
| 1.2 | 2020 | Timeline semaphores, buffer device address |
| 1.3 | 2022 | Dynamic rendering, inline uniform blocks |

This example uses the C++ Vulkan-Hpp wrapper (`<vulkan/vulkan.hpp>`, `vk::`
namespace) which provides RAII-friendly types and throws `vk::SystemError`
exceptions on failure, reducing boilerplate compared to the C API.

## Core Concepts You Need to Know

### Instance and Physical Devices

- **`vk::Instance`** — The connection between your application and the Vulkan
  library. Created first, it loads the Vulkan driver. You specify enabled
  layers and extensions at instance creation.
- **`vk::PhysicalDevice`** — Represents a physical GPU (or software renderer)
  on the system. Enumerated from the instance with `enumeratePhysicalDevices()`.
  Each physical device has properties (name, API version) and queue families.

### Logical Devices and Queues

- **`vk::Device`** — A logical device created from a physical device. This is
  your primary interface for creating GPU resources (buffers, pipelines, etc.)
  and submitting work.
- **Queue families** — Each physical device exposes one or more queue families.
  Each family supports a set of operations: compute, graphics, transfer, etc.
  You query family capabilities with `getQueueFamilyProperties()` and look for
  the `eCompute` flag.
- **`vk::Queue`** — Obtained from the logical device. You submit command buffers
  to a queue for execution. A compute queue can run compute shaders.

### Command Buffers and Command Pools

- **`vk::CommandPool`** — Allocates command buffer memory. Created per queue
  family. Commands pools are not thread-safe unless you use the
  `eResetCommandBuffer` flag.
- **`vk::CommandBuffer`** — Records GPU commands (bind pipeline, bind
  descriptors, dispatch, etc.). Recorded once, then submitted to a queue.
  This example uses `eOneTimeSubmit` since the command buffer is used only
  once per execute call.

### Pipelines and Shader Modules

- **`vk::ShaderModule`** — Wraps compiled SPIR-V bytecode. Created from a
  `.spv` file (or embedded bytes). The module contains one or more entry
  points (functions).
- **`vk::ComputePipeline`** — A pipeline object that binds a shader module
  entry point to a pipeline layout. For compute, there is a single shader
  stage — no vertex/fragment stages like in graphics.
- **`vk::PipelineLayout`** — Defines the interface between the shader and
  the host: descriptor set layouts (for buffer bindings) and push constant
  ranges (for small values passed directly in the command buffer).

### Descriptors

Descriptors are how the shader accesses buffer and image resources:

- **`vk::DescriptorSetLayout`** — Declares what resources the shader expects
  (binding number, type, shader stage). Think of it as a "schema."
- **`vk::DescriptorPool`** — Allocates descriptor sets. You specify how many
  descriptors of each type the pool can hold.
- **`vk::DescriptorSet`** — A concrete binding of descriptors matching a
  layout. You write buffer handles into descriptor sets, then bind the set
  during command buffer recording.

### Memory Management

Vulkan exposes fine-grained control over GPU memory:

- **`vk::Buffer`** — A handle representing a region of GPU-accessible memory
  with a specific usage (storage buffer, uniform buffer, etc.).
- **`vk::DeviceMemory`** — Actual backing memory allocated from the GPU.
  Buffers do not have memory until you explicitly allocate and bind it.
- **Memory types** — Each physical device has multiple memory types with
  different properties:
  - `eHostVisible` — CPU can map and access this memory.
  - `eHostCoherent` — No need to flush/invalidate caches between CPU and GPU.
  - `eDeviceLocal` — GPU-local memory, fastest for GPU access.

The host uploads data by calling `mapMemory`, copying bytes with `memcpy`,
then `unmapMemory`. Reading back works the same way.

### Synchronization

- **`vk::Fence`** — A host-GPU synchronization primitive. The host submits a
  command buffer with a fence, then calls `waitForFences` to block until the
  GPU finishes. This example uses a fence to wait for kernel completion before
  reading back results.

### SPIR-V and GLSL

Vulkan shaders are distributed as SPIR-V bytecode, not GLSL source. The typical
workflow:

1. Write GLSL (`.comp` for compute shaders).
2. Compile to SPIR-V with `glslc` (part of the Vulkan SDK).
3. Embed the `.spv` bytes into the binary at build time.

GLSL compute shader structure:

```glsl
#version 450
layout(local_size_x = 64) in;
// ... buffer bindings and push constants ...
void main() {
    uint i = gl_GlobalInvocationID.x;
    // ... compute ...
}
```

- `#version 450` — GLSL version (SPIR-V 1.0).
- `local_size_x = 64` — Each work-group has 64 invocations (threads).
- `gl_GlobalInvocationID.x` — Global thread index, equivalent to CUDA's
  `threadIdx.x + blockIdx.x * blockDim.x`.

## Hardware Considerations

Vulkan requires a relatively modern GPU:

| Vendor | Minimum | Notes |
|--------|---------|-------|
| NVIDIA | Kepler (GTX 600 series) | Full Vulkan 1.3 support on Turing+ |
| AMD | GCN (Radeon HD 7700+) | Full support on RDNA+ |
| Intel | Haswell (HD 4400+) | Full support on Xe GPUs |
| ARM | Mali Midgard+ | Common on Android devices |

On Linux, Mesa provides open-source Vulkan drivers for AMD (RADV), Intel
(ANV), and others. Verify your setup with:

```bash
vulkaninfo       # lists physical devices and capabilities
```

## Vulkan Compute vs CUDA — A Comparison

| Dimension | Vulkan Compute | CUDA |
|-----------|----------------|------|
| Vendor lock-in | None — runs on NVIDIA, AMD, Intel, ARM, Qualcomm | NVIDIA GPUs only |
| Boilerplate | Very high — descriptors, pipelines, memory allocation all explicit | Low — `cudaMalloc`, kernel launch with `<<<>>>` syntax |
| Explicitness | Everything is explicit: memory types, barriers, pipeline creation | Implicit context management, automatic memory transfers |
| Memory control | Fine-grained — choose memory type per allocation | Abstracted — `cudaMalloc` picks the right type |
| Kernel language | GLSL/HLSL → SPIR-V | CUDA C++ (`.cu`, `nvcc`) |
| Ecosystem | Primarily graphics-oriented; compute ecosystem growing | Massive — cuBLAS, cuDNN, TensorRT, Thrust, Nsight |
| Debugging | RenderDoc, Vulkan validation layers | NVIDIA Nsight, cuda-memcheck |
| Portability | Runs on any Vulkan-capable GPU, including mobile | NVIDIA only |
| Performance | Portable but verbose; may leave performance on the table without tuning | Best on NVIDIA hardware, heavily optimized toolchain |

**When to choose Vulkan Compute**: You need cross-vendor GPU compute, your
project already uses Vulkan for rendering, or you're targeting mobile/embedded
GPUs.

**When to choose CUDA**: You are targeting NVIDIA GPUs, want the best
performance with the least boilerplate, and need access to the CUDA library
ecosystem.

## How This Program Works (Step by Step)

This program adds two float vectors `A` and `B` to produce `C`, where
`C[i] = A[i] + B[i]`. It uses five source files:

| File | Role |
|------|------|
| `main.cpp` | Host entry point — sets up data, runs kernel, validates results |
| `vulkan_runtime.cpp` | Instance → physical device → logical device → queue setup |
| `vulkan_program.cpp` | Loads embedded SPIR-V into a shader module |
| `vector_add.cpp` | Full compute pipeline — descriptors, pipeline, buffers, dispatch |
| `vector_add_kernel.comp` | The GLSL compute shader |

### Step 1: Runtime initialization (`vulkan_runtime.cpp`)

`VulkanRuntime::Initialize()` creates the Vulkan connection and device:

```cpp
instance_ = vk::createInstance(instance_ci);
auto physical_devices = instance_.enumeratePhysicalDevices();
```

Then it searches for a queue family that supports compute:

```cpp
for (uint32_t j = 0; j < queue_families.size(); ++j) {
    if (queue_families[j].queueFlags & vk::QueueFlagBits::eCompute) {
        if (compute_queue_family_index_ == 0xFFFFFFFF) {   // first match wins
            compute_queue_family_index_ = j;
            physical_device_ = physical_devices[i];
        }
    }
}
```

Finally it creates the logical device and retrieves the queue:

```cpp
device_ = physical_device_.createDevice(device_ci);
queue_ = device_.getQueue(compute_queue_family_index_, 0);
```

### Step 2: Shader module loading (`vulkan_program.cpp`)

`VulkanProgram::InitializeFromEmbedded()` wraps the embedded SPIR-V bytes
into a shader module:

```cpp
vk::ShaderModuleCreateInfo ci({}, size_bytes,
                              reinterpret_cast<const uint32_t*>(spirv));
shader_module_ = device_.createShaderModule(ci);
```

### Step 3: Execute the vector add (`vector_add.cpp`)

`VulkanVectorAddProgram::Execute()` is the most complex part. It creates all
Vulkan objects needed for a single compute dispatch, then cleans them up.

**3a. Descriptor set layout:**

Defines three storage buffer bindings (A, B, C) visible to the compute stage:

```cpp
std::vector<vk::DescriptorSetLayoutBinding> bindings = {
    {0, vk::DescriptorType::eStorageBuffer, 1, vk::ShaderStageFlagBits::eCompute, nullptr},
    {1, vk::DescriptorType::eStorageBuffer, 1, vk::ShaderStageFlagBits::eCompute, nullptr},
    {2, vk::DescriptorType::eStorageBuffer, 1, vk::ShaderStageFlagBits::eCompute, nullptr},
};
descriptor_set_layout = device.createDescriptorSetLayout(
    vk::DescriptorSetLayoutCreateInfo({}, bindings));
```

**3b. Pipeline layout and compute pipeline:**

The pipeline layout connects descriptor sets and push constants to the shader:

```cpp
vk::PushConstantRange push_range(vk::ShaderStageFlagBits::eCompute, 0, sizeof(uint32_t));
pipeline_layout = device.createPipelineLayout(
    vk::PipelineLayoutCreateInfo({}, 1, &descriptor_set_layout, 1, &push_range));
```

Then the compute pipeline is created — binding the shader module entry point
to the pipeline layout:

```cpp
auto rv = device.createComputePipeline(
    nullptr, vk::ComputePipelineCreateInfo({}, shader_stage, pipeline_layout));
```

**3c. Descriptor pool and descriptor set:**

Allocate a descriptor set from a pool, then write buffer handles into it:

```cpp
descriptor_sets = device.allocateDescriptorSets(
    vk::DescriptorSetAllocateInfo(descriptor_pool, 1, &descriptor_set_layout));
```

**3d. Buffer creation and memory allocation:**

For each buffer, the code creates a `vk::Buffer`, queries its memory
requirements, finds a suitable memory type (host-visible + host-coherent),
allocates `vk::DeviceMemory`, and binds it:

```cpp
buf_a = device.createBuffer(vk::BufferCreateInfo(
    {}, buf_size, vk::BufferUsageFlagBits::eStorageBuffer, vk::SharingMode::eExclusive));
auto reqs = device.getBufferMemoryRequirements(buf);
uint32_t type_idx = find_memory_type(reqs);  // host-visible + host-coherent
vk::DeviceMemory mem = device.allocateMemory(vk::MemoryAllocateInfo(reqs.size, type_idx));
device.bindBufferMemory(buf, mem, 0);
```

**3e. Upload data:**

Map device memory, copy host data in, unmap:

```cpp
void* mapped = device.mapMemory(mem, 0, size);
std::memcpy(mapped, data, size);
device.unmapMemory(mem);
```

**3f. Update descriptor sets:**

Connect the buffer handles to the descriptor set bindings:

```cpp
vk::DescriptorBufferInfo buf_info_a(buf_a, 0, buf_size);
vk::WriteDescriptorSet write{descriptor_sets[0], 0, 0, 1,
    vk::DescriptorType::eStorageBuffer, nullptr, &buf_info_a};
device.updateDescriptorSets(writes.size(), writes.data(), 0, nullptr);
```

**3g. Record command buffer:**

Create a command pool and buffer, then record: bind pipeline, bind descriptor
sets, push constants, dispatch:

```cpp
cmd_buffers[0].begin(vk::CommandBufferBeginInfo(vk::CommandBufferUsageFlagBits::eOneTimeSubmit));
cmd_buffers[0].bindPipeline(vk::PipelineBindPoint::eCompute, compute_pipeline);
cmd_buffers[0].bindDescriptorSets(vk::PipelineBindPoint::eCompute, pipeline_layout, 0, 1,
                                  descriptor_sets.data(), 0, nullptr);
cmd_buffers[0].pushConstants(pipeline_layout, vk::ShaderStageFlagBits::eCompute, 0,
                             sizeof(uint32_t), &n_u32);
uint32_t group_count = (n + 63U) / 64U;
cmd_buffers[0].dispatch(group_count, 1, 1);
cmd_buffers[0].end();
```

The dispatch math: the shader uses `local_size_x = 64`, so each work-group
processes 64 elements. The host dispatches `(n + 63) / 64` groups to cover all
`n` elements (with potential overshoot, guarded in the shader).

**3h. Submit and wait:**

```cpp
vk::Fence fence = device.createFence(vk::FenceCreateInfo());
runtime_.Queue().submit(submit_info, fence);
device.waitForFences(fence, vk::True, UINT64_MAX);
```

**3i. Read back results:**

```cpp
void* mapped_c = device.mapMemory(mem_c, 0, buf_size);
std::memcpy(c, mapped_c, buf_size);
device.unmapMemory(mem_c);
```

**3j. Cleanup:**

All Vulkan objects created in `Execute()` are destroyed by the `cleanup`
lambda — descriptor set layout, pipeline layout, pipeline, descriptor pool,
buffers, device memory, command pool, and fence.

### Step 4: The compute shader (`vector_add_kernel.comp`)

```glsl
#version 450

layout(local_size_x = 64) in;

layout(binding = 0) buffer BufferA { float data_A[]; };
layout(binding = 1) buffer BufferB { float data_B[]; };
layout(binding = 2) buffer BufferC { float data_C[]; };

layout(push_constant) uniform PushConstants { uint n; };

void main() {
    uint i = gl_GlobalInvocationID.x;
    if (i < n) {
        data_C[i] = data_A[i] + data_B[i];
    }
}
```

Line by line:

- `#version 450` — GLSL version targeting SPIR-V 1.0 / Vulkan 1.0.
- `layout(local_size_x = 64) in` — Each work-group has 64 invocations
  (threads). The host must dispatch enough groups to cover all elements.
- `buffer BufferA { float data_A[]; }` — SSBO (Shader Storage Buffer Object)
  bound at descriptor set binding 0. Runtime-sized arrays (`data_A[]`) are
  valid in SSBOs.
- `push_constant uniform PushConstants { uint n; }` — A small value (`n`)
  passed directly in the command buffer via `pushConstants`, avoiding the need
  for a separate uniform buffer.
- `gl_GlobalInvocationID.x` — Global thread index across all dispatched
  work-groups. Equivalent to `get_global_id(0)` in OpenCL or
  `threadIdx.x + blockIdx.x * blockDim.x` in CUDA.
- `if (i < n)` — Bounds guard. Since `group_count = (n + 63) / 64`, the last
  group may spawn invocations beyond `n`. This guard prevents out-of-bounds
  writes.

### Step 5: Validation (`main.cpp`)

Back on the host, `main.cpp` loops over all `n` elements and checks each one:

```cpp
for (int i = 0; i < kN; ++i) {
    float want = h_a[i] + h_b[i];
    if (std::abs(h_c[i] - want) > 1e-5F) {
        std::cerr << "Mismatch at " << i << ": want " << want << " got " << h_c[i] << "\n";
        return 1;
    }
}
```

---

## Build and run

From repo root:

```bash
bazel build //src/cpp_accelerator/cmd/hello-world-vulkan:hello-world-vulkan
bazel run   //src/cpp_accelerator/cmd/hello-world-vulkan:hello-world-vulkan
```

Expected line:

```text
Vulkan hello world OK (SPIR-V embedded in binary, n=1024)
```

## Requirements

- Vulkan-capable driver + working ICD (`vulkaninfo` should see a physical device).
- `glslc` on `PATH` (Vulkan SDK or distro package).
- `xxd` on host.
- `libvulkan` available for linker/runtime.

## What this example does

- `vulkan_runtime.cpp` creates instance/device/queue; `main.cpp` drives the program and validates output.
- `vector_add.cpp` builds a compute pipeline and dispatches work.
- `vector_add_kernel.comp` is GLSL compute shader source.
- Build embeds SPIR-V bytes into the binary; no runtime `.spv` file read.

## Embedded shader flow

`spirv_shader_embed_cpp(name = "vector_add_kernel", shader = "vector_add_kernel.comp")`
in `BUILD` generates:

- `:vector_add_kernel_spirv` -> `vector_add_kernel.spv`
- `:vector_add_kernel_spv_embed` -> `vector_add_kernel_spv_embed.cpp`
- `:vector_add_kernel_blob_h` -> `vector_add_kernel_blob.h`

`vector_add.cpp` includes:

```cpp
#include "src/cpp_accelerator/cmd/hello-world-vulkan/vector_add_kernel_blob.h"
```

and accesses bytes through:

- `vector_add_kernel_blob::spirv()`
- `vector_add_kernel_blob::spirv_size_bytes()`

## Troubleshooting

- `glslc: not found`: install Vulkan SDK / package and ensure it is on `PATH`.
- `xxd: not found`: install package that provides `xxd` (`vim-common` on Debian/Ubuntu).
- Link/runtime Vulkan errors: verify `libvulkan` and driver installation.
- No physical devices: check ICD/driver with `vulkaninfo`.
