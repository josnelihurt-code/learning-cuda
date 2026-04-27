load("@rules_cc//cc:defs.bzl", "cc_binary")

cc_binary(
    name = "hello-world-vulkan",
    srcs = ["main.cpp"],
    visibility = ["//visibility:public"],
    deps = [
        "@vulkan_hpp//:vulkan_hpp",
        "@spdlog",
    ],
    linkopts = [
        "/lib/x86_64-linux-gnu/libvulkan.so.1.3.275",
    ],
)
