# Jetson Camera Backends (Argus/V4L2) - End-to-End Guide

This document explains how camera detection and camera streaming work in this project, with focus on Jetson Nano Orin production deployment.

It captures the implementation details and operational fixes applied during debugging of:

- cameras detected but stream not starting
- GStreamer plugin/library missing errors in container
- Argus daemon socket/device visibility issues in Docker

---

## 1) Quick Mental Model

There are two independent phases:

1. **Detection phase** (`ListCameras`)
   - Probes available camera backends.
   - Returns `RemoteCameraInfo` list (sensor IDs, display names, modes).
2. **Streaming phase** (`StartCameraStream`)
   - Starts live capture pipeline for selected camera.
   - Encodes H.264 and pushes frames to WebRTC track.

A system can pass detection but still fail streaming (different pipeline, different dependencies).

---

## 2) Backend Architecture in This Codebase

Main files:

- `camera_backend.h` - common backend interface
- `nvidia_argus_backend.cpp` - Jetson CSI backend (`nvarguscamerasrc`)
- `v4l2_backend.cpp` - V4L2/USB backend
- `stub_backend.cpp` - fallback no-op backend

Build wiring:

- `src/cpp_accelerator/adapters/camera/backends/BUILD`
- compile-time flags:
  - `--config=nvidia-argus-camera`
  - `--config=v4l2-camera`

Jetson production build currently enables Argus through:

- `src/cpp_accelerator/Dockerfile.build`
- ARM path uses `CAMERA_FLAGS="--config=nvidia-argus-camera"`

---

## 3) Current Jetson Argus Stream Pipeline

Since the high-resolution stills refactor (#722), `NvidiaArgusBackend::Start(...)`
builds a single pipeline — no encoder probing, no NVENC path. The sensor always
captures at full resolution (`4056x3040 @ 15 fps`, NVMM NV12) and a `tee` fans
out to three consumers that each need a different resolution:

```text
nvarguscamerasrc sensor-id=<id> wbmode=<mode>     (always 4056x3040 @ 15 fps)
   !
 tee name=cam_tee
   |
   +--> still_sink branch    full-res NV12 in system memory -> still captures
   |       queue leaky=2 max-size-buffers=1 ! nvvidconv !
   |       video/x-raw,4056x3040,NV12 !
   |       appsink name=still_sink emit-signals=false max-buffers=1 drop=true
   |
   +--> raw_sink branch      encode-res NVMM NV12 -> GPU frame processor (YOLO)
   |       queue leaky=2 max-size-buffers=2 ! nvvidconv !
   |       video/x-raw(memory:NVMM),<encode_w>x<encode_h>,NV12 !
   |       appsink name=raw_sink emit-signals=true max-buffers=2 drop=true
   |
   +--> stream_sink branch   encode-res I420 -> x264enc -> h264parse -> WebRTC
           queue leaky=2 max-size-buffers=2 ! nvvidconv !
           video/x-raw,I420,<encode_w>x<encode_h> !
           x264enc tune=zerolatency speed-preset=ultrafast bitrate=2000
                   vbv-buf-capacity=400 intra-refresh=true key-int-max=60 !
           video/x-h264,profile=baseline !
           h264parse config-interval=-1 !
           video/x-h264,stream-format=byte-stream,alignment=au !
           appsink name=stream_sink emit-signals=true max-buffers=2 drop=true
```

Encoding is always software `x264enc` (Orin Nano has no NVENC — §13.1; the
encoder probe this section used to describe was removed by the #722 refactor).

Important runtime behavior implemented:

- Output is pinned to **Annex-B / AU-aligned** H.264 (`stream-format=byte-stream,
  alignment=au`). The downstream consumer is `live_video_processor` which
  feeds bytes to libavcodec; libavcodec's H.264 decoder rejects AVC framing,
  and `h264parse` will silently negotiate to AVC if downstream caps don't pin
  byte-stream. See §13 for the failure signature.
- Software path uses **rolling intra-refresh** instead of periodic full
  I-frames. This avoids large keyframe bursts that overrun the WebRTC
  outbound queue and cause partial-frame artifacts. See §13.
- `wbmode` defaults to `4` (warm-fluorescent) and is overridable at runtime
  via the `NVARGUS_WBMODE` env var (1=auto, 2=incandescent, 3=fluorescent,
  4=warm-fluorescent, 5=daylight, 6=cloudy-daylight).
- If a request arrives as invalid `0x0@0fps`, the backend normalizes to
  `1280x720` and ignores the requested fps — the sensor always drives the
  clock at 15 fps. The request's width/height set the `raw_sink`/`stream_sink`
  (encode) resolution only; the source still captures full-sensor.
- `h264parse` is optional at parse time; if missing the pipeline still
  builds without parser (with warning log) — but byte-stream pinning is
  lost in that case.

---

## 4) Required Docker Runtime Dependencies (Jetson)

Installed in `src/cpp_accelerator/docker-cuda-runtime/Dockerfile`:

- `gstreamer1.0-tools`
- `gstreamer1.0-plugins-base`
- `gstreamer1.0-plugins-bad`
- `gstreamer1.0-plugins-good`
- `gstreamer1.0-plugins-ugly`
- `libgstreamer1.0-0`
- `libgstreamer-plugins-base1.0-0`
- `libegl1`
- `libgles2`
- `kmod`

Why each mattered in incidents:

- missing `libEGL.so.1` -> NVIDIA GStreamer plugins failed to load
- missing `libGLESv2.so.2` -> `nvarguscamerasrc` plugin load failures
- missing `h264parse` -> streaming pipeline parse error (`no element "h264parse"`)
- missing `kmod` tools -> noisy `lsmod/modprobe` warnings

Build-time dependencies (for compiling camera backends) live in:

- `src/cpp_accelerator/docker-cpp-dependencies/Dockerfile`
- includes:
  - `libgstreamer1.0-dev`
  - `libgstreamer-plugins-base1.0-dev`

---

## 5) Required Jetson Docker Mounts/Devices

`src/cpp_accelerator/docker-compose.yml` now maps required resources.

### Socket mount

- `/tmp/argus_socket:/tmp/argus_socket`

Without it, Argus daemon connection fails with:

- `Connecting to nvargus-daemon failed: No such file or directory`

### Device mappings

CSI + media graph + Tegra nodes:

- `/dev/video0`, `/dev/video1`
- `/dev/v4l-subdev0..3`
- `/dev/media0`
- `/dev/nvhost-ctrl-gpu`
- `/dev/nvhost-ctrl-isp`, `-isp-thi`
- `/dev/nvhost-ctrl-nvcsi`
- `/dev/nvhost-ctrl-vi0`, `-vi0-thi`
- `/dev/nvhost-ctrl-vi1`, `-vi1-thi`
- `/dev/nvhost-nvsched_ctrl_fifo-gpu`
- `/dev/nvmap`
- `/dev/nvgpu`

Without these, you can get:

- detection success but stream session creation failure
- `Failed to create CaptureSession`

---

## 6) Why Detection May Work But Streaming Fails

This happened repeatedly during debugging and is expected if runtime is incomplete.

Examples:

1. **Detection succeeded**:
   - `ProbeSensor(...)` with a minimal pipeline passed.
2. **Streaming failed**:
   - production pipeline had stricter caps/encoder/parser requirements
   - invalid request dimensions/fps (`0x0@0fps`)
   - missing element (`h264parse`)
   - missing mount/device for Argus session

Takeaway: always validate both phases independently.

---

## 7) Operational Diagnostics Checklist

Run on Jetson host:

```bash
ls -l /tmp/argus_socket
ls /dev/video* /dev/v4l-subdev* /dev/media*
```

Run inside container:

```bash
ls -l /tmp/argus_socket
gst-inspect-1.0 nvarguscamerasrc
gst-inspect-1.0 nvv4l2h264enc
gst-inspect-1.0 h264parse
```

Test minimal Argus probe:

```bash
gst-launch-1.0 -q nvarguscamerasrc sensor-id=0 num-buffers=1 ! fakesink
```

Test stream pipeline shape:

```bash
gst-launch-1.0 nvarguscamerasrc sensor-id=0 ! \
  'video/x-raw(memory:NVMM),width=1920,height=1080,framerate=30/1,format=NV12' ! \
  nvvidconv ! nvv4l2h264enc insert-sps-pps=true bitrate=2000000 ! \
  h264parse config-interval=-1 ! fakesink -e
```

Application-side logs to watch:

- `/tmp/cppaccelerator.log` (inside accelerator container)
- `cuda-go-server` logs for:
  - `ListCameras` camera count
  - session lifecycle

---

## 8) Debug Timeline of Real Failures and Fixes

1. Missing `/dev/nvhost-ctrl` style mappings from compose
   - fixed by mapping actual device set present on target Jetson

2. `libgstreamer-1.0.so.0` missing
   - fixed by runtime package install

3. `libEGL.so.1` missing
   - fixed by `libegl1`

4. `libGLESv2.so.2` missing
   - fixed by `libgles2`

5. Argus daemon socket unavailable
   - fixed with `/tmp/argus_socket` bind mount

6. Argus capture session failures due to missing media/subdev nodes
   - fixed with `v4l-subdev*`, `media0`, plus additional Tegra node mappings

7. `StartCameraStream` received `0x0@0fps`
   - fixed with parameter normalization in Argus backend

8. `h264parse` element absent in runtime image
   - fixed by adding `gstreamer1.0-plugins-bad`
   - backend also made resilient if parser is absent

---

## 9) Zero-to-Expert Learning Path (Jetson + GStreamer)

### Level 0 - Basic Concepts

- GStreamer pipeline = graph of elements connected with `!`
- source (`nvarguscamerasrc`) -> transform (`nvvidconv`) -> encoder (`nvv4l2h264enc`) -> sink (`appsink`)
- each element may come from a different plugin package

### Level 1 - Jetson Camera Basics

- CSI cameras on Jetson typically use Argus stack (`nvarguscamerasrc`)
- Argus requires daemon communication (`/tmp/argus_socket`)
- Docker isolation means you must pass socket and required `/dev/*` nodes explicitly

### Level 2 - Packaging and Runtime Contracts

- compile-time success does not guarantee runtime success
- verify both:
  - headers/libs at build time
  - runtime `.so` and plugin elements in final image
- use `gst-inspect-1.0 <element>` as first-line validation

### Level 3 - Production Reliability

- log backend selection and backend-specific failures
- normalize invalid client parameters to known-safe defaults
- prefer explicit, actionable logs over generic "failed to start stream"
- keep compose device list close to validated target hardware

### Level 4 - Performance/Quality Tuning

- tune `nvv4l2h264enc` (bitrate, profile, rate control, IDR interval)
- evaluate parser necessity for downstream packetizer contract
- profile frame flow and packetization behavior in WebRTC path

---

## 10) Authoritative External References

### NVIDIA Jetson Accelerated GStreamer

- NVIDIA Jetson Linux Developer Guide - Accelerated GStreamer  
  https://docs.nvidia.com/jetson/archives/r38.4/DeveloperGuide/SD/Multimedia/AcceleratedGstreamer.html

This doc includes:

- Jetson accelerated plugin usage (`nvarguscamerasrc`, `nvv4l2h264enc`)
- installation guidance (including `nvidia-l4t-gstreamer`)
- example pipelines with `h264parse`

### GStreamer Plugin Taxonomy and h264parse

- GStreamer "bad" plugins module page  
  https://gstreamer.freedesktop.org/modules/gst-plugins-bad.html
- h264parse plugin documentation  
  https://gstreamer.freedesktop.org/documentation/videoparsersbad/h264parse.html
- gst-plugins-bad source repository  
  https://github.com/GStreamer/gst-plugins-bad

Note: freedesktop pages may block automated fetch in some environments, but those are the official upstream docs.

### Community Jetson-in-Docker Argus Context

- NVIDIA Developer Forums (Argus in Docker discussion)  
  https://forums.developer.nvidia.com/t/using-nvarguscamerasrc-within-a-docker-container/328246

Treat forum threads as practical guidance, not canonical specification.

---

## 11) Practical Runbook (Production Jetson)

1. Deploy latest compose and image.
2. Verify socket/devices mounted in container.
3. Verify required elements:
   - `nvarguscamerasrc`
   - `nvv4l2h264enc`
   - `h264parse` (or confirm backend fallback warning if absent)
4. Trigger `ListCameras`:
   - expect `camera_count > 0`
5. Trigger `StartCameraStream`:
   - ensure logs do not show `0x0@0fps` unhandled
   - ensure Argus pipeline enters PLAYING
6. If stream still black:
   - inspect WebRTC stats/channel logs
   - verify encoded frames are emitted from appsink callback

---

## 12) Key Rule for This Project

For Jetson production camera path, keep Argus explicit and fail loudly on real backend issues.

Do not hide Argus failures behind compile-time backend fallbacks that change operational behavior silently.

---

## 13) Postmortem — Orin Nano + Mixed CSI Modules Incident

This section documents a multi-issue debugging session against an Orin Nano
Super Dev Kit running JetPack R36.4.4, with one IMX477 (cam 0) and one
IMX519 (cam 1) module attached. Each numbered subsection is one independent
failure mode and its resolution. The order matches the order in which each
became visible — fixing one exposed the next.

### 13.1) `Cannot identify device '/dev/v4l2-nvenc'` — Orin Nano has no NVENC

**Symptom**

```
[NvidiaArgusBackend] Failed to set pipeline to PLAYING state:
  Cannot identify device '/dev/v4l2-nvenc'.
  /dvs/.../v4l2_calls.c(657): gst_v4l2_open ():
  /GstPipeline:.../nvv4l2h264enc:nvv4l2h264enc0:
  system error: No such file or directory
```

**Root cause**

Jetson **Orin Nano has no NVENC hardware encoder** — only NVDEC. The
`nvv4l2h264enc` GStreamer plugin is still registered in the L4T container
image (because the package ships the plugin unconditionally), but the
underlying kernel device node `/dev/v4l2-nvenc` is never created on this
SKU. Plugin probe alone is insufficient — it returns true on Orin Nano even
though the encoder cannot work. Confirm with:

```bash
ls /dev/v4l2-nvenc          # absent on Orin Nano
gst-inspect-1.0 nvv4l2h264enc   # present on host AND container
```

Encoder-bearing SKUs (Orin NX, AGX Orin) have the device node. Orin Nano
does not.

**Fix**

Probe both the plugin **and** the device node, and fall back to software
`x264enc` when either is missing:

```cpp
const bool nvenc_plugin = HasGstElement("nvv4l2h264enc");
const bool nvenc_device = (access("/dev/v4l2-nvenc", F_OK) == 0);
const bool has_nvenc = nvenc_plugin && nvenc_device;
```

Software encode at 1080p30 with `x264enc tune=zerolatency speed-preset=
ultrafast` runs at ~15% total CPU on Orin Nano (measured 12-21% across all
6 cores at idle clocks via `tegrastats`), with no thermal climb at room
temperature. Plenty of headroom for the rest of the WebRTC + CUDA pipeline.

**Superseded**: the high-resolution stills refactor (#722) removed this probe
and the NVENC path entirely — `x264enc` is now the unconditional encoder and
the backend no longer references `nvv4l2h264enc` (see §3).

### 13.2) `Invalid NAL unit 0` flood — h264parse picked AVC framing

**Symptom (after the §13.1 fix deployed)**

Pipeline reaches PLAYING. Argus produces frames. WebRTC connects. Container
log floods with libavcodec errors:

```
[h264 @ 0xfffe...] No start code is found.
[h264 @ 0xfffe...] Error splitting the input into NAL units.
[h264 @ 0xfffe...] Invalid NAL unit 0, skipping.
[h264 @ 0xfffe...] sps_id 24 out of range
[h264 @ 0xfffe...] data partitioning is not implemented...
[h264 @ 0xfffe...] slice type 15 too large at 1
```

No video reaches the browser.

**Root cause**

The downstream consumer is `adapters/webrtc/live_video_processor.cpp`,
which feeds raw appsink bytes directly to libavcodec
(`avcodec_send_packet`). libavcodec's H.264 decoder defaults to expecting
**Annex-B byte-stream** (NALs separated by `00 00 00 01` start codes).

`h264parse` does not have a hardcoded output framing — it negotiates with
its downstream pad. With the previous `nvv4l2h264enc → h264parse → appsink`
chain, negotiation happened to settle on byte-stream. With the new
`x264enc → h264parse → appsink` chain, h264parse settled on **AVC**
(4-byte length-prefixed NALs, the format used inside `.mp4` boxes).
Verified empirically by capturing a frame:

| build         | first 16 bytes                                  | meaning                  |
|---------------|-------------------------------------------------|--------------------------|
| broken        | `00 00 00 02 09 10 ... 00 00 00 1c 67 42 ...`   | length-prefixed (AVC)    |
| fixed         | `00 00 00 01 09 10 ... 00 00 00 01 67 42 ...`   | start codes (byte-stream)|

Length prefixes happen to look like all-zero NAL headers to a byte-stream
decoder, hence the `Invalid NAL unit 0` / `slice type 15 too large` /
`sps_id out of range` symptoms — the decoder is interpreting random
mid-NAL bytes as new NAL headers.

**Fix**

Pin the framing explicitly with a caps filter immediately after h264parse:

```text
h264parse config-interval=-1 !
video/x-h264,stream-format=byte-stream,alignment=au !
appsink ...
```

This is required regardless of which encoder feeds the parser. Applied to
both the hardware and software branches so behavior doesn't depend on
encoder negotiation luck.

### 13.3) Cam 0 shows only the bottom 5% of the frame, blinking

**Symptom (after the §13.2 fix deployed)**

Cam 1 streams cleanly. Cam 0 shows a thin band of real image at the bottom
of the frame, with the upper 95% replaced by jittery garbage that flickers
each frame.

**Root cause** (encoder hypothesis — turned out to be partially right but
not the actual final cause; see §13.4 for the real one. The encoder
hardening below is still worth keeping.)

A WebRTC viewer that has not yet received a valid I-frame can only render
the per-frame *delta* of subsequent P-frames against a missing reference.
The visible result is exactly a thin band of correct pixels in the area
where the P-frame happens to encode few changes, with everything else
garbage that flickers each frame.

`x264enc` at default settings emits a full I-frame every `key-int-max`
frames. At 1080p30 a single I-frame can be 50-100 KB. RTP-fragmented over
WebRTC, any lost fragment makes the entire keyframe undecodable, and the
viewer is stuck on P-frames-only until the next keyframe attempt — which
also gets dropped, indefinitely.

`tegrastats` showed steady CPU but the libdatachannel log showed:

```
rtc::impl::LogCounter: Number of media packets dropped due to a full queue: 10/sec
```

confirming outbound-side drops every second.

**Fix**

Replace periodic full keyframes with **rolling intra-refresh** (one column
of macroblocks coded as intra per frame, completing a full refresh over
N frames). No frame ever spikes huge. Validated empirically — frame size
profile from a 120-frame test:

| frame # | bytes |
|---------|-------|
| 0       | 7,586 (initial keyframe + IR start) |
| 1-119   | 492-1,045 (uniform)                 |

Compare to the prior config which would have a single ~50-100 KB I-frame
every 30 frames. Encoder change:

```text
x264enc tune=zerolatency speed-preset=ultrafast
        bitrate=2000 vbv-buf-capacity=400
        intra-refresh=true key-int-max=60
```

### 13.4) Cam 0 still horizontal-stripe garbage — wrong sensor driver bound

**Symptom (after §13.3 fix)**

Cam 1 streams perfectly. Cam 0 still produces dense horizontal stripes
covering most of the frame, with only the bottom rows ever showing
anything resembling the scene. Looks like line-stride misalignment, not
keyframe loss.

**Root cause**

Found in `dmesg`:

```
imx519 9-001a:  tegracam sensor driver:imx519_v2.0.6
imx519 10-001a: tegracam sensor driver:imx519_v2.0.6
imx519 10-001a: imx519_board_setup: invalid sensor model id: 477
tegra-camrtc-capture-vi tegra-capture-vi: subdev imx519 10-001a bound
```

The IMX519 kernel driver is being bound to **both** CSI ports, including
the one with the IMX477 module. Driver reads the sensor's model-id
register, gets `477` back (IMX477's ID), logs the mismatch as a warning,
**and binds anyway**. The IMX477 sensor is then driven with IMX519 register
sequences and timing — wrong line stride, wrong pixel format negotiation,
wrong PLL config. Output is byte garbage that decodes to the stripe pattern
seen on screen. Confirmed via `/sys/class/video4linux/v4l-subdev*/name`:
both subdevs reported `imx519`.

The booted device-tree overlay on this Jetson came from `extlinux.conf`:

```
LABEL JetsonIO
    MENU LABEL Custom Header Config: <CSI Camera IMX519 Dual>
    OVERLAYS .../tegra234-p3767-camera-p3768-imx519-dual.dtbo
```

i.e. the overlay declares **both** CSI ports as IMX519. The IMX477 driver
was never even loaded — no module match, hence no proper bind to fall back
to.

**Fix**

The Arducam/NVIDIA dev-kit ships overlays for IMX477-dual, IMX519-dual,
and a few mixed combos (IMX477+IMX219), but **no IMX477+IMX519 mixed
overlay**. To run mixed sensor types you would need to author a custom
DTBO. For this deployment we picked the cheaper path:

1. Decided to keep cam 0 (IMX477) and disable cam 1.
2. Switched the boot overlay to `imx477-dual` via `extlinux.conf`:
   ```bash
   sudo sed -i 's|imx519-dual.dtbo|imx477-dual.dtbo|' /boot/extlinux/extlinux.conf
   sudo reboot
   ```
3. Reinstalled the IMX477 driver bundle from the Arducam package
   (`./install_full.sh -m imx477`), which also drops the IMX477-specific
   `camera_overrides.isp` into `/var/nvidia/nvcam/settings/`.

After reboot, `dmesg` showed `imx477` bound to the IMX477 port and stripe
garbage was gone.

### 13.5) Cyan/blue color cast on cam 0 — bad ISP override file

**Symptom (after §13.4 fix)**

Geometry now correct. Image clean but with a strong cyan/blue-green cast.
White objects appear cyan; warm tones appear desaturated. Setting
`NVARGUS_WBMODE=6` (cloudy-daylight, 6500K target) only marginally
improves it.

**Root cause**

The active `/var/nvidia/nvcam/settings/camera_overrides.isp` was verified
byte-identical to the file shipped inside Arducam's IMX477 .deb (md5
matched) — i.e. it is the correct file for this sensor, not a stale leftover
from the IMX519 install. The Arducam stock IMX477 ISP tuning simply has a
biased Color Correction Matrix (CCM) that produces a cool cast on
mixed-light scenes. AWB mode tweaks (`wbmode=N`) only adjust the
*temperature target*; the CCM bias is independent and persists across all
modes.

The `.isp` file is a compiled NVIDIA Char-lite calibration blob (LSC, CCM,
optical-black, AWB target, gamma combined). It is **not editable as a
text WB file**. Producing a corrected one requires the full NVIDIA
calibration toolchain (Macbeth chart, sensor RAW captures, Char-lite tool).

**Fix**

Bypass the Arducam override entirely and let `nvargus-daemon` use NVIDIA's
built-in IMX477 tuning. NVIDIA ships per-sensor default tuning compiled
into the daemon; it activates when no override file is present.

```bash
sudo mv /var/nvidia/nvcam/settings/camera_overrides.isp \
        /var/nvidia/nvcam/settings/camera_overrides.isp.arducam-bak
sudo systemctl restart nvargus-daemon
docker restart cuda-accelerator-client
```

Colors corrected to neutral. The Arducam override is preserved as `.bak`
in case it's needed for a different lighting environment in the future.

### 13.6) Log noise from libdatachannel + per-frame info logs

**Symptom**

Container log was dominated by:

```
rtc::impl::IceTransport::LogCallback: juice: agent.c:...
rtc::impl::PeerConnection::changeSignalingState: ...
rtc::impl::LogCounter: Number of media packets dropped due to a full queue: 10/sec
[WebRTC:<sid>] onFrame fired (#N) size=... processor=true outbound=true open=true
[WebRTC:<sid>] Frame #N processed OK, 1 encoded units
```

— hundreds of lines per second, drowning real errors.

**Fix**

Two adjustments in `adapters/webrtc/webrtc_manager.cpp`:

1. Lowered libdatachannel log threshold from `Info` → `Error`. Only real
   failures surface; the chatty ICE/DTLS/SCTP/queue-counter messages stay
   silent.
2. Per-frame info logs (`onFrame fired`, `Frame #N processed OK`) demoted
   from `info` to `SPDLOG_TRACE` after the first 5 frames per session.
   First 5 still log at `info` so a session start is observable.

The "media packets dropped" warning was a real symptom (encoder bursts
overrunning the outbound queue) and was addressed at the source by
§13.3's intra-refresh change. After that fix, even with debug logging
re-enabled, the warning stops appearing.

---

## 14) Operational Cheat Sheet (Jetson Orin Nano, This Deployment)

Quick reference for re-deploying this stack from scratch.

### 14.1) Verify hardware capability matrix on first deploy

```bash
# Sensor binding (host)
sudo dmesg | grep -iE 'imx|tegracam'
ls -l /sys/class/video4linux/v4l-subdev*/name
xargs -a <(ls /sys/class/video4linux/v4l-subdev*/name) cat

# Encoder presence
ls /dev/v4l2-nvenc 2>&1   # absent on Orin Nano (expected)
ls /dev/v4l2-nvdec        # present (decoder is independent)
gst-inspect-1.0 nvv4l2h264enc | head -1   # plugin registered? yes
gst-inspect-1.0 x264enc | head -1         # software fallback present?
```

If sensor binding is wrong (e.g. IMX519 driver bound to IMX477 port),
fix the boot overlay in `/boot/extlinux/extlinux.conf` and reboot before
proceeding.

### 14.2) Validate the pipeline shape end-to-end before deploying the C++

Without the live container running, sanity-check that the GStreamer chain
the C++ will build actually works:

```bash
# Software-encode + Annex-B caps + decode round-trip
gst-launch-1.0 videotestsrc num-buffers=120 pattern=ball ! \
  video/x-raw,format=I420,width=1920,height=1080,framerate=30/1 ! \
  x264enc tune=zerolatency speed-preset=ultrafast bitrate=2000 \
          vbv-buf-capacity=400 intra-refresh=true key-int-max=60 ! \
  video/x-h264,profile=baseline ! \
  h264parse config-interval=-1 ! \
  'video/x-h264,stream-format=byte-stream,alignment=au' ! \
  filesink location=/tmp/out.h264
od -An -v -tx1 -N16 /tmp/out.h264   # must start 00 00 00 01
ffmpeg -i /tmp/out.h264 -f null -   # must decode without warnings
```

### 14.3) Tunable runtime knobs (no rebuild required)

| env var                | effect                                                              |
|------------------------|---------------------------------------------------------------------|
| `NVARGUS_WBMODE`       | `nvarguscamerasrc` white-balance preset (1-6); default 4            |
| `ACCELERATOR_IMAGE`    | full image reference for `docker compose`                           |
| `ACCELERATOR_DEVICE_ID`/`_DISPLAY_NAME` | identity registered with the control plane          |

`scripts/dev/jetson-deploy.sh` persists the first two in `.env` so they
survive subsequent `docker compose` invocations.

### 14.4) Switching back to a different sensor configuration

If a future module swap restores cam 1, or you switch to dual-IMX477:

1. Pick the matching overlay from `/boot/` (e.g.
   `tegra234-p3767-camera-p3768-imx477-dual.dtbo`,
   `imx477-imx219.dtbo`, etc.).
2. Update the `OVERLAYS` line in `/boot/extlinux/extlinux.conf`.
3. Reinstall the matching driver bundle: `./install_full.sh -m <sensor>`
   from the Arducam package directory.
4. Reboot.
5. Verify `/sys/class/video4linux/v4l-subdev*/name` reports the expected
   sensors, then redeploy the container.

There is **no shipped overlay for IMX477 + IMX519 simultaneously** — only
matched-pair or IMX477+IMX219 combinations. Mixing IMX477 and IMX519 on
the dev kit requires authoring a custom DTBO.
