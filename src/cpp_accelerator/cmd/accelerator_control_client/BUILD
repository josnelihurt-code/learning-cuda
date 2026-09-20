load("@rules_cc//cc:defs.bzl", "cc_binary")

cc_binary(
    name = "accelerator_control_client",
    srcs = ["main.cpp"],
    visibility = ["//visibility:public"],
    deps = [
        "//src/cpp_accelerator/adapters/camera:camera_detector",
        "//src/cpp_accelerator/adapters/camera:camera_hub",
        "//src/cpp_accelerator/adapters/image_io:image_adapters",
        "//src/cpp_accelerator/adapters/grpc_control:accelerator_control_client",
        "//src/cpp_accelerator/application/bird_watch:bird_watcher",
        "//src/cpp_accelerator/adapters/grpc_control:processor_engine_adapter",
        "//src/cpp_accelerator/adapters/webrtc:webrtc_manager",
        "//src/cpp_accelerator/application/engine:engine",
        "//src/cpp_accelerator/core:otel_metrics",
        "//src/cpp_accelerator/core:signal_handler",
        "//src/cpp_accelerator/core:version",
        "//proto:image_processor_service_cc_proto",
        "@com_github_grpc_grpc//:grpc++",
        "@spdlog",
        "@com_google_absl//absl/flags:flag",
        "@com_google_absl//absl/flags:parse",
    ],
)
