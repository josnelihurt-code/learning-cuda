# OpenCL hello world

Minimal OpenCL example: add two float vectors on the GPU and verify the result.
This sample uses embedded shader assets generated at build time.

## What is OpenCL?

OpenCL (Open Computing Language) is an open, royalty-free standard for
heterogeneous parallel computing. It was created by the Khronos Group and first
released as OpenCL 1.0 in 2009. The current specification is OpenCL 3.0.

OpenCL lets you write programs that execute across CPUs, GPUs, DSPs, FPGAs, and
other accelerators. Unlike CUDA, it is not tied to a single vendor — code
written against the OpenCL API can run on hardware from NVIDIA, AMD, Intel,
ARM, Qualcomm, and others.

Key versions:

| Version | Year | Highlights |
|---------|------|------------|
| 1.0 | 2009 | Initial release, C-based kernel language |
| 1.2 | 2011 | Device partitioning, separate compilation |
| 2.0 | 2013 | Shared virtual memory, dynamic parallelism, C++ kernel language |
| 2.2 | 2017 | SPIR-V as intermediate language |
| 3.0 | 2020 | Optional features model, backwards-compatible with 1.2 |

## Core Concepts You Need to Know

### The Platform Model

OpenCL organizes the hardware into a hierarchy:

- **Platform** — A vendor's OpenCL implementation (ICD — Installable Client
  Driver). A system can have multiple platforms (e.g., one from NVIDIA, one from
  Intel). You enumerate them with `clGetPlatformIDs`.
- **Device** — A compute unit within a platform. A platform may expose multiple
  devices (e.g., a GPU and a CPU). Devices have types: `CL_DEVICE_TYPE_GPU`,
  `CL_DEVICE_TYPE_CPU`, `CL_DEVICE_TYPE_ACCELERATOR`, etc.
- **Context** — An execution environment that groups devices, memory objects,
  and command queues. Created with `clCreateContext`.
- **Command Queue** — A queue that holds commands (kernel launches, memory
  transfers) submitted to a specific device. Created with
  `clCreateCommandQueueWithProperties`.

### The Execution Model

OpenCL executes code in a data-parallel fashion:

- **Kernel** — A function written in OpenCL C (or compiled to SPIR-V) that runs
  on the device. Marked with the `__kernel` qualifier.
- **Work-item** — A single instance of a kernel invocation. Each work-item has
  a unique global ID.
- **Work-group** — A collection of work-items that share local memory and can
  synchronize. Work-groups are scheduled independently.
- **NDRange** — The total set of work-items, organized in 1D, 2D, or 3D. The
  host specifies the global size and optionally the local (work-group) size when
  calling `clEnqueueNDRangeKernel`.

Work-items get their ID from built-in functions:

```c
get_global_id(dim)    // global index in dimension dim
get_local_id(dim)     // index within the work-group
get_global_size(dim)  // total number of work-items in dimension dim
```

### The Memory Model

OpenCL defines four memory regions accessible from kernels:

| Region | Qualifier | Scope | Notes |
|--------|-----------|-------|-------|
| Global | `__global` | All work-items | Main device memory, accessible by host via buffers |
| Constant | `__constant` | All work-items | Read-only, cached |
| Local | `__local` | Work-group | Shared within a work-group, fast |
| Private | `__private` | Single work-item | Registers / stack, default for function locals |

The host manages device memory through buffer objects (`cl_mem`), created with
`clCreateBuffer`. Data is transferred via `clEnqueueWriteBuffer` /
`clEnqueueReadBuffer` or at buffer creation time with `CL_MEM_COPY_HOST_PTR`.

### The Programming Model

**Kernel language**: OpenCL C (a subset of C99 with vector types and address
space qualifiers). OpenCL 2.2+ also accepts SPIR-V as an intermediate language.

**Host API**: A C API exposed through `<CL/cl.h>`. Functions follow the naming
pattern `clVerbNoun` (`clCreateBuffer`, `clSetKernelArg`, etc.). Every function
returns a `cl_int` error code.

**Compilation flow**:

1. At build time, kernel source (`.cl`) can be compiled to SPIR-V IL.
2. At runtime, the host loads either SPIR-V IL (`clCreateProgramWithIL`) or
   raw source (`clCreateProgramWithSource`).
3. The program is built for the target device with `clBuildProgram`.
4. Kernels are extracted with `clCreateKernel`.

## Hardware Considerations

OpenCL runs on a wide range of hardware:

| Vendor | Support | Notes |
|--------|---------|-------|
| NVIDIA | OpenCL 1.2–3.0 | All GeForce / Quadro / Tesla GPUs |
| AMD | OpenCL 1.2–2.0 | Radeon GPUs, ROCm stack |
| Intel | OpenCL 1.2–3.0 | Integrated GPUs (Gen9+), Xeon Phi, oneAPI |
| ARM | OpenCL 1.2–2.0 | Mali GPUs |
| Qualcomm | OpenCL 2.0 | Adreno GPUs |

The ICD (Installable Client Driver) loader dispatches API calls to the correct
vendor driver. Install the vendor's driver package and verify with:

```bash
clinfo          # lists all platforms and devices
```

## OpenCL vs CUDA — A Comparison

| Dimension | OpenCL | CUDA |
|-----------|--------|------|
| Vendor lock-in | None — runs on NVIDIA, AMD, Intel, ARM, etc. | NVIDIA GPUs only |
| Kernel language | OpenCL C, SPIR-V IL | CUDA C++ (`.cu` files, `nvcc` compiler) |
| Kernel compilation | Runtime (source or IL → device binary) | At runtime (NVCC / driver JIT) or ahead-of-time |
| Memory management | Explicit `clCreateBuffer`, `clEnqueueRead/WriteBuffer` | `cudaMalloc`, `cudaMemcpy` |
| API style | C API with `cl*` functions, manual error codes | C++ API with `cuda*` functions, error codes or exceptions |
| Boilerplate | Moderate — context/queue setup, kernel arg setting | Lower — context is implicit, kernel launch syntax is `<<<>>>` |
| Ecosystem | Khronos ecosystem, wider hardware reach | NVIDIA ecosystem (cuBLAS, cuDNN, Thrust, etc.) |
| Performance | Portable; may not reach vendor-specific peaks on any single GPU | Tuned for NVIDIA hardware, often best performance on NVIDIA |
| Debugging | Limited vendor tools | NVIDIA Nsight, cuda-memcheck |

**When to choose OpenCL**: You need cross-vendor portability, or you're
targeting non-NVIDIA hardware (AMD GPUs, Intel integrated GPUs, FPGAs).

**When to choose CUDA**: You are targeting NVIDIA GPUs exclusively and want
maximum performance, best tooling, and access to the CUDA ecosystem (cuBLAS,
cuDNN, TensorRT, etc.).

## How This Program Works (Step by Step)

This program adds two float vectors `A` and `B` to produce `C`, where
`C[i] = A[i] + B[i]`. It uses five source files:

| File | Role |
|------|------|
| `main.cpp` | Host entry point — sets up data, runs kernel, validates results |
| `opencl_runtime.cpp` | Platform → device → context → queue initialization |
| `opencl_program.cpp` | Loads SPIR-V IL or OpenCL C source, builds the program |
| `vector_add.cpp` | Creates buffers, sets kernel args, dispatches, reads back |
| `vector_add_kernel.cl` | The GPU kernel — the actual vector add |

### Step 1: Runtime initialization (`opencl_runtime.cpp`)

`OpenClRuntime::Initialize()` sets up the four foundational objects in order:

```cpp
clGetPlatformIDs(1, &platform_, nullptr);
clGetDeviceIDs(platform_, CL_DEVICE_TYPE_DEFAULT, 1, &device_, nullptr);
context_ = clCreateContext(nullptr, 1, &device_, nullptr, nullptr, &err);
queue_ = clCreateCommandQueueWithProperties(context_, device_, nullptr, &err);
```

This is the standard OpenCL bootstrap sequence: find a platform, get a device,
create a context for that device, and create a command queue to submit work.

### Step 2: Program loading (`opencl_program.cpp`)

`OpenClProgram::InitializeFromEmbedded()` loads the kernel code. It tries
SPIR-V IL first (faster, pre-compiled), then falls back to OpenCL C source:

```cpp
program_ = clCreateProgramWithIL(runtime.Context(), il, il_size, &err);
// if that fails:
program_ = clCreateProgramWithSource(runtime.Context(), 1, &src, &src_len, &err);
// then build for the target device:
clBuildProgram(program_, 1, &device, nullptr, nullptr, nullptr);
```

### Step 3: Execute the vector add (`vector_add.cpp`)

`OpenClVectorAddProgram::Execute()` does the full compute cycle:

**3a. Allocate device buffers and copy input data:**

```cpp
cl_mem d_a = clCreateBuffer(context, CL_MEM_READ_ONLY | CL_MEM_COPY_HOST_PTR,
                            n * sizeof(float), host_ptr_a, &err);
cl_mem d_b = clCreateBuffer(context, CL_MEM_READ_ONLY | CL_MEM_COPY_HOST_PTR,
                            n * sizeof(float), host_ptr_b, &err);
cl_mem d_c = clCreateBuffer(context, CL_MEM_WRITE_ONLY,
                            n * sizeof(float), nullptr, &err);
```

`CL_MEM_COPY_HOST_PTR` copies data from the host at creation time, so no
separate write call is needed.

**3b. Create the kernel and set arguments:**

```cpp
cl_kernel kernel = clCreateKernel(program, "vector_add_kernel", &err);
clSetKernelArg(kernel, 0, sizeof(cl_mem), &d_a);
clSetKernelArg(kernel, 1, sizeof(cl_mem), &d_b);
clSetKernelArg(kernel, 2, sizeof(cl_mem), &d_c);
clSetKernelArg(kernel, 3, sizeof(int), &n);
```

Arguments are set by index — index 0 is `A`, 1 is `B`, 2 is `C`, 3 is `n`.

**3c. Dispatch and read back:**

```cpp
size_t global = n;
clEnqueueNDRangeKernel(queue, kernel, 1, nullptr, &global, nullptr, 0, nullptr, nullptr);
clEnqueueReadBuffer(queue, d_c, CL_TRUE, 0, n * sizeof(float), c, 0, nullptr, nullptr);
clFinish(queue);
```

`CL_TRUE` makes `clEnqueueReadBuffer` blocking — it waits for the kernel to
finish before copying `d_c` into the host buffer `c`.

**3d. Cleanup:**

```cpp
clReleaseKernel(kernel);
clReleaseMemObject(d_a);
clReleaseMemObject(d_b);
clReleaseMemObject(d_c);
```

### Step 4: The kernel (`vector_add_kernel.cl`)

```c
__kernel void vector_add_kernel(__global const float* A,
                                __global const float* B,
                                __global float* C,
                                const int n) {
    int i = get_global_id(0);
    if (i < n) {
        C[i] = A[i] + B[i];
    }
}
```

Line by line:

- `__kernel` — Marks this function as a GPU kernel.
- `__global const float* A` — Pointer to global (device) memory, read-only.
- `__global float* C` — Pointer to global memory, writable.
- `const int n` — Scalar argument passed directly from the host (not a buffer).
- `get_global_id(0)` — Returns this work-item's index in the first dimension.
  The host enqueued an NDRange of size `n`, so each work-item gets a unique
  `i` from 0 to `n-1`.
- `if (i < n)` — Bounds check. This is necessary because the NDRange size may
  be rounded up to a multiple of the work-group size. In this program the host
  doesn't specify a work-group size, so the runtime picks one, and the global
  size is exactly `n` — but the guard is good practice.

### Step 5: Validation (`main.cpp`)

Back on the host, `main.cpp` loops over the result and checks each element:

```cpp
for (int i = 0; i < n; ++i) {
    const float want = h_a[i] + h_b[i];
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

`vector_add.cpp` includes:

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
