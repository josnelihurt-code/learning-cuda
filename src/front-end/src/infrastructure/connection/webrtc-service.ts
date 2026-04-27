import { createPromiseClient, type PromiseClient } from '@connectrpc/connect';
import { createConnectTransport } from '@connectrpc/connect-web';
import { logger } from '@/infrastructure/observability/otel-logger';
import { telemetryService } from '@/infrastructure/observability/telemetry-service';
import type {
  CreateWebRTCSessionOptions,
  IWebRTCService,
} from '@/domain/interfaces/i-webrtc-service';
import { WebRTCSession } from '@/domain/value-objects/webrtc-session';
import {
  CloseSessionRequest,
  PollEventsRequest,
  SignalingMessage,
  type StartSessionResponse,
  SendIceCandidateRequest,
} from '@/gen/webrtc_signal_pb';
import { ProcessImageRequest } from '@/gen/image_processor_service_pb';
import { WebRTCSignalingService } from '@/gen/webrtc_signal_connect';
import { tracingInterceptor } from '@/infrastructure/grpc/tracing-interceptor';
import { nextMessageId, packMessage } from '@/infrastructure/transport/data-channel-framing';

type ConnectionState = 'connected' | 'disconnected' | 'connecting' | 'error';

const DEFAULT_POLL_TIMEOUT_MS = 25_000;
const RETRY_DELAY_MS = 1_000;
const DEFAULT_HEARTBEAT_INTERVAL_MS = 10_000;
const HEARTBEAT_STALE_THRESHOLD_MS = 15_000;
const HEARTBEAT_MAX_CONSECUTIVE_FAILURES = 3;
const ICE_GATHERING_TIMEOUT_MS = 1_500;
const STATS_DATA_CHANNEL_LABEL = 'cpp-processor-stats';
const CONTROL_DATA_CHANNEL_LABEL = 'control';

type ControlChannelListener = (sessionId: string, channel: RTCDataChannel | null) => void;

export class WebRTCService implements IWebRTCService {
  private initialized = false;
  private initPromise: Promise<void> | null = null;
  private sessions: Map<string, WebRTCSession> = new Map();
  private peerConnections: Map<string, RTCPeerConnection> = new Map();
  private dataChannels: Map<string, RTCDataChannel> = new Map();
  private detectionChannels: Map<string, RTCDataChannel> = new Map();
  private statsChannels: Map<string, RTCDataChannel> = new Map();
  private controlChannels: Map<string, RTCDataChannel> = new Map();
  private controlChannelListeners: Set<ControlChannelListener> = new Set();
  private remoteStreamHandlers: Map<string, (stream: MediaStream) => void> = new Map();
  private remoteStreams: Map<string, MediaStream> = new Map();
  private pollControllers: Map<string, AbortController> = new Map();
  private pollCursors: Map<string, number> = new Map();
  // Pending local ICE candidates queued while StartSession is in flight.
  // Prevents racing the server-side session registration (would otherwise 404).
  private pendingIceCandidates: Map<string, RTCIceCandidate[]> = new Map();
  private sessionNegotiated: Map<string, boolean> = new Map();
  private heartbeatTimers: Map<string, ReturnType<typeof setInterval>> = new Map();
  private lastHeartbeatSentAt: Map<string, number> = new Map();
  private lastHeartbeatAckAt: Map<string, number> = new Map();
  private heartbeatConsecutiveFailures: Map<string, number> = new Map();
  private reconnectingSessions: Set<string> = new Set();
  private connectionState: ConnectionState = 'disconnected';
  private lastRequest: string | null = null;
  private lastRequestTime: Date | null = null;
  private signalingClient: PromiseClient<typeof WebRTCSignalingService>;

  constructor() {
    const transport = createConnectTransport({
      baseUrl: window.location.origin,
      interceptors: [tracingInterceptor],
      useBinaryFormat: false,
    });

    this.signalingClient = createPromiseClient(WebRTCSignalingService, transport);
  }

  async initialize(): Promise<void> {
    if (this.initPromise) {
      return this.initPromise;
    }

    this.initPromise = telemetryService.withSpanAsync(
      'WebRTCService.initialize',
      {
        'service.name': 'WebRTCService',
        'service.method': 'initialize',
      },
      async (span) => {
        if (!this.isSupported()) {
          span?.setAttribute('webrtc.supported', false);
          logger.warn('WebRTC is not supported in this browser');
          this.initialized = false;
          return;
        }

        span?.setAttribute('webrtc.supported', true);
        span?.addEvent('WebRTC API available');

        this.initialized = true;
        logger.info('WebRTC service initialized successfully');
      }
    );

    return this.initPromise;
  }

  isInitialized(): boolean {
    return this.initialized;
  }

  isSupported(): boolean {
    return (
      typeof RTCPeerConnection !== 'undefined' &&
      typeof RTCDataChannel !== 'undefined' &&
      typeof navigator !== 'undefined' &&
      typeof navigator.mediaDevices !== 'undefined' &&
      typeof navigator.mediaDevices.getUserMedia !== 'undefined'
    );
  }

  createPeerConnection(config?: RTCConfiguration): RTCPeerConnection | null {
    if (!this.isSupported()) {
      logger.error('WebRTC is not supported, cannot create peer connection');
      return null;
    }

    try {
      const peerConnection = new RTCPeerConnection({
        iceServers: [{ urls: 'stun:stun.l.google.com:19302' }],
        ...config,
      });

      logger.info('Peer connection created', {
        'webrtc.ice_servers_count': peerConnection.getConfiguration().iceServers?.length || 0,
      });

      return peerConnection;
    } catch (error) {
      logger.error('Failed to create peer connection', {
        'error.message': error instanceof Error ? error.message : String(error),
      });
      return null;
    }
  }

  createDataChannel(
    peerConnection: RTCPeerConnection,
    label: string,
    options?: RTCDataChannelInit
  ): RTCDataChannel | null {
    if (!this.isSupported()) {
      logger.error('WebRTC is not supported, cannot create data channel');
      return null;
    }

    try {
      const dataChannel = peerConnection.createDataChannel(label, options);
      logger.info('Data channel created', {
        'webrtc.data_channel.label': label,
        'webrtc.data_channel.ordered': options?.ordered !== false,
      });
      return dataChannel;
    } catch (error) {
      logger.error('Failed to create data channel', {
        'error.message': error instanceof Error ? error.message : String(error),
        'webrtc.data_channel.label': label,
      });
      return null;
    }
  }

  async getUserMedia(constraints: MediaStreamConstraints): Promise<MediaStream> {
    if (!this.isSupported()) {
      throw new Error('WebRTC is not supported in this browser');
    }

    try {
      const stream = await navigator.mediaDevices.getUserMedia(constraints);
      logger.info('User media stream obtained', {
        'webrtc.media.audio': constraints.audio !== false,
        'webrtc.media.video': constraints.video !== false,
      });
      return stream;
    } catch (error) {
      logger.error('Failed to get user media', {
        'error.message': error instanceof Error ? error.message : String(error),
        'webrtc.media.audio': constraints.audio !== false,
        'webrtc.media.video': constraints.video !== false,
      });
      throw error;
    }
  }

  async setupPingChannel(sessionId: string): Promise<void> {
    if (!this.isSupported()) {
      logger.debug(`[WebRTC:${sessionId}] WebRTC not supported, skipping ping channel setup`);
      return;
    }

    await this.initialize();
    if (!this.initialized) {
      logger.debug(`[WebRTC:${sessionId}] Service not initialized, skipping ping channel setup`);
      return;
    }

    const peerConnection = this.createPeerConnection();
    if (!peerConnection) {
      logger.debug(`[WebRTC:${sessionId}] Failed to create peer connection`);
      return;
    }

    const dataChannel = this.createDataChannel(peerConnection, sessionId);
    if (!dataChannel) {
      logger.debug(`[WebRTC:${sessionId}] Failed to create data channel`);
      peerConnection.close();
      return;
    }

    this.peerConnections.set(sessionId, peerConnection);
    this.dataChannels.set(sessionId, dataChannel);
    this.configureDataChannelHandlers(sessionId, dataChannel, peerConnection);

    dataChannel.onopen = () => {
      logger.debug(`[WebRTC:${sessionId}] Data channel opened`);
      for (const chunk of packMessage(nextMessageId(), new TextEncoder().encode('ping'))) {
        dataChannel.send(chunk);
      }
      logger.debug(`[WebRTC:${sessionId}] Sent: ping`);
    };

    dataChannel.onmessage = (event: MessageEvent) => {
      const message = event.data;
      logger.debug(`[WebRTC:${sessionId}] Received: ${message}`);
      if (message === 'pong') {
        logger.info(`[WebRTC:${sessionId}] Ping-pong successful`);
      }
    };

    try {
      await this.negotiateSession(sessionId, peerConnection);
      this.ensurePolling(sessionId);
    } catch (error) {
      logger.debug(`[WebRTC:${sessionId}] Ping channel setup failed`, {
        'error.message': error instanceof Error ? error.message : String(error),
      });
      this.cleanupSession(sessionId);
    }
  }

  async createSession(
    sourceId: string,
    options: CreateWebRTCSessionOptions = {}
  ): Promise<WebRTCSession> {
    logger.info(`[WebRTC] createSession called for sourceId: ${sourceId}`);

    if (!this.isSupported()) {
      logger.error('[WebRTC] WebRTC is not supported in this browser');
      throw new Error('WebRTC is not supported in this browser');
    }

    await this.initialize();
    if (!this.initialized) {
      logger.error('[WebRTC] WebRTC service not initialized');
      throw new Error('WebRTC service not initialized');
    }

    const sessionMode = options.mode ?? 'frame-processing';
    const session = WebRTCSession.create(sourceId, sessionMode);
    const sessionId = session.getId();

    const peerConnection = this.createPeerConnection();
    if (!peerConnection) {
      throw new Error('Failed to create peer connection');
    }

    this.peerConnections.set(sessionId, peerConnection);
    this.sessions.set(sessionId, session);
    this.connectionState = 'connecting';
    this.configurePeerConnectionStateHandlers(sessionId, peerConnection);
    this.configureRemoteTrackHandlers(sessionId, peerConnection);

    if (options.onRemoteStream) {
      this.remoteStreamHandlers.set(sessionId, options.onRemoteStream);
    }

    if (options.localStream) {
      for (const track of options.localStream.getTracks()) {
        if (track.kind === 'video' && sessionMode === 'camera-mediatrack') {
          const transceiver = peerConnection.addTransceiver(track, {
            direction: 'sendonly',
            streams: [options.localStream],
          });
          const caps = RTCRtpSender.getCapabilities('video');
          if (caps) {
            const h264Codecs = caps.codecs.filter(
              c => c.mimeType.toLowerCase() === 'video/h264'
            );
            if (h264Codecs.length > 0) {
              transceiver.setCodecPreferences(h264Codecs);
              logger.info(`[WebRTC:${sessionId}] Forced H264 codec preference for webcam track`);
            }
          }
        } else {
          peerConnection.addTrack(track, options.localStream);
        }
      }
      logger.info(`[WebRTC:${sessionId}] Local media tracks attached`, {
        'webrtc.track_count': options.localStream.getTracks().length,
        'webrtc.session_mode': sessionMode,
      });
    }

    if (sessionMode === 'camera-mediatrack') {
      peerConnection.addTransceiver('video', { direction: 'recvonly' });
      logger.info(`[WebRTC:${sessionId}] Added recvonly transceiver for processed camera video`);
    }

    const shouldUseDataChannel = options.useDataChannel ?? true;
    if (shouldUseDataChannel) {
      const dataChannel = this.createDataChannel(peerConnection, 'process-image');
      if (!dataChannel) {
        logger.debug(`[WebRTC:${sessionId}] Failed to create data channel`);
        peerConnection.close();
        throw new Error('Failed to create data channel');
      }
      this.dataChannels.set(sessionId, dataChannel);
      this.configureDataChannelHandlers(sessionId, dataChannel, peerConnection);

      // The "control" data channel carries ControlRequest/ControlResponse
      // envelopes used by the browser to ask the C++ accelerator for filter
      // capabilities and version info. It is created alongside the primary
      // process-image channel so a single SDP negotiation provisions both.
      const controlChannel = this.createDataChannel(peerConnection, CONTROL_DATA_CHANNEL_LABEL);
      if (controlChannel) {
        controlChannel.binaryType = 'arraybuffer';
        this.controlChannels.set(sessionId, controlChannel);
        const notifyOpen = () => {
          for (const listener of this.controlChannelListeners) {
            try { listener(sessionId, controlChannel); } catch (e) { void e; }
          }
        };
        const notifyClose = () => {
          this.controlChannels.delete(sessionId);
          for (const listener of this.controlChannelListeners) {
            try { listener(sessionId, null); } catch (e) { void e; }
          }
        };
        if (controlChannel.readyState === 'open') {
          notifyOpen();
        } else {
          controlChannel.addEventListener('open', notifyOpen);
        }
        controlChannel.addEventListener('close', notifyClose);
        controlChannel.addEventListener('error', notifyClose);
      }

      // Opening multiple SCTP data channels concurrently in the same SDP
      // triggers a DCEP race with libdatachannel on the C++ side that causes
      // the SCTP stream to close within milliseconds of opening. Detections
      // for frame-processing mode travel inside ProcessImageResponse, so the
      // auxiliary channel is only required when video frames flow via an
      // RTP media track (camera-mediatrack) and responses are async.
      if (sessionMode === 'camera-mediatrack') {
        const detectionChannel = this.createDataChannel(peerConnection, 'detections');
        if (detectionChannel) {
          detectionChannel.binaryType = 'arraybuffer';
          this.detectionChannels.set(sessionId, detectionChannel);
        }
      }
    }

    try {
      await this.negotiateSession(sessionId, peerConnection);
      this.ensurePolling(sessionId);

      if (shouldUseDataChannel) {
        await this.waitForDataChannelOpen(sessionId);
      }

      logger.info(`[WebRTC:${sessionId}] Session created successfully`, {
        'session.source_id': sourceId,
        'session.mode': sessionMode,
      });

      return session;
    } catch (error) {
      logger.error(`[WebRTC:${sessionId}] Failed to create session`, {
        'error.message': error instanceof Error ? error.message : String(error),
      });
      this.cleanupSession(sessionId);
      throw error;
    }
  }

  async closeSession(sessionId: string): Promise<void> {
    try {
      logger.info(`[WebRTC:${sessionId}] Closing signaling session`);
      this.lastRequest = 'CloseSession';
      this.lastRequestTime = new Date();
      await this.signalingClient.closeSession(new CloseSessionRequest({ sessionId }));
    } catch (error) {
      logger.debug(`[WebRTC:${sessionId}] Failed to close signaling session cleanly`, {
        'error.message': error instanceof Error ? error.message : String(error),
      });
    } finally {
      this.cleanupSession(sessionId);
      if (this.sessions.size === 0) {
        this.connectionState = 'disconnected';
      }
    }
  }

  // Connect-RPC's fetch is cancelled by the browser when the page unloads, so
  // an `await closeSession(...)` from a `beforeunload` handler does not reach
  // the server. The orphaned session keeps the C++ accelerator's encoder /
  // CUDA pool / signaling stream alive until ICE consent expires (~30s),
  // during which a fresh page load cannot establish a new session and stays
  // stuck on "connecting". `navigator.sendBeacon` is the only transport the
  // browser guarantees to flush during unload.
  closeSessionBeacon(sessionId: string): boolean {
    if (typeof navigator === 'undefined' || typeof navigator.sendBeacon !== 'function') {
      return false;
    }
    try {
      const url = `${window.location.origin}/cuda_learning.WebRTCSignalingService/CloseSession`;
      const body = new Blob([JSON.stringify({ sessionId })], { type: 'application/json' });
      const queued = navigator.sendBeacon(url, body);
      this.cleanupSession(sessionId);
      if (this.sessions.size === 0) {
        this.connectionState = 'disconnected';
      }
      return queued;
    } catch {
      return false;
    }
  }

  startHeartbeat(sessionId: string, intervalMs: number): void {
    const dataChannel = this.dataChannels.get(sessionId);
    if (!dataChannel) {
      logger.debug(`[WebRTC:${sessionId}] Cannot start heartbeat: data channel not found`);
      return;
    }

    if (this.heartbeatTimers.has(sessionId)) {
      return;
    }

    const effectiveInterval = intervalMs > 0 ? intervalMs : DEFAULT_HEARTBEAT_INTERVAL_MS;

    if (dataChannel.readyState === 'open') {
      this.connectionState = 'connected';
      this.lastRequest = 'Data channel ready';
      this.lastRequestTime = new Date();
    }

    const timer = setInterval(() => {
      this.sendHeartbeat(sessionId, 'interval');
      this.evaluateHeartbeatHealth(sessionId);
    }, effectiveInterval);

    this.heartbeatTimers.set(sessionId, timer);
    this.lastHeartbeatAckAt.set(sessionId, Date.now());
    logger.debug(`[WebRTC:${sessionId}] Heartbeat started`, {
      'webrtc.heartbeat_interval_ms': effectiveInterval,
    });
  }

  stopHeartbeat(sessionId: string): void {
    const timer = this.heartbeatTimers.get(sessionId);
    if (timer) {
      clearInterval(timer);
      this.heartbeatTimers.delete(sessionId);
      logger.debug(`[WebRTC:${sessionId}] Heartbeat stopped`);
    }

    if (!this.sessions.has(sessionId)) {
      return;
    }

    const dataChannel = this.dataChannels.get(sessionId);
    if (!dataChannel || dataChannel.readyState !== 'open') {
      this.connectionState = 'disconnected';
    }
  }

  private sendHeartbeat(sessionId: string, reason: 'open' | 'interval' | 'guarded-resend'): void {
    const dataChannel = this.dataChannels.get(sessionId);
    if (!dataChannel || dataChannel.readyState !== 'open') {
      logger.debug(`[WebRTC:${sessionId}] Heartbeat skipped: data channel not open`, {
        'heartbeat.reason': reason,
        'data_channel.ready_state': dataChannel?.readyState ?? 'missing',
      });
      return;
    }

    // Empty ProcessImageRequest acts as a keepalive: the C++ side refreshes
    // last_heartbeat on any inbound DC message and treats filter-less control
    // updates as heartbeats (no-op on live filter state).
    const payload = new ProcessImageRequest({ sessionId, apiVersion: '1.1' }).toBinary();

    try {
      for (const chunk of packMessage(nextMessageId(), payload)) {
        dataChannel.send(chunk);
      }
      this.lastHeartbeatSentAt.set(sessionId, Date.now());
      this.heartbeatConsecutiveFailures.set(sessionId, 0);
      logger.verbose(`[WebRTC:${sessionId}] Heartbeat sent`, {
        'heartbeat.reason': reason,
      });
    } catch (error) {
      const failures = (this.heartbeatConsecutiveFailures.get(sessionId) ?? 0) + 1;
      this.heartbeatConsecutiveFailures.set(sessionId, failures);
      logger.debug(`[WebRTC:${sessionId}] Heartbeat send failed`, {
        'error.message': error instanceof Error ? error.message : String(error),
        'data_channel.ready_state': dataChannel.readyState,
        'heartbeat.reason': reason,
        'heartbeat.consecutive_failures': failures,
      });
    }
  }

  private evaluateHeartbeatHealth(sessionId: string): void {
    const lastSentAt = this.lastHeartbeatSentAt.get(sessionId) ?? 0;
    const lastAckAt = this.lastHeartbeatAckAt.get(sessionId) ?? 0;
    const failures = this.heartbeatConsecutiveFailures.get(sessionId) ?? 0;
    const now = Date.now();

    if (lastAckAt > 0 && now - lastAckAt > DEFAULT_HEARTBEAT_INTERVAL_MS) {
      this.sendHeartbeat(sessionId, 'guarded-resend');
    }

    if (failures >= HEARTBEAT_MAX_CONSECUTIVE_FAILURES) {
      logger.warn(`[WebRTC:${sessionId}] Heartbeat degraded: repeated send failures`, {
        'heartbeat.consecutive_failures': failures,
      });
      this.triggerReconnect(sessionId, 'send-failures');
      return;
    }

    if (lastSentAt > 0 && now - lastSentAt > HEARTBEAT_STALE_THRESHOLD_MS) {
      logger.warn(`[WebRTC:${sessionId}] Heartbeat degraded: stale outbound heartbeat`, {
        'heartbeat.last_sent_ms_ago': now - lastSentAt,
        'heartbeat.last_ack_ms_ago': lastAckAt > 0 ? now - lastAckAt : -1,
      });
      this.triggerReconnect(sessionId, 'stale-heartbeat');
    }
  }

  private triggerReconnect(
    sessionId: string,
    reason: 'send-failures' | 'stale-heartbeat' | 'data-channel-error' | 'data-channel-closed'
  ): void {
    if (this.reconnectingSessions.has(sessionId)) {
      return;
    }

    this.reconnectingSessions.add(sessionId);
    logger.warn(`[WebRTC:${sessionId}] Triggering controlled reconnect`, {
      'reconnect.reason': reason,
    });

    void this.closeSession(sessionId).catch((error) => {
      logger.error(`[WebRTC:${sessionId}] Controlled reconnect close failed`, {
        'error.message': error instanceof Error ? error.message : String(error),
      });
    }).finally(() => {
      this.reconnectingSessions.delete(sessionId);
    });
  }

  getActiveSessions(): WebRTCSession[] {
    return Array.from(this.sessions.values());
  }

  getDataChannel(sessionId: string): RTCDataChannel | null {
    return this.dataChannels.get(sessionId) ?? null;
  }

  getDetectionDataChannel(sessionId: string): RTCDataChannel | null {
    return this.detectionChannels.get(sessionId) ?? null;
  }

  getStatsDataChannel(sessionId: string): RTCDataChannel | null {
    return this.statsChannels.get(sessionId) ?? null;
  }

  getControlDataChannel(sessionId: string): RTCDataChannel | null {
    return this.controlChannels.get(sessionId) ?? null;
  }

  getAnyOpenControlDataChannel(): RTCDataChannel | null {
    for (const channel of this.controlChannels.values()) {
      if (channel.readyState === 'open') {
        return channel;
      }
    }
    return null;
  }

  addControlChannelListener(listener: ControlChannelListener): () => void {
    this.controlChannelListeners.add(listener);
    return () => this.controlChannelListeners.delete(listener);
  }

  ensureStatsDataChannel(sessionId: string): RTCDataChannel | null {
    const existing = this.statsChannels.get(sessionId);
    if (existing) {
      return existing;
    }
    const peerConnection = this.peerConnections.get(sessionId);
    if (!peerConnection) {
      return null;
    }
    const statsChannel = this.createDataChannel(peerConnection, STATS_DATA_CHANNEL_LABEL);
    if (!statsChannel) {
      return null;
    }
    statsChannel.binaryType = 'arraybuffer';
    this.statsChannels.set(sessionId, statsChannel);
    return statsChannel;
  }

  sendControlRequest(sessionId: string, request: ProcessImageRequest): void {
    const dataChannel = this.dataChannels.get(sessionId);
    if (!dataChannel || dataChannel.readyState !== 'open') {
      throw new Error(`WebRTC data channel is not open for session ${sessionId}`);
    }

    const payload = request.toBinary();
    for (const chunk of packMessage(nextMessageId(), payload)) {
      dataChannel.send(chunk);
    }
  }

  getConnectionStatus(): { state: ConnectionState; lastRequest: string | null; lastRequestTime: Date | null } {
    const hasOpenChannel = Array.from(this.dataChannels.values()).some((channel) => channel.readyState === 'open');
    const hasConnectingChannel = Array.from(this.dataChannels.values()).some((channel) => channel.readyState === 'connecting');

    let currentState: ConnectionState = 'disconnected';
    if (this.connectionState === 'error') {
      currentState = 'error';
    } else if (this.connectionState === 'connected') {
      currentState = 'connected';
    } else if (hasOpenChannel) {
      currentState = 'connected';
    } else if (hasConnectingChannel || this.sessions.size > 0) {
      currentState = 'connecting';
    }

    return {
      state: currentState,
      lastRequest: this.lastRequest,
      lastRequestTime: this.lastRequestTime,
    };
  }

  private async negotiateSession(sessionId: string, peerConnection: RTCPeerConnection): Promise<void> {
    logger.info(`[WebRTC:${sessionId}] Negotiating signaling session over Connect`);

    this.pendingIceCandidates.set(sessionId, []);
    this.sessionNegotiated.set(sessionId, false);

    peerConnection.onicecandidate = (event: RTCPeerConnectionIceEvent) => {
      if (!event.candidate) {
        return;
      }

      if (!this.sessionNegotiated.get(sessionId)) {
        const buffer = this.pendingIceCandidates.get(sessionId);
        if (buffer) {
          buffer.push(event.candidate);
          logger.debug(`[WebRTC:${sessionId}] Buffered ICE candidate (session not yet negotiated)`, {
            'webrtc.pending_candidates': buffer.length,
          });
        }
        return;
      }

      // Once the peer connection is established, discard late trickle candidates.
      // libdatachannel (juice) reinitializes the ICE agent when new remote candidates
      // arrive post-connection, which tears down the DTLS/SCTP transport and aborts
      // the data channel within milliseconds of it opening. The already-selected pair
      // keeps the connection alive; late srflx/relay candidates are redundant on LAN.
      if (
        peerConnection.connectionState === 'connected' ||
        peerConnection.iceConnectionState === 'connected' ||
        peerConnection.iceConnectionState === 'completed'
      ) {
        logger.debug(`[WebRTC:${sessionId}] Discarding late trickle ICE candidate`, {
          'webrtc.candidate_type': event.candidate.type ?? 'unknown',
          'peer_connection.state': peerConnection.connectionState,
          'peer_connection.ice_connection_state': peerConnection.iceConnectionState,
        });
        return;
      }

      void this.sendIceCandidateSafe(sessionId, event.candidate);
    };

    const offer = await peerConnection.createOffer();
    await peerConnection.setLocalDescription(offer);

    // Block until ICE gathering completes so the SDP offer embeds every candidate.
    // libdatachannel/juice on the peer side reinitializes the ICE agent when new
    // remote candidates arrive after the DTLS/SCTP transport is established, which
    // aborts the data channel milliseconds after it opens. Non-trickle negotiation
    // avoids that race entirely; the modest extra setup latency is acceptable for
    // a dev/LAN topology and still compatible with STUN over WAN.
    await this.waitForIceGatheringComplete(peerConnection, ICE_GATHERING_TIMEOUT_MS);

    const finalOfferSdp = peerConnection.localDescription?.sdp || offer.sdp || '';

    this.lastRequest = 'StartSession';
    this.lastRequestTime = new Date();
    const response = await this.signalingClient.startSession({
      sessionId,
      sdpOffer: finalOfferSdp,
    });

    await this.applyStartSessionResponse(sessionId, response);

    this.sessionNegotiated.set(sessionId, true);
    // Any candidates gathered after the SDP was serialized are redundant: they
    // were not in the offer the peer parsed, and sending them now would trigger
    // the exact post-connect ICE reset that we just avoided.
    this.pendingIceCandidates.get(sessionId)?.splice(0);
  }

  private waitForIceGatheringComplete(
    peerConnection: RTCPeerConnection,
    timeoutMs: number
  ): Promise<void> {
    if (peerConnection.iceGatheringState === 'complete') {
      return Promise.resolve();
    }
    return new Promise<void>((resolve) => {
      const settle = (): void => {
        peerConnection.removeEventListener('icegatheringstatechange', onChange);
        clearTimeout(timer);
        resolve();
      };
      const onChange = (): void => {
        if (peerConnection.iceGatheringState === 'complete') {
          settle();
        }
      };
      const timer = setTimeout(settle, timeoutMs);
      peerConnection.addEventListener('icegatheringstatechange', onChange);
    });
  }

  private async sendIceCandidateSafe(
    sessionId: string,
    candidate: RTCIceCandidate
  ): Promise<void> {
    try {
      this.lastRequest = 'SendIceCandidate';
      this.lastRequestTime = new Date();
      await this.signalingClient.sendIceCandidate(new SendIceCandidateRequest({
        sessionId,
        candidate: {
          candidate: candidate.candidate,
          sdpMid: candidate.sdpMid || '',
          sdpMlineIndex: candidate.sdpMLineIndex || 0,
        },
      }));
      logger.debug(`[WebRTC:${sessionId}] Sent ICE candidate`);
    } catch (error) {
      this.handleSessionError(
        sessionId,
        error instanceof Error ? error : new Error(String(error))
      );
    }
  }


  private async applyStartSessionResponse(
    sessionId: string,
    response: StartSessionResponse
  ): Promise<void> {
    const peerConnection = this.peerConnections.get(sessionId);
    if (!peerConnection) {
      throw new Error(`Peer connection not found for session ${sessionId}`);
    }

    if (!response.sdpAnswer) {
      throw new Error(`Missing SDP answer for session ${sessionId}`);
    }

    logger.info(`[WebRTC:${sessionId}] Applying SDP answer`, {
      'webrtc.sdp_length': response.sdpAnswer.length,
      'webrtc.sdp_preview': response.sdpAnswer.substring(0, 500),
    });

    try {
      await peerConnection.setRemoteDescription(
        new RTCSessionDescription({
          type: 'answer',
          sdp: response.sdpAnswer,
        })
      );
      logger.info(`[WebRTC:${sessionId}] Remote description applied from StartSession response`);
    } catch (error) {
      logger.error(`[WebRTC:${sessionId}] Failed to set remote description`, {
        'error.message': error instanceof Error ? error.message : String(error),
        'webrtc.sdp_full': response.sdpAnswer,
      });
      throw error;
    }
  }

  private ensurePolling(sessionId: string): void {
    if (this.pollControllers.has(sessionId)) {
      return;
    }

    const abortController = new AbortController();
    this.pollControllers.set(sessionId, abortController);
    if (!this.pollCursors.has(sessionId)) {
      this.pollCursors.set(sessionId, 0);
    }

    void this.runPollLoop(sessionId, abortController);
  }

  private async runPollLoop(sessionId: string, abortController: AbortController): Promise<void> {
    while (!abortController.signal.aborted && this.peerConnections.has(sessionId)) {
      try {
        const response = await this.signalingClient.pollEvents(new PollEventsRequest({
          sessionId,
          cursor: BigInt(this.pollCursors.get(sessionId) ?? 0),
          timeoutMs: BigInt(DEFAULT_POLL_TIMEOUT_MS),
        }), {
          signal: abortController.signal,
        });

        this.pollCursors.set(sessionId, this.toNumber(response.nextCursor));

        for (const event of response.events) {
          await this.handleSignalingMessage(event, sessionId);
        }
      } catch (error) {
        if (abortController.signal.aborted) {
          return;
        }

        logger.error(`[WebRTC:${sessionId}] PollEvents failed`, {
          'error.message': error instanceof Error ? error.message : String(error),
        });

        await this.delay(RETRY_DELAY_MS);
      }
    }
  }

  private async handleSignalingMessage(message: SignalingMessage, sessionId: string): Promise<void> {
    const peerConnection = this.peerConnections.get(sessionId);
    const caseName = message.message.case;

    if (!peerConnection && caseName !== 'closeSessionResponse') {
      logger.warn(`[WebRTC:${sessionId}] Received signaling message but peer connection not found`);
      return;
    }

    if (caseName === 'startSessionResponse') {
      if (this.sessionNegotiated.get(sessionId)) {
        logger.debug(
          `[WebRTC:${sessionId}] Ignoring duplicate startSessionResponse (already negotiated)`,
          {
            'peer_connection.signaling_state': peerConnection?.signalingState ?? 'unknown',
          }
        );
        return;
      }
      await this.applyStartSessionResponse(sessionId, message.message.value);
      this.sessionNegotiated.set(sessionId, true);
      this.pendingIceCandidates.get(sessionId)?.splice(0);
      return;
    }

    if (caseName === 'iceCandidate') {
      const candidate = message.message.value.candidate;
      if (!candidate || !peerConnection) {
        return;
      }

      await peerConnection.addIceCandidate(new RTCIceCandidate({
        candidate: candidate.candidate,
        sdpMid: candidate.sdpMid || null,
        sdpMLineIndex: candidate.sdpMlineIndex || null,
      }));

      logger.debug(`[WebRTC:${sessionId}] Added remote ICE candidate`);
      return;
    }

    if (caseName === 'iceCandidateResponse') {
      logger.debug(`[WebRTC:${sessionId}] ICE candidate acknowledged`);
      return;
    }

    if (caseName === 'closeSessionResponse') {
      logger.info(`[WebRTC:${sessionId}] Remote signaling session closed`);
      this.cleanupSession(sessionId);
      return;
    }

    if (caseName === 'keepAlive') {
      this.lastHeartbeatAckAt.set(sessionId, Date.now());
      logger.debug(`[WebRTC:${sessionId}] Keepalive received`);
    }
  }

  isDataChannelOpen(sessionId: string): boolean {
    return this.dataChannels.get(sessionId)?.readyState === 'open';
  }

  getPeerConnection(sessionId: string): RTCPeerConnection | null {
    return this.peerConnections.get(sessionId) ?? null;
  }

  async replaceLocalVideoTrack(sessionId: string, track: MediaStreamTrack | null): Promise<boolean> {
    const peerConnection = this.peerConnections.get(sessionId);
    if (!peerConnection) {
      logger.warn(`[WebRTC:${sessionId}] replaceLocalVideoTrack: no peer connection`);
      return false;
    }
    const sender = peerConnection.getSenders().find((s) => s.track?.kind === 'video' || (!s.track && track?.kind === 'video'));
    if (!sender) {
      logger.warn(`[WebRTC:${sessionId}] replaceLocalVideoTrack: no video sender`);
      return false;
    }
    try {
      await sender.replaceTrack(track);
      logger.info(`[WebRTC:${sessionId}] Replaced local video track`, {
        'webrtc.has_track': String(track !== null),
      });
      return true;
    } catch (error) {
      logger.error(`[WebRTC:${sessionId}] Failed to replace local video track`, {
        'error.message': error instanceof Error ? error.message : String(error),
      });
      return false;
    }
  }

  async waitForTransportReady(sessionId: string, timeoutMs = 10_000): Promise<RTCDataChannel> {
    const started = Date.now();
    await this.waitForDataChannelOpen(sessionId, timeoutMs);
    const dataChannel = this.dataChannels.get(sessionId);
    if (!dataChannel) {
      throw new Error(`Data channel not found for session ${sessionId}`);
    }
    const peerConnection = this.peerConnections.get(sessionId);
    if (!peerConnection) {
      throw new Error(`Peer connection not found for session ${sessionId}`);
    }
    const remaining = Math.max(0, timeoutMs - (Date.now() - started));
    await this.waitForSctpConnected(peerConnection, remaining);
    return dataChannel;
  }

  private waitForSctpConnected(
    peerConnection: RTCPeerConnection,
    timeoutMs = 3_000
  ): Promise<void> {
    const sctp = peerConnection.sctp;
    if (!sctp) {
      return Promise.resolve();
    }
    if (sctp.state === 'connected') {
      return Promise.resolve();
    }
    return new Promise<void>((resolve, reject) => {
      const started = Date.now();
      const tick = (): void => {
        const currentSctp = peerConnection.sctp;
        const connectionState = peerConnection.connectionState;
        if (connectionState === 'closed' || connectionState === 'failed') {
          reject(
            new Error(`Peer connection ${connectionState} while waiting for SCTP`)
          );
          return;
        }
        if (currentSctp && currentSctp.state === 'connected') {
          resolve();
          return;
        }
        if (Date.now() - started >= timeoutMs) {
          // Degrade gracefully: do not block the caller forever. Consumers
          // should still guard their sends, but at least the session proceeds.
          logger.warn('[WebRTC] SCTP connected wait timed out, proceeding anyway', {
            'sctp.state': currentSctp?.state ?? 'missing',
            'peer.connection_state': connectionState,
            'timeout_ms': timeoutMs,
          });
          resolve();
          return;
        }
        setTimeout(tick, 15);
      };
      tick();
    });
  }

  private waitForDataChannelOpen(sessionId: string, timeoutMs = 10_000): Promise<void> {
    return new Promise((resolve, reject) => {
      const dc = this.dataChannels.get(sessionId);
      if (!dc) {
        reject(new Error(`Data channel not found for session ${sessionId}`));
        return;
      }
      if (dc.readyState === 'open') {
        resolve();
        return;
      }
      if (dc.readyState === 'closing' || dc.readyState === 'closed') {
        reject(new Error(`Data channel already ${dc.readyState} for session ${sessionId}`));
        return;
      }
      const cleanup = () => {
        clearTimeout(timer);
        dc.removeEventListener('open', onOpen);
        dc.removeEventListener('close', onClose);
        dc.removeEventListener('error', onClose);
      };
      const onOpen = () => {
        cleanup();
        if (dc.readyState === 'open') {
          resolve();
        } else {
          reject(new Error(`Data channel is ${dc.readyState} after open event for session ${sessionId}`));
        }
      };
      const onClose = () => {
        cleanup();
        reject(new Error(`Data channel closed while waiting for session ${sessionId}`));
      };
      const timer = setTimeout(() => {
        cleanup();
        reject(new Error(`Data channel open timeout for session ${sessionId}`));
      }, timeoutMs);
      dc.addEventListener('open', onOpen);
      dc.addEventListener('close', onClose);
      dc.addEventListener('error', onClose);
    });
  }

  private configureDataChannelHandlers(
    sessionId: string,
    dataChannel: RTCDataChannel,
    peerConnection: RTCPeerConnection
  ): void {
    dataChannel.binaryType = 'arraybuffer';
    dataChannel.onopen = () => {
      const debugInfo: Record<string, string | number> = {
        'data_channel.label': dataChannel.label,
        'data_channel.ready_state': dataChannel.readyState,
      };

      if (dataChannel.id !== null) {
        debugInfo['data_channel.id'] = dataChannel.id;
      }

      logger.info(`[WebRTC:${sessionId}] Data channel opened`, debugInfo);

      // Chrome fires onopen as soon as the DCEP handshake accepts the channel,
      // but the underlying RTCSctpTransport can still be in 'connecting'. A
      // send() in that window raises OperationError: "Failure to send data"
      // (errorDetail: data-channel-failure) and Chrome closes the channel.
      // We must wait for sctp.state === 'connected' before exposing the
      // channel as usable and before starting the heartbeat loop.
      void this.waitForSctpConnected(peerConnection)
        .then(() => {
          if (dataChannel.readyState !== 'open') {
            return;
          }
          this.connectionState = 'connected';
          this.lastRequest = 'Data channel ready';
          this.lastRequestTime = new Date();
          this.startHeartbeat(sessionId, DEFAULT_HEARTBEAT_INTERVAL_MS);
        })
        .catch((error) => {
          logger.error(`[WebRTC:${sessionId}] SCTP wait failed`, {
            'error.message': error instanceof Error ? error.message : String(error),
          });
        });
    };

    dataChannel.onmessage = (event: MessageEvent) => {
      this.lastHeartbeatAckAt.set(sessionId, Date.now());
      this.connectionState = 'connected';
      this.lastRequest = typeof event.data === 'string' ? 'Data channel message' : 'Binary frame received';
      this.lastRequestTime = new Date();
    };

    dataChannel.onerror = (event: Event) => {
      const rtcError = (event as RTCErrorEvent).error;
      const errorInfo: Record<string, string | number | boolean> = {
        'error.type': event.type,
        'data_channel.label': dataChannel.label,
        'data_channel.ready_state': dataChannel.readyState,
        'peer_connection.state': peerConnection.connectionState,
        'peer_connection.ice_connection_state': peerConnection.iceConnectionState,
      };
      if (rtcError) {
        errorInfo['rtc_error.message'] = rtcError.message;
        errorInfo['rtc_error.error_detail'] = rtcError.errorDetail;
        if (rtcError.sctpCauseCode != null) {
          errorInfo['rtc_error.sctp_cause_code'] = rtcError.sctpCauseCode;
        }
        if (rtcError.receivedAlert != null) {
          errorInfo['rtc_error.received_alert'] = rtcError.receivedAlert;
        }
        if (rtcError.sentAlert != null) {
          errorInfo['rtc_error.sent_alert'] = rtcError.sentAlert;
        }
      }
      logger.error(`[WebRTC:${sessionId}] Data channel error`, errorInfo);
      // Additional flat-string log so MCP transports that truncate object
      // payloads still carry the full RTCError details in plain text.
      logger.error(
        `[WebRTC:${sessionId}] Data channel error (flat) ${JSON.stringify(errorInfo)}`
      );

      if (dataChannel.readyState === 'closed') {
        this.connectionState = 'disconnected';
        this.triggerReconnect(sessionId, 'data-channel-error');
      }
    };

    dataChannel.onclose = () => {
      logger.debug(`[WebRTC:${sessionId}] Data channel closed`, {
        'data_channel.label': dataChannel.label,
        'peer_connection.state': peerConnection.connectionState,
        'peer_connection.ice_connection_state': peerConnection.iceConnectionState,
      });
      this.connectionState = 'disconnected';
      if (this.sessions.has(sessionId)) {
        this.triggerReconnect(sessionId, 'data-channel-closed');
      }
    };
  }

  private configureRemoteTrackHandlers(
    sessionId: string,
    peerConnection: RTCPeerConnection
  ): void {
    peerConnection.ontrack = (event: RTCTrackEvent) => {
      // C++ may not attach a stream to the outbound track; wrap it if needed.
      const remoteStream =
        event.streams[0] ??
        (() => {
          const s = new MediaStream();
          s.addTrack(event.track);
          return s;
        })();

      this.remoteStreams.set(sessionId, remoteStream);
      this.remoteStreamHandlers.get(sessionId)?.(remoteStream);
      this.connectionState = 'connected';
      this.lastRequest = 'Remote media track';
      this.lastRequestTime = new Date();

      logger.info(`[WebRTC:${sessionId}] Remote media track received`, {
        'webrtc.track_kind': event.track.kind,
        'webrtc.stream_id': remoteStream.id,
      });
    };
  }

  private configurePeerConnectionStateHandlers(
    sessionId: string,
    peerConnection: RTCPeerConnection
  ): void {
    peerConnection.onconnectionstatechange = () => {
      const state = peerConnection.connectionState;
      logger.debug(`[WebRTC:${sessionId}] Connection state changed`, {
        'connection_state': state,
        'ice_connection_state': peerConnection.iceConnectionState,
        'signaling_state': peerConnection.signalingState,
      });

      if (state === 'connected') {
        this.connectionState = 'connected';
      } else if (state === 'failed' || state === 'disconnected' || state === 'closed') {
        this.connectionState = state === 'failed' ? 'error' : 'disconnected';
      }
    };

    peerConnection.oniceconnectionstatechange = () => {
      const iceState = peerConnection.iceConnectionState;
      logger.debug(`[WebRTC:${sessionId}] ICE connection state changed`, {
        'ice_connection_state': iceState,
        'connection_state': peerConnection.connectionState,
      });

      if (iceState === 'failed' || iceState === 'disconnected' || iceState === 'closed') {
        this.connectionState = iceState === 'failed' ? 'error' : 'disconnected';
      }
    };

    peerConnection.onicecandidateerror = (event: Event) => {
      const iceEvent = event as RTCPeerConnectionIceErrorEvent;
      const payload = {
        errorCode: iceEvent.errorCode,
        errorText: iceEvent.errorText,
        address: iceEvent.address,
        port: iceEvent.port,
        url: iceEvent.url,
        hostCandidate: iceEvent.hostCandidate,
      };
      logger.debug(`[WebRTC:${sessionId}] ICE candidate error ${JSON.stringify(payload)}`);
    };
  }

  private handleSessionError(sessionId: string, error: Error): void {
    logger.error(`[WebRTC:${sessionId}] Session error`, {
      'error.message': error.message,
    });

    const peerConnection = this.peerConnections.get(sessionId);
    if (!peerConnection) {
      return;
    }

    if (
      peerConnection.iceConnectionState === 'failed' ||
      peerConnection.iceConnectionState === 'disconnected'
    ) {
      this.cleanupSession(sessionId);
    }
  }

  private cleanupSession(sessionId: string): void {
    // Delete from sessions map first so onclose/onconnectionstatechange handlers
    // see sessions.has(sessionId) === false and skip triggerReconnect.
    this.sessions.delete(sessionId);

    const heartbeatTimer = this.heartbeatTimers.get(sessionId);
    if (heartbeatTimer) {
      clearInterval(heartbeatTimer);
      this.heartbeatTimers.delete(sessionId);
    }

    this.pendingIceCandidates.delete(sessionId);
    this.sessionNegotiated.delete(sessionId);
    this.lastHeartbeatSentAt.delete(sessionId);
    this.lastHeartbeatAckAt.delete(sessionId);
    this.heartbeatConsecutiveFailures.delete(sessionId);
    this.reconnectingSessions.delete(sessionId);

    const pollController = this.pollControllers.get(sessionId);
    if (pollController) {
      pollController.abort();
      this.pollControllers.delete(sessionId);
    }
    this.pollCursors.delete(sessionId);

    const peerConnection = this.peerConnections.get(sessionId);
    if (peerConnection) {
      try {
        logger.debug(`[WebRTC:${sessionId}] Closing peer connection`);
        peerConnection.close();
      } catch (error) {
        logger.debug(`[WebRTC:${sessionId}] Error closing peer connection`, {
          'error.message': error instanceof Error ? error.message : String(error),
        });
      }
      this.peerConnections.delete(sessionId);
    }

    const dataChannel = this.dataChannels.get(sessionId);
    if (dataChannel) {
      try {
        logger.debug(`[WebRTC:${sessionId}] Closing data channel`);
        dataChannel.close();
      } catch (error) {
        logger.debug(`[WebRTC:${sessionId}] Error closing data channel`, {
          'error.message': error instanceof Error ? error.message : String(error),
        });
      }
      this.dataChannels.delete(sessionId);
    }

    const detectionChannel = this.detectionChannels.get(sessionId);
    if (detectionChannel) {
      try {
        detectionChannel.close();
      } catch (error) {
        logger.debug(`[WebRTC:${sessionId}] Error closing detection channel`, {
          'error.message': error instanceof Error ? error.message : String(error),
        });
      }
      this.detectionChannels.delete(sessionId);
    }

    const statsChannel = this.statsChannels.get(sessionId);
    if (statsChannel) {
      try {
        statsChannel.close();
      } catch (error) {
        logger.debug(`[WebRTC:${sessionId}] Error closing stats channel`, {
          'error.message': error instanceof Error ? error.message : String(error),
        });
      }
      this.statsChannels.delete(sessionId);
    }

    const controlChannel = this.controlChannels.get(sessionId);
    if (controlChannel) {
      try {
        controlChannel.close();
      } catch (error) {
        logger.debug(`[WebRTC:${sessionId}] Error closing control channel`, {
          'error.message': error instanceof Error ? error.message : String(error),
        });
      }
      this.controlChannels.delete(sessionId);
      for (const listener of this.controlChannelListeners) {
        try { listener(sessionId, null); } catch (e) { void e; }
      }
    }

    this.remoteStreams.delete(sessionId);
    this.remoteStreamHandlers.delete(sessionId);
  }

  private toNumber(value: bigint | number | string): number {
    return typeof value === 'bigint' ? Number(value) : Number(value);
  }

  private delay(ms: number): Promise<void> {
    return new Promise((resolve) => window.setTimeout(resolve, ms));
  }
}

export const webrtcService = new WebRTCService();
