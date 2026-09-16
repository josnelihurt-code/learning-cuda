# WebRTC Session Lifecycle Hardening

## Context

This document captures two loose ends identified during the WebRTC reliability investigation
that addressed the signaling race (404 `signaling session not found`) and the 30-second cleanup
disconnect. Neither caused the observed production symptoms but both are latent bugs or design
gaps that should be closed.

## Issue 1: Zombie Go Signaling Sessions

### Problem

The Go API keeps a per-session state machine at `src/go_api/pkg/interfaces/connectrpc/webrtc_session_manager.go`
around the gRPC bidirectional stream to the C++ service. Sessions live in the
`WebRTCSignalingSessionManager.sessions` map and are only removed by `removeSession`, which is
called from:

- `StartSession` error paths (create/send/wait failure).
- `CloseSession` (happy path).

They are **not** removed when:

- The upstream gRPC stream to C++ fails or closes (the receive loop marks `closed=true` but
  leaves the entry in the map).
- The browser disappears without calling `CloseSession` (tab closed without `beforeunload`
  firing, mobile backgrounded, connection dropped).
- The C++ cleanup thread proactively closes the session peer connection.

### Consequence

The map grows unboundedly. Each entry holds a cancelled context, a closed stream, an event
queue, and a `notifyCh`. In practice this is a slow leak: ~300 bytes per zombie plus GC
pressure. Not user-visible, but it accumulates over days. Under high reconnect churn (unreliable
mobile networks) the leak rate can become measurable.

There is also a correctness concern: a subsequent `PollEvents` or `SendIceCandidate` for a
zombie session will take the `errSignalingSessionClosed` path and return
`CodeFailedPrecondition`, which the frontend may interpret as a retryable transient rather than
terminal. Low severity but possible.

### Proposed Fix

In `webRTCSignalingSession.receiveLoop`, when the loop exits (EOF or error), call back into the
manager to `removeSession`. Options:

1. Pass a `onTerminated func()` callback into `newWebRTCSignalingSession` and invoke it from
   `markClosed`. Clean and keeps the coupling inside the manager.
2. Add a periodic GC scan in the manager that removes entries where `session.closed &&
   time.Since(lastActivity) > N` seconds. Simpler but uses a goroutine.

Option 1 is preferred.

### Test

Add a unit test in `webrtc_session_manager_test.go` that:

1. Creates a session against a fake client whose stream returns EOF immediately.
2. Asserts that after the receive loop exits, the session is absent from the map.

## Issue 2: No TURN Server Configured

### Problem

Both the C++ peer connection (`src/cpp_accelerator/adapters/webrtc/webrtc_manager.cpp:85`) and the
frontend peer connection (`src/front-end/src/infrastructure/connection/webrtc-service.ts`,
`DEFAULT_ICE_SERVERS` at line 34, applied at line 128)
configure only `stun:stun.l.google.com:19302`. No TURN relay is available.

This is already tracked as backlog item `#511 Study STUN/TURN for NAT traversal` in
`docs/backlog/video-streaming.md`, but it is worth making the failure mode explicit here because
it is mechanically different from the issues that motivated this document.

### Consequence

When either peer is behind a symmetric NAT or a firewall that blocks direct UDP, ICE gathering
succeeds (host and server-reflexive candidates are collected) but no candidate pair ever passes
connectivity checks. The symptom is:

- `iceConnectionState` transitions to `checking` and stays there.
- Eventually the browser reports `iceConnectionState: failed`.
- No SCTP or RTP traffic ever flows.

This is distinct from the 30-second cleanup disconnect (where the connection is established and
then torn down) and distinct from the signaling race (where the 404 happens before ICE
gathering).

Today the production deployment works because both ends (browser on consumer networks, C++
server on the Jetson behind a residential NAT) happen to allow UDP hole-punching via STUN. Any
deployment into a corporate network, mobile data on some carriers, or behind CGNAT would fail.

### Proposed Fix

1. Add a TURN server. Options: coturn self-hosted (cheap, adds ops burden), commercial service
   (Twilio, Cloudflare). For a learning project, self-hosted coturn on vultr alongside the
   existing infra is pragmatic.
2. Make STUN/TURN configurable in both services via `config/config.*.yaml` so dev and prod can
   diverge.
3. Validate with: browser on a mobile network with Wi-Fi disabled, or behind `iptables` rules
   that block outbound UDP except to the TURN endpoint.

## Priority

Both issues are non-urgent.

- Issue 1 is a slow leak; address on the next pass through the signaling layer or when memory
  pressure becomes observable.
- Issue 2 blocks deployments into restrictive networks. Address when the project needs to work
  outside the current home/office setup.

## Related

- `#511 Study STUN/TURN for NAT traversal` (existing backlog entry)
- `docs/backlog/webrtc-trickle-ice-inline.md`
