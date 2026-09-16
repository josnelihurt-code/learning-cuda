# UML Diagrams — cpp_accelerator

Mermaid class diagrams generated automatically from C++ source using [clang-uml](https://github.com/bkryza/clang-uml).

## Generated diagrams

| Diagram | File | Scope |
|---------|------|-------|
| Domain Layer | [cpp_domain_layer.mmd](generated/cpp_domain_layer.mmd) | `jrb::domain` — interfaces & models |
| Application Layer | [cpp_application_layer.mmd](generated/cpp_application_layer.mmd) | `jrb::application` — engine, pipeline, factories |
| Ports | [cpp_ports.mmd](generated/cpp_ports.mmd) | `jrb::ports` — hexagonal port interfaces |
| Core Utilities | [cpp_core.mmd](generated/cpp_core.mmd) | `jrb::core` — Logger, Telemetry, Result |
| Control & Media Adapters | [cpp_control_adapters.mmd](generated/cpp_control_adapters.mmd) | `jrb::adapters::grpc_control` + `jrb::adapters::webrtc` |
| Compute — CPU | [cpp_compute_cpu.mmd](generated/cpp_compute_cpu.mmd) | `jrb::adapters::compute::cpu` |
| Compute — CUDA | [cpp_compute_cuda.mmd](generated/cpp_compute_cuda.mmd) | `jrb::adapters::compute::cuda` + TensorRT |
| Compute — OpenCL | [cpp_compute_opencl.mmd](generated/cpp_compute_opencl.mmd) | `jrb::adapters::compute::opencl` |
| Compute — Vulkan | [cpp_compute_vulkan.mmd](generated/cpp_compute_vulkan.mmd) | `jrb::adapters::compute::vulkan` |

Diagrams are **not** committed to the repository — they must be regenerated locally from fresh source.

## One-time setup

Install clang-uml (Ubuntu 24.04):

```bash
scripts/dev/install-clang-uml.sh
```

Ensure `compile_commands.json` is up to date:

```bash
bazel run @hedron_compile_commands//:refresh_all
```

## Generating diagrams

```bash
# All diagrams
scripts/build/uml.sh

# Single diagram
scripts/build/uml.sh --diagram cpp_domain_layer

# Refresh compile_commands.json first, then generate
scripts/build/uml.sh --refresh-db
```

Output files land in `docs/uml/generated/` as `.mmd` files.

## Viewing diagrams

- **VS Code**: install the _Mermaid Preview_ extension (bierner.markdown-mermaid), then open any `.mmd` file
- **Online**: paste the file contents at <https://mermaid.live>
- **CLI render to SVG**: `mmdc -i docs/uml/generated/cpp_domain_layer.mmd -o /tmp/domain.svg`

## Configuration

Diagram scope and generation options are defined in [`.clang-uml`](../../.clang-uml) at the repository root.

Key settings:

| Setting | Value | Reason |
|---------|-------|--------|
| `compilation_database_dir` | `.` (repo root) | Points clang-uml at `compile_commands.json` |
| `generate_method_arguments` | `none` | Keeps diagrams readable |
| `include_relations_also_as_members` | `false` | Avoids redundant edges |
| `generate_packages` | `true` | Shows namespace hierarchy |
| `remove_compile_flags` | `-frandom-seed`, `-MD`, `-Wno-missing-requires` | Removes Bazel-specific flags clang-uml doesn't understand |

## Troubleshooting

**Parse errors for CUDA `.cu` files**  
Kernel source files are intentionally excluded from globs — only `.h` launcher headers are scanned to avoid device-code parse failures.

**Missing proto-generated types**  
Headers under `bazel-out/` are only available after a full build. Run `bazel build //src/cpp_accelerator/...` before generating diagrams if you see missing type errors.

**clang-uml version**  
Minimum supported: **0.4.0**. Check with `clang-uml --version`.
