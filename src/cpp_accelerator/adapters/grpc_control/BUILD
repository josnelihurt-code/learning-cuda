load("@rules_cc//cc:defs.bzl", "cc_library")

cc_library(
    name = "processor_engine_provider",
    hdrs = ["processor_engine_provider.h"],
    visibility = ["//visibility:public"],
    deps = [
        "//proto:image_processor_service_cc_proto",
        "//src/cpp_accelerator/core:otel_metrics",
    ],
)

cc_library(
    name = "processor_engine_adapter",
    srcs = ["processor_engine_adapter.cpp"],
    hdrs = ["processor_engine_adapter.h"],
    visibility = ["//visibility:public"],
    deps = [
        ":processor_engine_provider",
        "//src/cpp_accelerator/application/engine:engine",
    ],
)

cc_library(
    name = "accelerator_control_client",
    srcs = ["accelerator_control_client.cpp"],
    hdrs = ["accelerator_control_client.h"],
    visibility = ["//visibility:public"],
    deps = [
        ":processor_engine_provider",
        "//src/cpp_accelerator/adapters/webrtc:webrtc_manager",
        "//proto:accelerator_control_cc_proto",
        "//proto:accelerator_control_grpc",
        "//proto:image_processor_service_cc_proto",
        "//proto:common_cc_proto",
        "//src/cpp_accelerator/ports:i_control_port",
        "@com_github_grpc_grpc//:grpc++",
        "@spdlog",
    ],
)
