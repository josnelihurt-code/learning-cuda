__kernel void vector_add_kernel(__global const float* A,
                                __global const float* B,
                                __global float* C,
                                const int n) {
    int i = get_global_id(0);
    if (i < n) {
        C[i] = A[i] + B[i];
    }
}
