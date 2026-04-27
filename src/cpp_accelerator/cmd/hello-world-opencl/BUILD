load("@rules_cc//cc:defs.bzl", "cc_binary")

cc_binary(
    name = "hello-world-opencl",
    srcs = ["main.cpp"],
    visibility = ["//visibility:public"],
    deps = [
        "@opencl_sdk//:opencl",
        "@spdlog",
    ],
    linkopts = [
        "/lib/x86_64-linux-gnu/libOpenCL.so.1.0.0",
    ],
)
