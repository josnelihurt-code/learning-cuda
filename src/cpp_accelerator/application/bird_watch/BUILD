load("@rules_cc//cc:defs.bzl", "cc_library")

cc_library(
    name = "bird_watcher",
    srcs = ["bird_watcher.cpp"] + select({
        "//bazel/flags:nvidia_argus_camera_enabled": ["bird_watcher_gpu_argus.cpp"],
        "//conditions:default": ["bird_watcher_gpu_stub.cpp"],
    }),
    hdrs = ["bird_watcher.h"],
    visibility = ["//visibility:public"],
    deps = [
        "//proto:image_processor_service_cc_proto",
        "//src/cpp_accelerator/adapters/camera:camera_hub",
        "//src/cpp_accelerator/adapters/camera:gpu_frame_processor",
        "//src/cpp_accelerator/adapters/compute/cuda/memory:cuda_memory_pool",
        "//src/cpp_accelerator/application/engine:engine",
        "//src/cpp_accelerator/core:otel_metrics",
        "//src/cpp_accelerator/domain/interfaces:interfaces",
        "@com_github_paullouisageneau_libdatachannel//:libdatachannel",
        "@spdlog",
    ],
    linkopts = [
        "-lavcodec",
        "-lavutil",
        "-lswscale",
    ],
)
