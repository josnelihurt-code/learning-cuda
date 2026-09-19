load("@rules_cc//cc:defs.bzl", "cc_library", "cc_test")

cc_library(
    name = "sdp_utils",
    srcs = ["sdp_utils.cpp"],
    hdrs = ["sdp_utils.h"],
    visibility = ["//visibility:public"],
    deps = [
        "@com_github_paullouisageneau_libdatachannel//:libdatachannel",
        "@spdlog",
    ],
)

cc_test(
    name = "sdp_utils_test",
    srcs = ["sdp_utils_test.cpp"],
    deps = [
        ":sdp_utils",
        "@googletest//:gtest_main",
    ],
)
