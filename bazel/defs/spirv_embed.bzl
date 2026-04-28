"""SPIR-V helpers: GLSL ``.comp`` compile+embed, or embed an existing ``.spv`` (``xxd -i``).

Requires ``xxd`` on the host for embed steps (typical on Linux). GLSL compile uses ``glslc`` on ``PATH`` (Vulkan SDK / distro package).

For both embed paths, the file passed to ``xxd -i`` is named ``{name}.spv``, so symbols are
``unsigned char {name}_spv[]`` and ``unsigned int {name}_spv_len``.

Also emits ``{name}_blob.h`` (genrule) with ``struct {name}_blob`` (static ``spirv()`` / ``spirv_size_bytes()``;
optional ``cl_src()`` / ``cl_src_size_bytes()`` when ``embed_cl`` is set for OpenCL). On the same
``cc_library`` as your sources, set ``hdrs = [\":{name}_blob_h\"]`` and ``srcs = [\":{name}_spv_embed\"]``.
Include the header with a path under the output tree, e.g.
``#include \"src/cpp_accelerator/cmd/<pkg>/{name}_blob.h\"`` (matches ``-iquote $(GENDIR)`` from ``rules_cc`` here).
``name`` must be a valid C/C++ identifier (letters, digits, underscore).
"""

def _spirv_embed_blob_header(name, embed_cl = False):
    struct = "%s_blob" % name
    lines = [
        "#pragma once",
        "",
        "#include <cstddef>",
        "",
        "extern const unsigned char %s_spv[];" % name,
        "extern const unsigned int %s_spv_len;" % name,
    ]
    if embed_cl:
        lines.extend([
            "",
            "extern const unsigned char %s_cl[];" % name,
            "extern const unsigned int %s_cl_len;" % name,
        ])
    lines.extend([
        "",
        "struct %s {" % struct,
        "  %s() = delete;" % struct,
        "  static const unsigned char* spirv() { return %s_spv; }" % name,
        "  static std::size_t spirv_size_bytes() {",
        "    return static_cast<std::size_t>(%s_spv_len);" % name,
        "  }",
    ])
    if embed_cl:
        lines.extend([
            "  static const char* cl_src() {",
            "    return reinterpret_cast<const char*>(%s_cl);" % name,
            "  }",
            "  static std::size_t cl_src_size_bytes() {",
            "    return static_cast<std::size_t>(%s_cl_len);" % name,
            "  }",
        ])
    lines.append("};")
    body = "\n".join(lines)
    native.genrule(
        name = name + "_blob_h",
        outs = [name + "_blob.h"],
        cmd = "cat > $@ <<'SPIRV_IL_HDR_EOF'\n" + body + "\nSPIRV_IL_HDR_EOF",
        visibility = ["//visibility:private"],
    )

def spirv_shader_embed_cpp(name, shader):
    """Genrules: ``{name}_spirv`` -> ``{name}.spv``, ``{name}_spv_embed`` -> ``{name}_spv_embed.cpp``; plus ``{name}_blob.h``.

    Args:
      name: Base name (no extension). Must be a C++ identifier; matches ``struct {name}_blob`` and
            ``{name}_spv`` / ``{name}_spv_len`` from ``{name}.spv``.
      shader: Source label (e.g. ``\"my_shader.comp\"``).

    ``glslc`` must be on ``PATH`` during the build (e.g. Vulkan SDK).

    Link ``:{name}_spv_embed``; expose ``{name}_blob.h`` via a ``cc_library`` that lists ``:{name}_blob_h`` in ``hdrs``.
    """
    spir = name + "_spirv"
    embed = name + "_spv_embed"
    native.genrule(
        name = spir,
        srcs = [shader],
        outs = [name + ".spv"],
        cmd = "glslc -c $< -o $@",
        visibility = ["//visibility:private"],
    )
    native.genrule(
        name = embed,
        srcs = [":" + spir],
        outs = [name + "_spv_embed.cpp"],
        cmd = (
            "cp $(location :%s) %s.spv && xxd -i %s.spv > $@ && rm -f %s.spv" % (spir, name, name, name)
        ),
        visibility = ["//visibility:private"],
    )
    _spirv_embed_blob_header(name, embed_cl = False)

def spirv_bytes_embed_cpp(name, spv, embed_cl = False):
    """Embed an existing ``.spv`` as ``{name}_spv_embed.cpp`` and emit ``{name}_blob.h``.

    Uses ``xxd -i`` on a copy named ``{name}.spv``, producing the same symbols as
    ``spirv_shader_embed_cpp``: ``unsigned char {name}_spv[]`` and ``unsigned int {name}_spv_len``.

    Args:
      name: Base name (no extension); must be a C++ identifier.
      spv: Label of a rule that outputs a ``.spv`` file (e.g. ``\":my_kernel_spv\"``).
      embed_cl: If true, header also declares ``{name}_cl[]`` / ``{name}_cl_len`` (from ``text_bytes_embed_cpp``
        on ``{name}.cl``) and adds ``cl_src()`` / ``cl_src_size_bytes()`` — link ``:{name}_cl_embed`` too.

    Link ``:{name}_spv_embed``; expose ``{name}_blob.h`` via a ``cc_library`` that lists ``:{name}_blob_h`` in ``hdrs``.
    """
    embed = name + "_spv_embed"
    native.genrule(
        name = embed,
        srcs = [spv],
        outs = [name + "_spv_embed.cpp"],
        cmd = (
            "cp $(location %s) %s.spv && xxd -i %s.spv > $@ && rm -f %s.spv" % (spv, name, name, name)
        ),
        visibility = ["//visibility:private"],
    )
    _spirv_embed_blob_header(name, embed_cl = embed_cl)

def text_bytes_embed_cpp(name, text_src, out_cpp):
    """Embed a file as ``xxd -i`` output ``out_cpp`` (rule name ``name``).

    ``text_src`` is copied to its basename before ``xxd -i``, so C symbols match that basename
    (e.g. ``vector_add_kernel.cl`` → ``vector_add_kernel_cl[]`` / ``vector_add_kernel_cl_len``).
    """
    native.genrule(
        name = name,
        srcs = [text_src],
        outs = [out_cpp],
        cmd = (
            "cp $(location %s) %s && xxd -i %s > $@ && rm -f %s"
            % (text_src, text_src, text_src, text_src)
        ),
        visibility = ["//visibility:private"],
    )
