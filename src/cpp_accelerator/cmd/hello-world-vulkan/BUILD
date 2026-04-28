load("@rules_cc//cc:defs.bzl", "cc_binary", "cc_library")
load("//bazel/defs:spirv_embed.bzl", "spirv_shader_embed_cpp")

spirv_shader_embed_cpp(
    name = "vector_add_kernel",
    shader = "vector_add_kernel.comp",
)

cc_library(
    name = "hello_world_vulkan_lib",
    srcs = [
        "main.cpp",
        "vulkan_runtime.cpp",
        "vulkan_program.cpp",
        "vector_add.cpp",
        ":vector_add_kernel_spv_embed",
    ],
    hdrs = [
        "vulkan_runtime.h",
        "vulkan_program.h",
        "vector_add.h",
        ":vector_add_kernel_blob_h",
    ],
    visibility = ["//visibility:private"],
    deps = [
        "@vulkan_hpp//:vulkan_hpp",
    ],
)

cc_binary(
    name = "hello-world-vulkan",
    visibility = ["//visibility:public"],
    deps = [":hello_world_vulkan_lib"],
    linkopts = ["-lvulkan"],
)
