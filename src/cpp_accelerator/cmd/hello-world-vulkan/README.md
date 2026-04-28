# Vulkan compute hello world

Minimal compute-shader example: add two float vectors on the GPU with Vulkan and Vulkan-Hpp.
This sample is for learning, not production architecture.

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

- `main.cpp` creates instance/device/queue and validates output.
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
