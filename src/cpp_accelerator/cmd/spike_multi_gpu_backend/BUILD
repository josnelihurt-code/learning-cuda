load("@rules_cc//cc:defs.bzl", "cc_binary", "cc_library")

cc_library(
    name = "grayscale_backend_iface",
    srcs = ["grayscale_backend.cpp"],
    hdrs = ["grayscale_backend.h"],
    visibility = ["//visibility:public"],
)

cc_library(
    name = "demo_common",
    srcs = ["demo_common.cpp"],
    hdrs = ["demo_common.h"],
    visibility = ["//visibility:public"],
    deps = [
        ":grayscale_backend_iface",
        "@spdlog",
    ],
)

cc_library(
    name = "cuda_grayscale_backend",
    srcs = ["cuda_grayscale_backend.cpp"],
    hdrs = ["cuda_grayscale_backend.h"],
    visibility = ["//visibility:public"],
    deps = [
        ":grayscale_backend_iface",
        "//src/cpp_accelerator/adapters/compute/cuda/kernels:kernels",
    ],
)

cc_library(
    name = "mock_opencl_grayscale_backend",
    srcs = ["mock_opencl_grayscale_backend.cpp"],
    hdrs = ["mock_opencl_grayscale_backend.h"],
    visibility = ["//visibility:public"],
    deps = [
        ":grayscale_backend_iface",
        "@spdlog",
    ],
)

cc_binary(
    name = "spike_cuda",
    srcs = ["main_cuda.cpp"],
    visibility = ["//visibility:public"],
    deps = [
        ":cuda_grayscale_backend",
        ":demo_common",
        "@spdlog",
    ],
)

cc_binary(
    name = "spike_opencl_mock",
    srcs = ["main_opencl_mock.cpp"],
    visibility = ["//visibility:public"],
    deps = [
        ":demo_common",
        ":mock_opencl_grayscale_backend",
        "@spdlog",
    ],
)
