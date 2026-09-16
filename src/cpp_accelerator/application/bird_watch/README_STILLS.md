# BirdWatcher

BirdWatcher runs YOLO inference on the IMX477 stream while the live WebRTC
feed keeps flowing and full-resolution stills get archived on detection. This
note documents how the camera pipeline is wired today, the regression that
broke the WebRTC feed after the high-resolution still feature landed, and the
fix that restored streaming without losing the new capabilities.

## Hardware constraints (Jetson Orin Nano 8GB)

| Fact | Detail |
|------|--------|
| Sensor | IMX477 over MIPI CSI-2 |
| Full-sensor mode | 4056x3040 @ 15 fps |
| Encoder hardware | None: `/dev/v4l2-nvenc` is absent on this SKU. We rely on `x264enc` (software). |
| Decoder hardware | NVDEC present (`/dev/nvdec0`), `nvv4l2decoder` available, currently unused. |
| VIC (Video Image Compositor) | Used by `nvvidconv`. Effectively free for resize/format conversion. |
| Argus session limit | One `ICaptureSession` per CSI port. Two `nvarguscamerasrc` instances on the same `sensor-id` will fail at the daemon level. |
| iGPU memory model | Unified DRAM with the CPU. NVMM buffers are physically reachable from both, but going from a CPU mmap to a CUDA device pointer still needs an explicit copy in our current code. |

## Goal of the architecture

1. **Stream** the camera at 720p over WebRTC for the React frontend.
2. **Capture stills** at the full IMX477 resolution (4056x3040, ~12.3 MP) on
   bird detection events.
3. **Run YOLO** (TensorRT) on the 720p stream without paying the cost of an
   extra `H.264 encode -> H.264 decode -> RGB` round trip.

These three consumers can't share one resolution, so the camera pipeline fans
out from the source via a `tee` with three branches.

## Current data flow

```
IMX477 sensor
   |
   v
nvarguscamerasrc sensor-id=0  (always 4056x3040 @ 15 fps NVMM NV12)
   |
   v
GStreamer tee (cam_tee)
   |
   +--> still_sink branch
   |       queue leaky=2 max-size-buffers=1
   |       nvvidconv  (NVMM -> system memory, stride normalized to width)
   |       video/x-raw,4056x3040,NV12
   |       appsink name=still_sink, emit-signals=false, max-buffers=1, drop=true
   |
   +--> raw_sink branch
   |       queue leaky=2 max-size-buffers=2
   |       nvvidconv  (4K NVMM -> 720p NVMM via VIC, NV12)
   |       video/x-raw(memory:NVMM),1280x720,NV12
   |       appsink name=raw_sink, emit-signals=true, max-buffers=2, drop=true
   |
   +--> stream_sink branch
           queue leaky=2 max-size-buffers=2
           nvvidconv  (4K NVMM -> 720p I420 in system memory)
           x264enc tune=zerolatency speed-preset=ultrafast
                   bitrate=2000 vbv-buf-capacity=400
                   intra-refresh=true key-int-max=60
           video/x-h264,profile=baseline
           h264parse config-interval=-1
           video/x-h264,stream-format=byte-stream,alignment=au
           appsink name=stream_sink, emit-signals=true, max-buffers=2, drop=true
```

### Per-branch consumers

| Branch | Consumer | Path | Cost when idle |
|--------|----------|------|----------------|
| `still_sink` | `BirdWatcher::SaveCapture` via `CameraHub::GrabStillFrame` | `gst_app_sink_try_pull_sample` (pull-on-demand) -> `gst_buffer_map` -> `sws_scale NV12 -> RGB24` -> `writeBmp` 4056x3040 | The leaky queue keeps a fresh frame around but nothing copies until something pulls. |
| `raw_sink` | `GpuFrameProcessor` -> `BirdWatcher::OnRgbaFrame` | `MapNvmmBuffer` -> `cudaMemcpy(H->D)` Y+UV -> CUDA NV12->RGBA -> `cudaMemcpy(D->H)` -> RGBA callback -> YOLO via TensorRT | If no `RgbCallback` is registered, `Process()` returns before mapping. The branch keeps running but pays no GPU/CPU cost. |
| `stream_sink` | `NvidiaArgusBackend::Impl::OnStreamSample` -> `CameraHub` fan-out -> WebRTC `LiveVideoProcessor` | One Annex-B AU per buffer, copied into `rtc::binary`, dispatched to subscribers | Always active while the WebRTC session has a sink. |

The high-resolution still capture is the `still_sink` branch end-to-end. Note
that the leaky queue + `max-size-buffers=1` + `drop=true` design means
`GrabStillFrame` returns the freshest frame the VIC produced, never an old
one stuck in a queue. At 15 fps the worst-case staleness is around 67 ms.

### Delegation chain for stills

```
BirdWatcher::SaveCapture
  -> CameraHub::GrabStillFrame(sensor_id, &w, &h)
  -> GstCameraSource::GrabStillFrame
  -> GstCameraSourceImpl::GrabStillFrame
  -> NvidiaArgusBackend::GrabStillFrame
  -> gst_app_sink_try_pull_sample(still_sink, 500ms timeout)
  -> gst_buffer_map -> rtc::binary (NV12 stride=width)
  -> sws_scale to RGB24 -> writeBmp at 4056x3040
```

### BirdWatcher GPU path

`BirdWatcher::ConnectGpuPath` calls
`CameraHub::GetGpuFrameProcessor(sensor_id)` and registers an `RgbCallback`
on it. From that point on, `GpuFrameProcessor::Process` (driven by the
`raw_sink` appsink callback) maps the NVMM buffer, runs the
`cuda_nv12_to_rgba_device` kernel, downloads RGBA to the host, and dispatches
to `BirdWatcher::OnRgbaFrame`. The H.264 decode path inside BirdWatcher is
torn down (`DestroyDecoder()`) so YOLO inference no longer has to round-trip
through libavcodec.

When the `RgbCallback` is cleared (e.g. BirdWatcher disabled per session),
`Process()` returns at the start without touching the buffer or the GPU.

## The regression and how it was found

A few minutes after PR #722 (Phase 1 high-res stills + Phase 2 GPU NV12
pipeline) shipped, the React frontend started showing horizontal-scanline
corruption on the live video. The container logs on the Jetson were full of:

```
[ffmpeg] no frame!
[WebRTC:...] Live camera frame processing failed:
   failed to submit H264 packet to decoder: Invalid data found when processing input
```

Failures were strictly alternating with successes:

```
onFrame fired (#1) size=8391  -> Frame #1 processed OK, 0 encoded units
onFrame fired (#2) size=14253 -> Invalid data found when processing input
onFrame fired (#3) size=17359 -> Frame #3 processed OK, 0 encoded units
onFrame fired (#4) size=18243 -> Invalid data found when processing input
onFrame fired (#5) size=7378  -> Frame #5 processed OK, 0 encoded units
```

Two things stood out:

1. The pattern was strictly even-frame failure / odd-frame success, not a
   data-quality or timing distribution.
2. Even on success, every frame produced **zero** encoded units. Nothing was
   being forwarded to the WebRTC track.

That signature is what an Annex-B parser running one frame behind looks like,
not what a corrupt bitstream looks like.

### Verification before changing code

Before touching anything, the encoder pipeline was reproduced standalone on
the Jetson with the running container temporarily stopped (Argus only allows
one capture session per sensor). Two captures were taken with `gst-launch-1.0`,
both 75 frames at 720p, using exactly the encoder settings the application
uses (`tune=zerolatency speed-preset=ultrafast intra-refresh=true
key-int-max=60`, `h264parse config-interval=-1`,
`stream-format=byte-stream,alignment=au`):

| File | ffprobe profile | Frames decoded | ffmpeg `-v error -f null -` |
|------|-----------------|----------------|------------------------------|
| `encode_old.h264` (NVMM -> I420 -> x264enc) | Constrained Baseline 1280x720 | 75/75 | silent |
| `encode_new.h264` (NVMM -> system NV12 -> I420 -> x264enc) | Constrained Baseline 1280x720 | 75/75 | silent |

Both files decoded cleanly with libavcodec. The bitstream was not the bug.

### Root cause

Commit `c954f11` (`fix: add H264 Annex B parser to LiveVideoProcessor decoder`)
inserted an `AVCodecParserContext` into `LiveVideoProcessor::ProcessAccessUnit`,
running every Annex-B access unit through `av_parser_parse2` before
`avcodec_send_packet`. The commit message claimed
`avcodec_send_packet` rejected raw Annex-B input. That diagnosis is wrong:
libavcodec's H.264 decoder accepts Annex-B byte-stream natively when the
buffer already contains a complete access unit, which is exactly what
`h264parse alignment=au` upstream guarantees.

`av_parser_parse2` is a *streaming* parser. It does not emit a frame until it
sees the start code of the *next* access unit. With one full AU per buffer
arriving on each call:

- Call N feeds AU<sub>N</sub>: parser buffers it, returns `parsed_size = 0`,
  nothing is sent. `0 encoded units` even when the loop reports success.
- Call N+1 feeds AU<sub>N+1</sub>: parser detects the AU boundary and emits
  the previous AU. With `intra-refresh=true` (no IDRs after frame 1) and
  `config-interval=-1` (SPS/PPS only emitted once at startup), the bytes the
  parser carved at the seam don't always line up with what
  `avcodec_send_packet` accepts. Half the frames produced
  `AVERROR_INVALIDDATA`.

Frames that did decode were P-frames whose reference frames had been dropped
by the parser stage, so the output was decodable but visually corrupt. That
is exactly what the screenshot showed.

The earlier `NvBufSurface` fixes from PR #722 (struct layout in the CI stub,
`cudaMemcpy` for the Y/UV planes, and the `NvBufSurfaceSyncForCpu` direction
fix) were correct on their own merits and were kept as-is. The corruption
was on the receive side, not the camera side.

## Architectural cleanup that came with the fix

PR #722 also added a separate `EncodePipeline` (`appsrc -> nvvidconv ->
x264enc -> appsink`) that ran in parallel with the camera pipeline. The flow
became:

```
NV12 NVMM (camera)
  -> NvBufSurfaceMap + cudaMemcpy(H->D)
  -> CUDA NV12->RGBA
  -> CUDA RGBA->NV12  (no filter in between)
  -> cudaMemcpy(D->H)
  -> EncodePipeline appsrc -> nvvidconv -> I420 -> x264enc -> H.264
  -> LiveVideoProcessor (ffmpeg decode -> filters -> ffmpeg encode)
  -> WebRTC track
```

For the streaming path that adds: one CPU mmap, four `cudaMemcpy` calls, two
CUDA conversion kernels, and a second GStreamer pipeline. The
`NV12 -> RGBA -> NV12` step is invertible and applies no filter for the
streaming consumer. It was bought purely to feed the BirdWatcher RGBA tap,
which is a separate consumer with its own callback.

The streaming path also uses `LiveVideoProcessor` regardless of whether any
filter is active, so the H.264 round-trip the new pipeline was supposed to
eliminate was still happening on every WebRTC session. The only place the
round-trip was actually saved was the BirdWatcher RGBA tap.

Conclusion: the GPU NV12 round-trip and `EncodePipeline` were dead weight on
the streaming path. They belonged in the BirdWatcher RGBA tap and nowhere
else.

## Steps taken to solve the issue

1. **Diagnosis**: read the running container logs, reproduced the encoder
   chain standalone on the Jetson, and confirmed the bitstream was clean
   while every other access unit was being rejected by the libavcodec
   decoder.
2. **Layer 1 - revert the parser**
   ([`live_video_processor.{h,cpp}`](../../adapters/webrtc)):
   - Removed the `AVCodecParserContext* parser_context_` member and its
     forward declaration.
     - Removed `av_parser_init` / `av_parser_close`.
   - `ProcessAccessUnit` now hands each AU directly to
     `avcodec_send_packet` (the pre-`c954f11` behavior).
3. **Layer 2 - architectural cleanup**:
   - [`gpu_frame_processor.{h,cpp}`](../../adapters/camera): dropped the
     H.264 callback and `EncodePipeline` ownership. The new API is
     `Start(width, height, error)` plus `SetRgbCallback(cb)`. `Process()`
     short-circuits at the top when no callback is registered, so the
     streaming path pays nothing while YOLO is idle. Kept only the
     `cuda_nv12_to_rgba_device` kernel; removed the `RGBA->NV12` round-trip,
     the host NV12 staging buffer, and the related scratch buffers.
   - [`backends/nvidia_argus_backend.cpp`](../../adapters/camera/backends):
     restructured the `tee` into three branches. `still_sink` is unchanged.
     `raw_sink` carries 720p NVMM NV12 to `GpuFrameProcessor` for the
     BirdWatcher tap. `stream_sink` runs `nvvidconv -> I420 -> x264enc ->
     h264parse -> appsink` inline (the same shape as the working
     pre-PR#722 pipeline) and emits one Annex-B AU per buffer through
     `OnStreamSample` -> `impl->cb` -> `CameraHub` fan-out -> WebRTC.
   - Deleted `adapters/camera/encode_pipeline.{h,cpp}` and the matching
     `cc_library` from `adapters/camera/BUILD`. The separate appsrc-driven
     encode pipeline is no longer used.
4. **Version bump**: `4.6.9 -> 4.7.0`.

Net result: `+227 / -558` lines across nine files; ~330 lines removed.

## Behavior matrix after the fix

| Capability | Status |
|------------|--------|
| WebRTC streaming | Restored. `Frame #N processed OK` now produces one encoded unit per AU. No more `Invalid data found when processing input` errors. |
| 4056x3040 still capture | Unchanged. Same `still_sink` branch, same `GrabStillFrame` chain. |
| BirdWatcher YOLO via GPU RGBA tap | Preserved. `gpu_processor->SetRgbCallback(...)` activates the `cuda_nv12_to_rgba_device` path on demand. |
| Streaming overhead while YOLO is idle | Reduced. No more invertible NV12<->RGBA round-trip and no more parallel `EncodePipeline`. |

## Known follow-ups (not done)

- **Forward AUs without re-encode when the WebRTC session has no active
  filter.** `LiveVideoProcessor::ProcessAccessUnit` currently always decodes
  to RGB and re-encodes, even when `HasActiveFilters(state)` is false. For
  unfiltered sessions the pipeline could pass the inbound AU straight to
  the outbound track. This was discussed but explicitly deferred.
- **Real zero-copy NVMM->CUDA on the BirdWatcher tap.** Today
  `GpuFrameProcessor::Process` does `NvBufSurfaceMap` + `cudaMemcpy(H->D)`.
  On Jetson iGPU with unified DRAM, `NvBufSurfaceMapEglImage` plus
  `cuGraphicsEGLRegisterImage` (or allocating with
  `NVBUF_MEM_CUDA_UNIFIED`) would let CUDA read the pixel data without any
  copy. Worth doing if YOLO frame rate becomes a constraint.
- **Recovery from packet loss.** `intra-refresh=true` plus
  `config-interval=-1` means SPS/PPS are emitted exactly once at startup
  and there are no IDRs afterwards. Any WebRTC client that joins late or
  drops a packet has no way to resync. Switching to `key-int-max=30` (one
  IDR per second) and `config-interval=1` would make the stream resilient
  to packet loss and to mid-stream client joins.

## Reference - relevant files

| File | Role |
|------|------|
| `adapters/camera/backends/nvidia_argus_backend.{h,cpp}` | `tee` pipeline, three appsink callbacks, `GrabStillFrame` |
| `adapters/camera/gpu_frame_processor.{h,cpp}` | NV12 NVMM -> RGBA tap, idle when no `RgbCallback` |
| `adapters/camera/nvbuf_cuda_utils.{h,cpp}` | `NvBufSurface` mapping wrapper |
| `adapters/compute/cuda/kernels/nv12_utils_kernel.{h,cu}` | `cuda_nv12_to_rgba_device`, `cuda_nv12_letterbox_device` |
| `adapters/camera/camera_hub.{h,cpp}` | Subscriber fan-out, `GrabStillFrame` and `GetGpuFrameProcessor` delegation |
| `adapters/webrtc/live_video_processor.{h,cpp}` | WebRTC-side decode/filter/re-encode for every streaming session (see "Known follow-ups") |
| `application/bird_watch/bird_watcher.{h,cpp}` | Detection loop, queue management, GPU vs H.264 path selection |
| `application/bird_watch/bird_watcher_gpu_argus.cpp` | `ConnectGpuPath` / `DisconnectGpuPath` wiring on Argus |
| `third_party/nvbufsurface/nvbufsurface.h` | CI-only stub matching the JetPack 6 / L4T R36 layout. The real header at `/usr/src/jetson_multimedia_api/include/nvbufsurface.h` and `libnvbuf_utils.so` are mounted into the container at runtime. |
