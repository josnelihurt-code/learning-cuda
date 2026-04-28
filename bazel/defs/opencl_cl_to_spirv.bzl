load("//bazel/defs:spirv_embed.bzl", "spirv_bytes_embed_cpp", "text_bytes_embed_cpp")

def opencl_cl_to_spirv(name, cl_src, spirv):
    """Run extract-debs → clang → llvm-spirv to produce ``spirv``.

    Lower-level; prefer ``opencl_kernel_embedded`` when you want SPIR-V + CL embedded in the binary.

    Args:
      name: Genrule target name (e.g. ``\"vector_add_kernel_spv\"``).
      cl_src: Label of the ``.cl`` source (e.g. ``\"my_kernel.cl\"``).
      spirv: Output filename only (e.g. ``\"vector_add_kernel.spv\"``); must be a single output.

    Requires ``http_file`` repos ``@llvm_spirv_18_tool_deb`` and ``@libllvmspirvlib18_tool_deb``
    (see ``MODULE.bazel``). Host ``/usr/bin/clang``; Ubuntu 24.04 amd64 ``.deb`` URLs.
    """
    native.genrule(
        name = name,
        srcs = [
            cl_src,
            "@llvm_spirv_18_tool_deb//file",
            "@libllvmspirvlib18_tool_deb//file",
        ],
        outs = [spirv],
        cmd = """
        set -e
        ROOT=$$(pwd)
        TMP=$$(mktemp -d)
        trap 'rm -rf "$$TMP"' EXIT
        ( cd "$$TMP" && ar x "$$ROOT/$(location @llvm_spirv_18_tool_deb//file)" data.tar.zst && tar -xf data.tar.zst && rm -f data.tar.zst )
        ( cd "$$TMP" && ar x "$$ROOT/$(location @libllvmspirvlib18_tool_deb//file)" data.tar.zst && tar -xf data.tar.zst && rm -f data.tar.zst )
        export LD_LIBRARY_PATH="$$TMP/usr/lib/x86_64-linux-gnu:$$LD_LIBRARY_PATH"
        /usr/bin/clang -c -cl-std=CL2.0 -Xclang -finclude-default-header -target spir -O0 -emit-llvm \\
          -o "$$TMP/k.bc" "$$ROOT/$(location %s)"
        "$$TMP/usr/bin/llvm-spirv-18" "$$TMP/k.bc" -o "$@"
    """ % cl_src,
        visibility = ["//visibility:private"],
    )

def opencl_kernel_embedded(name, cl_src):
    """Compile ``cl_src`` to SPIR-V; embed SPIR-V bytes and optional CL text for linking.

    Use ``name`` as the stem shared by outputs and C symbols (no extension). Typical layout:
    ``cl_src = name + ".cl"`` (e.g. ``name = \"vector_add_kernel\"``, ``cl_src = \"vector_add_kernel.cl\"``).
    The OpenCL kernel entry in the ``.cl`` file should match ``name`` (e.g. ``__kernel void vector_add_kernel``).

    Generated targets:

    - ``:{name}_spv_embed`` — ``xxd`` of ``{name}.spv`` → ``{name}_spv[]``, ``{name}_spv_len``.
    - ``:{name}_blob_h`` — ``{name}_blob.h`` with ``struct {name}_blob`` (``spirv()`` / ``spirv_size_bytes()`` and
      ``cl_src()`` / ``cl_src_size_bytes()`` when ``cl_src`` is ``{name}.cl``).
    - ``:{name}_cl_embed`` — ``xxd`` of the ``.cl`` file → ``{name}_cl[]``, ``{name}_cl_len`` (link for CL fallback).

    Typical pattern: one ``cc_library`` with ``hdrs = [\":{name}_blob_h\"]`` and ``srcs`` listing your ``.cpp``,
    ``\":{name}_spv_embed\"``, and ``\":{name}_cl_embed\"``. Include as
    ``#include \"src/cpp_accelerator/cmd/<this_package>/{name}_blob.h\"`` (basename-only ``#include`` does not
    resolve with this repo's ``rules_cc`` / ``-iquote`` layout).

    Internal: ``:{name}_spv`` produces ``{name}.spv`` (OpenCL IL).
    """
    opencl_cl_to_spirv(
        name = name + "_spv",
        cl_src = cl_src,
        spirv = name + ".spv",
    )
    text_bytes_embed_cpp(
        name = name + "_cl_embed",
        text_src = cl_src,
        out_cpp = name + "_cl_embed.cpp",
    )
    spirv_bytes_embed_cpp(
        name = name,
        spv = ":" + name + "_spv",
        embed_cl = True,
    )
