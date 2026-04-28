load("@rules_cuda//cuda:defs.bzl", "cuda_library")

cuda_library(
    name = "kernels",
    srcs = [
        "blur/non_separable.cu",
        "blur/separable_basic.cu",
        "blur/separable_tiled.cu",
        "grayscale_kernel.cu",
        "letterbox_kernel.cu",
    ],
    hdrs = [
        "blur/device_utils.cuh",
        "blur_kernel.h",
        "grayscale_kernel.h",
        "letterbox_kernel.h",
    ],
    copts = [
        "--expt-relaxed-constexpr",
        "-Xcompiler=-Wno-error",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//src/cpp_accelerator/adapters/compute/cuda/memory:cuda_memory_pool",
    ],
)
