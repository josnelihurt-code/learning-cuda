load("@rules_cc//cc:defs.bzl", "cc_binary", "cc_library")
load("//bazel/defs:opencl_cl_to_spirv.bzl", "opencl_kernel_embedded")

opencl_kernel_embedded(
    name = "vector_add_kernel",
    cl_src = "vector_add_kernel.cl",
)

cc_library(
    name = "hello_world_opencl_lib",
    srcs = [
        "main.cpp",
        "opencl_program.cpp",
        "opencl_runtime.cpp",
        "vector_add.cpp",
        ":vector_add_kernel_spv_embed",
        ":vector_add_kernel_cl_embed",
    ],
    hdrs = [
        "opencl_program.h",
        "opencl_runtime.h",
        "vector_add.h",
        ":vector_add_kernel_blob_h",
    ],
    defines = ["CL_TARGET_OPENCL_VERSION=300"],
    visibility = ["//visibility:private"],
    deps = [
        "@opencl_sdk//:opencl",
    ],
)

cc_binary(
    name = "hello-world-opencl",
    visibility = ["//visibility:public"],
    deps = [":hello_world_opencl_lib"],
    linkopts = [
        "/lib/x86_64-linux-gnu/libOpenCL.so.1.0.0",
    ],
)
