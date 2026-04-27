#include <CL/cl.h>
#include <iostream>
#include <vector>
#include <stdexcept>
#include <spdlog/spdlog.h>

const char* kernelSource = R"(
__kernel void vector_add(__global const float* A, __global const float* B, __global float* C, const int n) {
    int i = get_global_id(0);
    if (i < n) {
        C[i] = A[i] + B[i];
    }
}
)";

void checkCLError(cl_int err, const char* operation) {
    if (err != CL_SUCCESS) {
        spdlog::error("OpenCL error during {}: {}", operation, err);
        throw std::runtime_error(std::string("OpenCL error: ") + operation);
    }
}

int main() {
    spdlog::info("OpenCL Hello World - Vector Addition");

    cl_int err;

    cl_uint numPlatforms;
    err = clGetPlatformIDs(0, nullptr, &numPlatforms);
    checkCLError(err, "clGetPlatformIDs (count)");

    if (numPlatforms == 0) {
        spdlog::error("No OpenCL platforms found");
        return 1;
    }

    spdlog::info("Found {} OpenCL platform(s)", numPlatforms);

    std::vector<cl_platform_id> platforms(numPlatforms);
    err = clGetPlatformIDs(numPlatforms, platforms.data(), nullptr);
    checkCLError(err, "clGetPlatformIDs (get)");

    for (cl_uint i = 0; i < numPlatforms; ++i) {
        char platformName[128];
        err = clGetPlatformInfo(platforms[i], CL_PLATFORM_NAME, sizeof(platformName), platformName, nullptr);
        checkCLError(err, "clGetPlatformInfo");
        spdlog::info("Platform {}: {}", i, platformName);

        cl_uint numDevices;
        err = clGetDeviceIDs(platforms[i], CL_DEVICE_TYPE_ALL, 0, nullptr, &numDevices);
        checkCLError(err, "clGetDeviceIDs (count)");

        if (numDevices == 0) {
            spdlog::warn("  No devices found on this platform");
            continue;
        }

        spdlog::info("  Found {} device(s)", numDevices);

        std::vector<cl_device_id> devices(numDevices);
        err = clGetDeviceIDs(platforms[i], CL_DEVICE_TYPE_ALL, numDevices, devices.data(), nullptr);
        checkCLError(err, "clGetDeviceIDs (get)");

        for (cl_uint j = 0; j < numDevices; ++j) {
            char deviceName[128];
            err = clGetDeviceInfo(devices[j], CL_DEVICE_NAME, sizeof(deviceName), deviceName, nullptr);
            checkCLError(err, "clGetDeviceInfo");
            spdlog::info("  Device {}: {}", j, deviceName);
        }
    }

    cl_uint numDevices;
    err = clGetDeviceIDs(platforms[0], CL_DEVICE_TYPE_ALL, 0, nullptr, &numDevices);
    checkCLError(err, "clGetDeviceIDs (count)");

    if (numDevices == 0) {
        spdlog::error("No devices found on platform 0");
        return 1;
    }

    std::vector<cl_device_id> devices(numDevices);
    err = clGetDeviceIDs(platforms[0], CL_DEVICE_TYPE_ALL, numDevices, devices.data(), nullptr);
    checkCLError(err, "clGetDeviceIDs (get)");

    cl_context context = clCreateContext(nullptr, 1, &devices[0], nullptr, nullptr, &err);
    checkCLError(err, "clCreateContext");
    spdlog::info("Created OpenCL context");

    const cl_command_queue_properties properties[] = {0};
    cl_command_queue queue = clCreateCommandQueueWithProperties(context, devices[0], properties, &err);
    checkCLError(err, "clCreateCommandQueueWithProperties");
    spdlog::info("Created command queue");

    const int n = 1024;
    std::vector<float> h_A(n), h_B(n), h_C(n);

    for (int i = 0; i < n; ++i) {
        h_A[i] = static_cast<float>(i);
        h_B[i] = static_cast<float>(i * 2);
    }

    cl_mem d_A = clCreateBuffer(context, CL_MEM_READ_ONLY | CL_MEM_COPY_HOST_PTR, n * sizeof(float), h_A.data(), &err);
    checkCLError(err, "clCreateBuffer (A)");

    cl_mem d_B = clCreateBuffer(context, CL_MEM_READ_ONLY | CL_MEM_COPY_HOST_PTR, n * sizeof(float), h_B.data(), &err);
    checkCLError(err, "clCreateBuffer (B)");

    cl_mem d_C = clCreateBuffer(context, CL_MEM_WRITE_ONLY, n * sizeof(float), nullptr, &err);
    checkCLError(err, "clCreateBuffer (C)");
    spdlog::info("Created device buffers");

    size_t kernelSourceSize = strlen(kernelSource);
    cl_program program = clCreateProgramWithSource(context, 1, &kernelSource, &kernelSourceSize, &err);
    checkCLError(err, "clCreateProgramWithSource");

    err = clBuildProgram(program, 1, &devices[0], nullptr, nullptr, nullptr);
    if (err != CL_SUCCESS) {
        size_t logSize;
        clGetProgramBuildInfo(program, devices[0], CL_PROGRAM_BUILD_LOG, 0, nullptr, &logSize);
        std::vector<char> buildLog(logSize);
        clGetProgramBuildInfo(program, devices[0], CL_PROGRAM_BUILD_LOG, logSize, buildLog.data(), nullptr);
        spdlog::error("Kernel build failed:\n{}", buildLog.data());
        throw std::runtime_error("Kernel build failed");
    }
    spdlog::info("Built kernel program");

    cl_kernel kernel = clCreateKernel(program, "vector_add", &err);
    checkCLError(err, "clCreateKernel");

    err = clSetKernelArg(kernel, 0, sizeof(cl_mem), &d_A);
    checkCLError(err, "clSetKernelArg (A)");
    err = clSetKernelArg(kernel, 1, sizeof(cl_mem), &d_B);
    checkCLError(err, "clSetKernelArg (B)");
    err = clSetKernelArg(kernel, 2, sizeof(cl_mem), &d_C);
    checkCLError(err, "clSetKernelArg (C)");
    err = clSetKernelArg(kernel, 3, sizeof(int), &n);
    checkCLError(err, "clSetKernelArg (n)");

    size_t globalWorkSize = n;
    err = clEnqueueNDRangeKernel(queue, kernel, 1, nullptr, &globalWorkSize, nullptr, 0, nullptr, nullptr);
    checkCLError(err, "clEnqueueNDRangeKernel");
    spdlog::info("Enqueued kernel execution");

    err = clEnqueueReadBuffer(queue, d_C, CL_TRUE, 0, n * sizeof(float), h_C.data(), 0, nullptr, nullptr);
    checkCLError(err, "clEnqueueReadBuffer");
    spdlog::info("Read results back to host");

    err = clFinish(queue);
    checkCLError(err, "clFinish");

    bool correct = true;
    for (int i = 0; i < n; ++i) {
        float expected = h_A[i] + h_B[i];
        if (std::abs(h_C[i] - expected) > 1e-5) {
            spdlog::error("Mismatch at index {}: expected {}, got {}", i, expected, h_C[i]);
            correct = false;
            break;
        }
    }

    if (correct) {
        spdlog::info("Vector addition verification: PASSED");
    } else {
        spdlog::error("Vector addition verification: FAILED");
    }

    clReleaseMemObject(d_C);
    clReleaseMemObject(d_B);
    clReleaseMemObject(d_A);
    clReleaseKernel(kernel);
    clReleaseProgram(program);
    clReleaseCommandQueue(queue);
    clReleaseContext(context);

    spdlog::info("OpenCL resources cleaned up successfully");

    return correct ? 0 : 1;
}
