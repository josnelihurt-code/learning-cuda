load("@rules_cc//cc:defs.bzl", "cc_library")

# Unified camera implementations using modular backends (V4L2, NVIDIA Argus, Stub).
# Each backend can be independently enabled via Bazel flags:
# - --//bazel/flags:v4l2_camera=true    (USB cameras on x86 or Jetson)
# - --//bazel/flags:nvidia_argus_camera=true (NVIDIA Argus on Jetson)
# If no backends are enabled, the stub backend is used (returns no cameras).

cc_library(
    name = "camera_detector",
    srcs = [
        "camera_detector.cpp",
        "camera_detector_impl.cpp",
        "camera_detector_impl.h",
    ] + select({
        "//bazel/flags:v4l2_camera_enabled": ["camera_detector_impl_v4l2.cpp"],
        "//conditions:default": ["camera_detector_impl_v4l2_stub.cpp"],
    }) + select({
        "//bazel/flags:nvidia_argus_camera_enabled": ["camera_detector_impl_argus.cpp"],
        "//conditions:default": ["camera_detector_impl_argus_stub.cpp"],
    }),
    hdrs = ["camera_detector.h"],
    visibility = ["//visibility:public"],
    deps = [
        ":backends",
        "//proto:accelerator_control_cc_proto",
        "@spdlog",
    ],
)

cc_library(
    name = "nvbuf_cuda_utils",
    srcs = select({
        "//bazel/flags:nvidia_argus_camera_enabled": ["nvbuf_cuda_utils.cpp"],
        "//conditions:default": [],
    }),
    hdrs = ["nvbuf_cuda_utils.h"],
    visibility = ["//visibility:public"],
    copts = select({
        "//bazel/flags:nvidia_argus_camera_enabled": [
            "-I/usr/include/gstreamer-1.0",
            "-I/usr/include/glib-2.0",
        ],
        "//conditions:default": [],
    }),
    deps = select({
        "//bazel/flags:nvidia_argus_camera_enabled": [
            "//third_party/glib-config:glibconfig",
            "//third_party/gstreamer:gstreamer",
            "//third_party/nvbufsurface:nvbufsurface",
            "@spdlog",
        ],
        "//conditions:default": ["@spdlog"],
    }),
    linkopts = select({
        "//bazel/flags:nvidia_argus_camera_enabled": [
            "-lnvbuf_utils",
        ],
        "//conditions:default": [],
    }),
)

cc_library(
    name = "gpu_frame_processor",
    srcs = select({
        "//bazel/flags:nvidia_argus_camera_enabled": ["gpu_frame_processor.cpp"],
        "//conditions:default": [],
    }),
    hdrs = ["gpu_frame_processor.h"],
    visibility = ["//visibility:public"],
    copts = select({
        "//bazel/flags:nvidia_argus_camera_enabled": [
            "-I/usr/include/gstreamer-1.0",
            "-I/usr/include/glib-2.0",
        ],
        "//conditions:default": [],
    }),
    deps = [
        ":nvbuf_cuda_utils",
        "//src/cpp_accelerator/adapters/compute/cuda/kernels:kernels",
        "@spdlog",
    ] + select({
        "//bazel/flags:nvidia_argus_camera_enabled": [
            "//third_party/glib-config:glibconfig",
            "//third_party/gstreamer:gstreamer",
            "//third_party/nvbufsurface:nvbufsurface",
        ],
        "//conditions:default": [],
    }),
    linkopts = select({
        "//bazel/flags:nvidia_argus_camera_enabled": [
            "-lEGL",
        ],
        "//conditions:default": [],
    }),
)

cc_library(
    name = "gst_camera_source",
    srcs = [
        "gst_camera_source.cpp",
        "gst_camera_source_impl.cpp",
        "gst_camera_source_impl.h",
    ] + select({
        "//bazel/flags:v4l2_camera_enabled": ["gst_camera_source_impl_v4l2.cpp"],
        "//conditions:default": ["gst_camera_source_impl_v4l2_stub.cpp"],
    }) + select({
        "//bazel/flags:nvidia_argus_camera_enabled": ["gst_camera_source_impl_argus.cpp"],
        "//conditions:default": ["gst_camera_source_impl_argus_stub.cpp"],
    }) + select({
        "//bazel/flags:nvidia_argus_camera_enabled": ["gst_camera_source_impl_gpu_argus.cpp"],
        "//conditions:default": ["gst_camera_source_impl_gpu_stub.cpp"],
    }),
    hdrs = ["gst_camera_source.h"],
    visibility = ["//visibility:public"],
    deps = [
        ":backends",
        ":gpu_frame_processor",
        "@com_github_paullouisageneau_libdatachannel//:libdatachannel",
        "@spdlog",
    ],
)

cc_library(
    name = "backends",
    deps = [
        "//src/cpp_accelerator/adapters/camera/backends:camera_backend",
        "//src/cpp_accelerator/adapters/camera/backends:stub_backend",
    ] + select({
        "//bazel/flags:v4l2_camera_enabled": ["//src/cpp_accelerator/adapters/camera/backends:v4l2_backend"],
        "//conditions:default": [],
    }) + select({
        "//bazel/flags:nvidia_argus_camera_enabled": ["//src/cpp_accelerator/adapters/camera/backends:nvidia_argus_backend"],
        "//conditions:default": [],
    }),
)

cc_library(
    name = "camera_hub",
    srcs = ["camera_hub.cpp"],
    hdrs = ["camera_hub.h"],
    visibility = ["//visibility:public"],
    deps = [
        ":gst_camera_source",
        "@com_github_paullouisageneau_libdatachannel//:libdatachannel",
        "@spdlog",
    ],
)
