cc_library(
    name = "opencl",
    hdrs = glob([
        "external/OpenCL-Headers/CL/*.h",
        "external/OpenCL-CLHPP/include/CL/*.hpp",
        "lib/include/CL/**/*.h",
        "lib/include/CL/**/*.hpp",
    ], allow_empty = True),
    includes = [
        "external/OpenCL-Headers",
        "external/OpenCL-CLHPP/include",
        "lib/include",
    ],
    visibility = ["//visibility:public"],
)
