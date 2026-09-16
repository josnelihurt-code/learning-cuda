# Video Streaming Optimization

> **Note: This file is archived for historical reference.**  
> All backlog items in this file have been migrated to GitHub Issues as part of the project's evolution from markdown-based backlog management to structured issue tracking. Each item was carefully analyzed, grouped with related tasks, and converted into actionable GitHub issues with proper labels, acceptance criteria, and context.
> 
> **Purpose**: This file is preserved to document the initial planning and evolution of video streaming optimization research. It serves as a historical record of how different optimization approaches (Connect-RPC, binary transport, hardware encoding, WebRTC) were identified and organized for exploration.
>
> **Current Status**: All pending items have been converted to GitHub Issues (#507-511). Items marked as "Completed" remain for historical reference. POC items have been converted to research and implementation tickets.
>
> **See**: [GitHub Issues](https://github.com/josnelihurt-code/learning-cuda/issues) for active project management.

Research and POC tasks for improving video transport, originally written when the transport was WebSocket + base64 PNG. The WebSocket transport has since been removed entirely; the current transport is WebRTC (`WebRTCSignalingService` in `proto/webrtc_signal.proto`).

## Implementation Analysis (historical)

**Status at the time of writing**: Working but inefficient
- Transport: WebSocket with base64-encoded PNG (former implementation, since removed — current transport is WebRTC via `WebRTCSignalingService` in `proto/webrtc_signal.proto`)
- Overhead: ~33% from base64 encoding + PNG compression CPU cost
- Latency: Acceptable for learning, not production-ready
- Bandwidth: ~2-5 MB/s for 720p @ 30 FPS

**Bottlenecks**:
1. PNG encoding on every frame (CPU-intensive)
2. Base64 encoding overhead
3. No video compression (each frame independent)
4. No adaptive bitrate

## Research Phase: Create POCs

### POC 1: Connect-RPC Implementation (In Progress)

**Status**: Migrated from stdlib HTTP to Connect-RPC with multi-source support

#### Completed
- [x] #156 Added ImageProcessorService to proto with ProcessImage and StreamProcessVideo RPCs
- [x] #157 Implemented Connect-RPC server in `src/go_api/pkg/interfaces/connectrpc/`
- [x] #158 Refactored main.go to clean App structure
- [x] #159 Setup buf for code generation with Docker
- [x] #160 Added HTTP annotations for REST-friendly endpoints
- [x] #161 Modularized protobuf into separate service definitions (common, config_service, image_processor_service)
- [x] #162 Implemented ListInputs endpoint with BDD test coverage
- [x] #163 Created dynamic video grid component supporting up to 9 sources
- [x] #164 Added drawer UI for source selection with real-time updates
- [x] #165 Support per-source filter and resolution configuration
- [x] #166 Extended FileService with video upload/list capabilities (9 scenarios)
- [x] #167 Implemented video repository and use cases with 24 unit tests
- [x] #168 Created frontend video components (selector, upload) with tabs
- [x] #169 Added WebSocket video session manager for frame streaming

#### Pending
- [ ] #507 Integrate video decoding library (gmf/ffmpeg) for actual frame extraction
- [ ] #507 Generate preview images from first video frame
- ~~#507 Implement StreamProcessVideo bidirectional streaming (currently returns Unimplemented)~~ (obsolete — the `StreamProcessVideo` RPC no longer exists in any proto; superseded by `SignalingStream` in `proto/webrtc_signal.proto:111`)
- [ ] #507 Complete video playback loop with frame-by-frame filter application
- [ ] #507 Benchmark latency vs current WebSocket
- [ ] #507 Add grpc-web support for browser compatibility

**Note**: Using Connect-RPC instead of traditional gRPC-Gateway. Connect provides native HTTP/JSON support without needing a separate gateway.

### POC 2: Binary Transport (Quick Win) - Next Optimization

**Status**: Historical — the former implementation used PNG base64 over WebSocket (working but inefficient); the current transport is WebRTC

**Why**: Eliminate base64 overhead and PNG encoding/decoding

**Current Flow:**
```
Browser → canvas.toDataURL('png') → base64 → WebSocket → 
Go decode PNG → RGB raw → RPC → C++/CUDA → 
RGB raw → encode PNG → base64 → WebSocket → Browser
```

**Proposed Flow:**
```
Browser → canvas.getImageData() → raw RGB → WebSocket binary → 
RPC → C++/CUDA → raw RGB → WebSocket binary → Browser
```

#### Tasks
- [ ] #508 Modify WebSocket to accept binary frames instead of JSON
- [ ] #508 Frontend: use canvas.getImageData().data (Uint8ClampedArray)
- [ ] #508 Backend: remove PNG decode step
- [ ] #508 Remove PNG encode step (return raw bytes)
- [ ] #508 Frontend: create ImageData and putImageData directly
- [ ] #508 Benchmark performance improvement

**Expected Outcome**: 
- 40-50% bandwidth reduction
- 30% CPU reduction
- 100-150 FPS (vs current ~25-30 FPS)
- Simpler code (no encode/decode)

### POC 3: H.264/H.265 with NVENC

**Why**: Biggest performance gain, hardware-accelerated encoding

#### Tasks
- [ ] #509 Research NVIDIA Video Codec SDK (NVENC)
- [ ] #509 Study H.264 encoding parameters (bitrate, keyframe interval)
- [ ] #509 Investigate FFmpeg integration vs direct NVENC API
- [ ] #509 Check browser H.264 decoding support

#### Implementation Plan - Option A (FFmpeg)
- [ ] #510 Add FFmpeg to Docker container
- [ ] #510 Create CGO wrapper for FFmpeg encoding
- [ ] #510 Pipeline: CUDA process → FFmpeg H.264 encode → stream
- [ ] #510 Use HLS or MPEG-DASH for adaptive bitrate
- [ ] #510 Test with video.js or native `<video>` tag

#### Implementation Plan - Option B (Direct NVENC)
- [ ] #510 Download NVIDIA Video Codec SDK
- [ ] #510 Link NVENC library in Bazel build
- [ ] #510 Create encoder wrapper: raw frames → H.264 NAL units
- [ ] #510 Stream NAL units via WebSocket
- [ ] #510 Use Media Source Extensions (MSE) in browser

**Expected Outcome**: 10-50x bandwidth reduction, GPU encoding, professional-grade streaming

**Resources**:
- https://developer.nvidia.com/video-codec-sdk
- https://trac.ffmpeg.org/wiki/HWAccelIntro
- https://developer.mozilla.org/en-US/docs/Web/API/Media_Source_Extensions_API

### POC 4: WebRTC

**Why**: Production-grade, adaptive bitrate, low latency, P2P capable

#### Tasks
- [ ] #511 Research WebRTC data channels vs media tracks
- [ ] #511 Study STUN/TURN for NAT traversal
- [ ] #511 Review Pion WebRTC library (Go)
- [ ] #511 Understand signaling protocols (SDP offer/answer)

#### Implementation Plan
- [ ] #511 Set up signaling server (WebSocket for SDP exchange)
- [ ] #511 Implement Pion WebRTC peer in Go
- [ ] #511 Use WebRTC data channels for control
- [ ] #511 Use media tracks for video (encoded with NVENC)
- [ ] #511 Handle ICE candidates for connection establishment
- [ ] #511 Test with built-in browser WebRTC

**Expected Outcome**: Production-ready streaming, sub-100ms latency, adaptive quality

**Resources**:
- https://github.com/pion/webrtc
- https://webrtc.org/getting-started/overview
- https://developer.mozilla.org/en-US/docs/Web/API/WebRTC_API

### POC 5: WebCodecs API (Browser-side encoding)


**Expected Outcome**: Massive bandwidth reduction on upload, client hardware encoding

**Resources**:
- https://developer.mozilla.org/en-US/docs/Web/API/WebCodecs_API
- https://web.dev/webcodecs/


## Metrics to Track

For each POC, measure:
- [ ] Bandwidth usage (MB/s)
- [ ] Latency (frame capture → display)
- [ ] CPU usage (server-side)
- [ ] GPU usage (encoding + processing)
- [ ] Frame drop rate
- [ ] Quality (PSNR/SSIM if applicable)

## Decision Criteria

Choose implementation based on:
1. **Learning Value**: Does it teach new concepts?
2. **Performance Gain**: Is the improvement significant?
3. **Implementation Effort**: Time to working POC?
4. **Production Readiness**: Can it scale?
5. **Browser Support**: Does it work everywhere?

## Notes

- Don't optimize prematurely - current solution works for learning
- Each POC should be in a separate branch
- Document findings in this file
- Can run multiple transports simultaneously (feature flag)
- Consider adding `/api/transport/select` endpoint to switch modes

## WebRTC Bidirectional Stream Implementation

**Status**: Implemented - Replaces unidirectional RPC methods with bidirectional streaming

### Architecture

```
Frontend (Connect-RPC Stream) 
    ↕ keepalive + reconexión automática
Go Server (Proxy transparente)
    ↕ gRPC stream bidireccional
C++ gRPC Server (Lógica WebRTC + cola thread-safe)
```

### Message Flow

1. **Start Session**: Frontend sends `start_session` → C++ creates WebRTC session → C++ sends `start_session_response` with SDP answer
2. **ICE Candidates**: 
   - Frontend → C++: Local candidates from browser
   - C++ → Frontend: Local candidates from server (via thread-safe queue)
3. **Keepalive**: C++ sends keepalive every 30 seconds to maintain stream
4. **Close Session**: Frontend sends `close_session` → C++ closes WebRTC session → C++ sends `close_session_response`

### Error Handling and Reconnection

- **Stream disconnection**: Frontend detects stream close and attempts reconnection
- **WebRTC state check**: If WebRTC is connected, stream reconnects silently; if not, session is cleaned up
- **Context cancellation**: Go proxy uses context cancellation for proper cleanup
- **Thread safety**: C++ uses mutex and condition variables for candidate queue

### Keepalive

- Interval: 30 seconds
- Purpose: Maintain stream alive, detect dead connections
- Implementation: Separate thread in C++ server sends keepalive messages

### Lifecycle Management

- **RAII**: C++ ensures cleanup with thread joins
- **WaitGroups**: Go uses WaitGroups to synchronize goroutines
- **Session cleanup**: Automatic cleanup when stream closes or session times out

