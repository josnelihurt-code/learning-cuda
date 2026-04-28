load("@rules_cc//cc:defs.bzl", "cc_binary", "cc_library")
load("//bazel/defs:spirv_embed.bzl", "spirv_shader_embed_cpp")

spirv_shader_embed_cpp(
    name = "vector_add_kernel",
    shader = "vector_add_kernel.comp",
)

cc_library(
    name = "hello_world_vulkan_vector_add",
    srcs = [
        "vector_add.cpp",
        ":vector_add_kernel_spv_embed",
    ],
    hdrs = [
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
    srcs = ["main.cpp"],
    visibility = ["//visibility:public"],
    deps = [
        ":hello_world_vulkan_vector_add",
        "@vulkan_hpp//:vulkan_hpp",
    ],
    linkopts = ["-lvulkan"],
)
