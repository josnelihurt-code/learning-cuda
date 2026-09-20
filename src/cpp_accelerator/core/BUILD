cc_library(
    name = "result",
    hdrs = ["result.h"],
    visibility = ["//visibility:public"],
)

genrule(
    name = "library_version_private",
    srcs = ["//:src/cpp_accelerator/VERSION"],
    outs = ["library_version_private.h"],
    cmd = "VERSION=$$(cat $(location //:src/cpp_accelerator/VERSION) | tr -d '[:space:]'); echo '#pragma once' > $@; echo '#define LIBRARY_VERSION \"'$$VERSION'\"' >> $@",
)

genrule(
    name = "git_hash_private",
    outs = ["git_hash_private.h"],
    cmd = "if [ -n \"$${LIBRARY_GIT_HASH:-}\" ]; then HASH=\"$$LIBRARY_GIT_HASH\"; else HASH=$$(git rev-parse --short HEAD 2>/dev/null || echo 'dev'); fi; echo '#pragma once' > $@; echo '#define LIBRARY_GIT_HASH \"'$$HASH'\"' >> $@",
)

cc_library(
    name = "version",
    hdrs = [
        "version.h",
        ":git_hash_private",
        ":library_version_private",
    ],
    visibility = ["//visibility:public"],
)

cc_library(
    name = "otel_config",
    srcs = ["otel_config.cpp"],
    hdrs = ["otel_config.h"],
    visibility = ["//visibility:public"],
)

cc_library(
    name = "otel_log_sink",
    srcs = ["otel_log_sink.cpp"],
    hdrs = ["otel_log_sink.h"],
    visibility = ["//visibility:public"],
    deps = [
        "@spdlog",
        "@io_opentelemetry_cpp//api:api",
        "@io_opentelemetry_cpp//exporters/otlp:otlp_grpc_log_record_exporter",
        "@io_opentelemetry_cpp//sdk/src/logs:logs",
        "@io_opentelemetry_cpp//sdk/src/resource:resource",
        ":otel_config",
        ":version",
    ],
)

cc_library(
    name = "otel_metrics",
    srcs = ["otel_metrics.cpp"],
    hdrs = ["otel_metrics.h"],
    visibility = ["//visibility:public"],
    deps = [
        "@spdlog",
        "@io_opentelemetry_cpp//api:api",
        "@io_opentelemetry_cpp//exporters/otlp:otlp_grpc_metric_exporter",
        "@io_opentelemetry_cpp//sdk/src/metrics:metrics",
        "@io_opentelemetry_cpp//sdk/src/resource:resource",
        ":otel_config",
        ":version",
    ],
)

cc_library(
    name = "logger",
    srcs = ["logger.cpp"],
    hdrs = ["logger.h"],
    visibility = ["//visibility:public"],
    deps = [
        ":otel_log_sink",
        "@spdlog",
    ],
    linkopts = [
        "-lavcodec",
        "-lavutil",
    ],
)

cc_library(
    name = "telemetry",
    srcs = ["telemetry_stub.cpp"],
    hdrs = ["telemetry.h"],
    visibility = ["//visibility:public"],
    deps = [
        "@spdlog",
    ],
)

cc_library(
    name = "signal_handler",
    srcs = ["signal_handler.cpp"],
    hdrs = ["signal_handler.h"],
    visibility = ["//visibility:public"],
    deps = [
        "@spdlog",
    ],
)

cc_test(
    name = "logger_test",
    srcs = ["logger_test.cpp"],
    deps = [
        ":logger",
        "@googletest//:gtest_main",
        "@spdlog",
    ],
)

cc_test(
    name = "result_test",
    srcs = ["result_test.cpp"],
    deps = [
        ":result",
        "@googletest//:gtest_main",
    ],
)
