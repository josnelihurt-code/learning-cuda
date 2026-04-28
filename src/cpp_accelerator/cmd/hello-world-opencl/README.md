# OpenCL hello world

Minimal OpenCL example: add two float vectors on the GPU and verify the result.
This sample uses embedded shader assets generated at build time.

## Build and run

From repo root:

```bash
bazel build //src/cpp_accelerator/cmd/hello-world-opencl:hello-world-opencl
bazel run   //src/cpp_accelerator/cmd/hello-world-opencl:hello-world-opencl
```

Expected output ends with one of:

```text
OpenCL hello world OK (n=8) — embedded SPIR-V IL (clCreateProgramWithIL)
```

or

```text
OpenCL hello world OK (n=8) — embedded OpenCL C (clCreateProgramWithSource)
```

The second line is fallback mode when IL loading is unavailable on the OpenCL runtime.

## Requirements

- OpenCL platform/device and runtime (`libOpenCL.so`).
- `xxd` on host.
- Bazel.
- For SPIR-V build path in this repo: host `clang` and `llvm-spirv` translator inputs configured in `MODULE.bazel`.

## What this sample does

- Initializes OpenCL platform/device/context/queue.
- Tries to create program from embedded SPIR-V IL first.
- Falls back to embedded OpenCL C source if IL program creation fails.
- Builds program, launches `vector_add_kernel`, reads back and validates output.

## Embedded asset flow

`opencl_kernel_embedded(name = "vector_add_kernel", cl_src = "vector_add_kernel.cl")`
in `BUILD` expands to:

- `:vector_add_kernel_spv` -> `vector_add_kernel.spv` (OpenCL C -> SPIR-V)
- `:vector_add_kernel_spv_embed` -> generated C++ bytes for SPIR-V
- `:vector_add_kernel_cl_embed` -> generated C++ bytes for `.cl` source
- `:vector_add_kernel_blob_h` -> generated header API

`main.cpp` includes:

```cpp
#include "src/cpp_accelerator/cmd/hello-world-opencl/vector_add_kernel_blob.h"
```

and uses:

- `vector_add_kernel_blob::spirv()`
- `vector_add_kernel_blob::spirv_size_bytes()`
- `vector_add_kernel_blob::cl_src()`
- `vector_add_kernel_blob::cl_src_size_bytes()`

## Troubleshooting

- `clCreateProgramWithIL` fails: runtime may not support OpenCL IL on this device/driver; fallback to source path should still work.
- `Embedded OpenCL C source is empty`: verify `vector_add_kernel.cl` and `:vector_add_kernel_cl_embed` wiring in `BUILD`.
- OpenCL link/runtime errors: check OpenCL loader/library path and installed ICD.
